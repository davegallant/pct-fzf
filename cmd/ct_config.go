package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/davegallant/pvectl/internal/api"
	"github.com/davegallant/pvectl/internal/editconf"
	"github.com/davegallant/pvectl/internal/ssh"
	"github.com/spf13/cobra"
)

// ctConfigAppendLines backs `ct config append`'s repeatable `--line`
// flag — one or more raw "lxc.subkey: value" passthrough lines (cgroup
// device rules, bind mounts, etc.) with no dedicated Proxmox API parameter.
// See api.Config.RawLXC: Proxmox's REST API does not expose these at all
// (confirmed by Proxmox maintainers, not a pvectl gap), so this appends
// directly to /etc/pve/lxc/<vmid>.conf on the node over SSH instead of
// going through PutConfig like every other `ct`/`qm` config command.
var ctConfigAppendLines []string

// runAppendConfig validates ctConfigAppendLines (at least one line, each
// starting with "lxc." — matching the shape of RawLXC's rendered lines)
// and appends them to c's config file over SSH. There is no digest/
// concurrency check here (unlike PutConfig) since this is a raw file
// append, not a structured field update.
func runAppendConfig(_ *api.Client, c api.Container) error {
	if len(ctConfigAppendLines) == 0 {
		return fmt.Errorf("at least one --line is required")
	}
	for _, line := range ctConfigAppendLines {
		if !strings.HasPrefix(strings.TrimSpace(line), "lxc.") {
			return fmt.Errorf("invalid --line %q: raw config lines must start with \"lxc.\"", line)
		}
	}

	fmt.Printf("appending %d line(s) to /etc/pve/lxc/%d.conf on %s:\n", len(ctConfigAppendLines), c.VMID, c.Node)
	for _, line := range ctConfigAppendLines {
		fmt.Printf("  %s\n", line)
	}

	if err := ssh.AppendRawConfig(c.Node, c.VMID, ctConfigAppendLines); err != nil {
		return fmt.Errorf("appending raw config to %s (%d): %w", c.Name, c.VMID, err)
	}
	fmt.Printf("appended %d line(s) to %s (%d)'s config — restart the container for changes to take effect\n", len(ctConfigAppendLines), c.Name, c.VMID)
	return nil
}

// ctConfigCmd groups every `ct config` subcommand — `view` (below), `edit`
// (edit.go), and `append` (above). A package-level var (rather than a
// local one scoped to this file's init) so edit.go's own init can add to
// it too.
var ctConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage a container's config",
}

// ctConfigViewJSON is `ct config view -o json`'s shape. RawLXC is included
// alongside Fields (unlike qm's view, which is just Fields) so `-o json`
// doesn't silently drop the "lxc.*" passthrough lines that the default
// text output shows.
type ctConfigViewJSON struct {
	Fields map[string]string `json:"fields"`
	RawLXC string            `json:"rawLxc,omitempty"`
}

func runConfigView(client *api.Client, c api.Container) error {
	cfg, err := client.GetConfig(context.Background(), c.Node, c.VMID)
	if err != nil {
		return fmt.Errorf("fetching config for %s (%d): %w", c.Name, c.VMID, err)
	}
	if jsonOutput {
		return printJSON(ctConfigViewJSON{Fields: cfg.Fields, RawLXC: cfg.RawLXC})
	}
	fmt.Print(editconf.Render(cfg.Fields) + cfg.RawLXC)
	return nil
}

func init() {
	ctConfigCmd.AddCommand(newSimpleActionCmd("view", "Show a container's config", mutationSafe, runConfigView))

	appendCmd := newSimpleActionCmd("append", "Append raw lxc.* config lines (e.g. cgroup rules, bind mounts) not exposed by the Proxmox API (requires SSH)", mutationMutating, runAppendConfig)
	appendCmd.Flags().StringArrayVar(&ctConfigAppendLines, "line", nil, `raw "lxc.subkey: value" config line to append (repeatable)`)
	ctConfigCmd.AddCommand(appendCmd)
	ctCmd.AddCommand(ctConfigCmd)
}
