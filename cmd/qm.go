package cmd

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/davegallant/pvectl/internal/api"
	"github.com/spf13/cobra"
)

var qmCmd = &cobra.Command{
	Use:   "qm",
	Short: "Manage QEMU VMs",
}

var qmListNode string

var qmListCmd = &cobra.Command{
	Use:         "list",
	Aliases:     []string{"ls"},
	Short:       "List VMs",
	Annotations: mutationAnnotation(mutationSafe),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := loadClient()
		if err != nil {
			return friendlySetupError(err)
		}
		return runQmList(client, qmListNode)
	},
}

func init() {
	rootCmd.AddCommand(qmCmd)
	qmCmd.AddCommand(qmListCmd)
	qmListCmd.Flags().StringVar(&qmListNode, "node", "", "only list VMs on this node")
}

// runQmList fetches and prints a plain VMID/NAME/NODE/STATUS table of
// every VM in the cluster, or just those on node if it's non-empty.
func runQmList(client *api.Client, node string) error {
	vms, err := client.ListVMs(context.Background())
	if err != nil {
		return fmt.Errorf("listing VMs: %w", err)
	}
	if node != "" {
		filtered := vms[:0]
		for _, v := range vms {
			if v.Node == node {
				filtered = append(filtered, v)
			}
		}
		vms = filtered
	}
	if jsonOutput {
		return printJSON(vms)
	}
	fmt.Print(renderVMList(vms))
	return nil
}

// renderVMList formats vms as a VMID/NAME/NODE/STATUS table from
// already-fetched data, plus a TAGS column when any VM is tagged (see
// anyTagged). It performs no I/O, so it's directly unit-testable — same
// pattern as renderContainerList.
func renderVMList(vms []api.VM) string {
	showTags := anyTagged(vms, func(v api.VM) []string { return v.Tags })

	var buf strings.Builder
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	header := "VMID\tNAME\tNODE\tSTATUS"
	if showTags {
		header += "\tTAGS"
	}
	_, _ = fmt.Fprintln(tw, header)
	for _, v := range vms {
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%s", v.VMID, v.Name, v.Node, v.Status)
		if showTags {
			_, _ = fmt.Fprintf(tw, "\t%s", joinTags(v.Tags))
		}
		_, _ = fmt.Fprintln(tw)
	}
	_ = tw.Flush()
	return buf.String()
}
