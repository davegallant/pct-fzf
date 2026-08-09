package cmd

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/davegallant/pvectl/internal/api"
	"github.com/spf13/cobra"
)

var ctCmd = &cobra.Command{
	Use:   "ct",
	Short: "Manage containers",
}

var ctListNode string

var ctListCmd = &cobra.Command{
	Use:         "list",
	Aliases:     []string{"ls"},
	Short:       "List containers",
	Annotations: mutationAnnotation(mutationSafe),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := loadClient()
		if err != nil {
			return friendlySetupError(err)
		}
		return runCtList(client, ctListNode)
	},
}

func init() {
	rootCmd.AddCommand(ctCmd)
	ctCmd.AddCommand(ctListCmd)
	ctListCmd.Flags().StringVar(&ctListNode, "node", "", "only list containers on this node")
}

// runCtList fetches and prints a plain VMID/NAME/NODE/STATUS table of
// every container in the cluster, or just those on node if it's non-empty.
func runCtList(client *api.Client, node string) error {
	containers, err := client.ListContainers(context.Background())
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}
	if node != "" {
		filtered := containers[:0]
		for _, c := range containers {
			if c.Node == node {
				filtered = append(filtered, c)
			}
		}
		containers = filtered
	}
	if jsonOutput {
		return printJSON(containers)
	}
	fmt.Print(renderContainerList(containers))
	return nil
}

// renderContainerList formats containers as a VMID/NAME/NODE/STATUS
// table from already-fetched data, plus a TAGS column when any container
// is tagged (see anyTagged). It performs no I/O, so it's directly
// unit-testable — same pattern as renderNodes/renderStorageReport.
func renderContainerList(containers []api.Container) string {
	showTags := anyTagged(containers, func(c api.Container) []string { return c.Tags })

	var buf strings.Builder
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	header := "VMID\tNAME\tNODE\tSTATUS"
	if showTags {
		header += "\tTAGS"
	}
	_, _ = fmt.Fprintln(tw, header)
	for _, c := range containers {
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%s", c.VMID, c.Name, c.Node, c.Status)
		if showTags {
			_, _ = fmt.Fprintf(tw, "\t%s", joinTags(c.Tags))
		}
		_, _ = fmt.Fprintln(tw)
	}
	_ = tw.Flush()
	return buf.String()
}
