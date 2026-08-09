package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/davegallant/pvectl/internal/api"
	"github.com/spf13/cobra"
)

// qmExecPollInterval is how often exec-status is polled while the guest
// command runs. The guest agent buffers everything until exit, so polling
// faster wouldn't produce output any sooner — this only bounds how long
// after the command finishes pvectl notices.
const qmExecPollInterval = 250 * time.Millisecond

var qmExecTimeout time.Duration

var qmExecCmd = &cobra.Command{
	Use:   "exec <name-or-vmid> -- <command> [args...]",
	Short: "Run a command inside a VM via the QEMU guest agent, non-interactively",
	Long: `Run a command inside a VM via the QEMU guest agent, non-interactively.

Unlike "pvectl ct exec", this does not stream. Proxmox's guest-agent API
is fire-and-poll: the command is started, then polled for completion, and
its output is only readable once it has exited. There is no stdin, no tty,
and no incremental output — so interactive or long-running commands are
the wrong fit, and that is a property of the Proxmox API rather than
something pvectl can work around.

The command is passed to the guest as argv, with no shell involved. Shell
syntax needs an explicit shell:

  pvectl qm exec myvm -- sh -c 'ls /etc | head'

Requires the guest agent to be enabled on the VM (qm set <vmid> --agent 1)
and running inside the guest.`,
	Annotations:       mutationAnnotation(mutationDestructive),
	Args:              requireMinArgs("name-or-vmid", "command"),
	ValidArgsFunction: completeVMNamesOnly,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := loadClient()
		if err != nil {
			return friendlySetupError(err)
		}
		v, err := resolveVM(client, args)
		if err != nil {
			return err
		}
		return runQmExec(client, v, args[1:])
	},
}

func init() {
	qmExecCmd.Flags().DurationVar(&qmExecTimeout, "timeout", 30*time.Second, "how long to wait for the command to finish")
	qmCmd.AddCommand(qmExecCmd)
}

// completeVMNamesOnly completes the VM name but deliberately stops there.
// ct exec completes remote paths by SSHing to the node and running ls;
// the guest agent has no equivalent that's worth a round trip per Tab, so
// the command and its arguments are left uncompleted.
func completeVMNamesOnly(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return completeVMNames(cmd, args, toComplete)
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

// exitCodeError carries a guest command's own exit status out to main,
// which exits with that code instead of printing an "error:" line — the
// guest already wrote whatever it had to say to stderr. Satisfies the same
// `ExitCode() int` shape as *exec.ExitError, which is how `ct exec`
// propagates SSH's exit status, so main handles both through one check.
type exitCodeError struct{ code int }

func (e exitCodeError) Error() string {
	return fmt.Sprintf("command exited with status %d", e.code)
}

func (e exitCodeError) ExitCode() int { return e.code }

// runQmExec starts command in v's guest and blocks until it exits or
// qmExecTimeout elapses, forwarding the guest's stdout/stderr and exit
// status to pvectl's own.
func runQmExec(client *api.Client, v api.VM, command []string) error {
	ctx := context.Background()

	// Checked up front so the common misconfiguration produces a
	// actionable message rather than the API's generic agent error.
	config, err := client.GetVMConfig(ctx, v.Node, v.VMID)
	if err != nil {
		return fmt.Errorf("fetching config for %s (%d): %w", v.Name, v.VMID, err)
	}
	if !api.AgentEnabled(config.Fields["agent"]) {
		return fmt.Errorf("%s (%d) has no guest agent configured — enable it with `qm set %d --agent 1`, then reboot the VM and install qemu-guest-agent inside the guest",
			v.Name, v.VMID, v.VMID)
	}

	pid, err := client.AgentExec(ctx, v.Node, v.VMID, command)
	if err != nil {
		// The config says the agent is enabled, so a failure here means
		// it isn't actually answering — a stopped VM, or the agent not
		// installed/running in the guest.
		return fmt.Errorf("starting command in %s (%d): %w — the agent is enabled in the VM's config but not responding; check the VM is running and qemu-guest-agent is installed and started inside the guest",
			v.Name, v.VMID, err)
	}

	result, err := pollAgentExec(ctx, client, v, pid)
	if err != nil {
		return err
	}

	if result.OutData != "" {
		fmt.Fprint(os.Stdout, result.OutData)
	}
	if result.ErrData != "" {
		fmt.Fprint(os.Stderr, result.ErrData)
	}
	if result.OutTruncated {
		fmt.Fprintln(os.Stderr, "warning: stdout was truncated by the guest agent")
	}
	if result.ErrTruncated {
		fmt.Fprintln(os.Stderr, "warning: stderr was truncated by the guest agent")
	}

	if result.ExitCode != 0 {
		return exitCodeError{code: result.ExitCode}
	}
	return nil
}

// pollAgentExec polls exec-status until the guest command exits or the
// timeout elapses. A timeout reports the guest-side pid, since the command
// keeps running in the guest after pvectl stops waiting — the user needs
// the pid to check on it.
func pollAgentExec(ctx context.Context, client *api.Client, v api.VM, pid int) (api.AgentExecResult, error) {
	deadline := time.Now().Add(qmExecTimeout)
	for {
		result, err := client.AgentExecStatus(ctx, v.Node, v.VMID, pid)
		if err != nil {
			return api.AgentExecResult{}, fmt.Errorf("polling command status in %s (%d): %w", v.Name, v.VMID, err)
		}
		if result.Exited {
			return result, nil
		}
		if time.Now().After(deadline) {
			return api.AgentExecResult{}, fmt.Errorf("command did not finish within %s — it is still running in %s (%d) as guest pid %d",
				qmExecTimeout, v.Name, v.VMID, pid)
		}
		time.Sleep(qmExecPollInterval)
	}
}
