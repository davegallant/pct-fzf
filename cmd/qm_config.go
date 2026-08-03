package cmd

import (
	"context"
	"fmt"

	"github.com/davegallant/pvectl/internal/api"
	"github.com/davegallant/pvectl/internal/editconf"
	"github.com/spf13/cobra"
)

// qmConfigCmd groups every `qm config` subcommand — `view` (below) and
// `edit` (qm_edit.go). A package-level var (rather than a local one
// scoped to some other file's init) so qm_edit.go's own init can add to
// it. Unlike ctConfigCmd, there's no `append` here: VMConfig has no raw
// lxc.*-style passthrough block to begin with, so there's nothing
// Proxmox's API fails to expose for QEMU VMs the way it does for LXC
// containers.
var qmConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage a VM's config",
}

func runVMConfigView(client *api.Client, v api.VM) error {
	cfg, err := client.GetVMConfig(context.Background(), v.Node, v.VMID)
	if err != nil {
		return fmt.Errorf("fetching config for %s (%d): %w", v.Name, v.VMID, err)
	}
	if jsonOutput {
		return printJSON(cfg.Fields)
	}
	fmt.Print(editconf.Render(cfg.Fields))
	return nil
}

func init() {
	qmConfigCmd.AddCommand(newSimpleVMActionCmd("view", "Show a VM's config", mutationSafe, runVMConfigView))
	qmCmd.AddCommand(qmConfigCmd)
}
