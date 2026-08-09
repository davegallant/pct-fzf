package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/davegallant/pvectl/internal/api"
	"github.com/spf13/cobra"
)

var storageCmd = &cobra.Command{
	Use:   "storage",
	Short: "Manage cluster storage",
}

var storageListCmd = &cobra.Command{
	Use:         "list",
	Aliases:     []string{"ls"},
	Short:       "Show cluster storage usage and health",
	Annotations: mutationAnnotation(mutationSafe),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := loadClient()
		if err != nil {
			return friendlySetupError(err)
		}
		return runStorage(client)
	},
}

func init() {
	rootCmd.AddCommand(storageCmd)
	storageCmd.AddCommand(storageListCmd)
}

func runStorage(client *api.Client) error {
	resources, err := client.ClusterResources(context.Background())
	if err != nil {
		return fmt.Errorf("fetching cluster resources: %w", err)
	}
	if jsonOutput {
		return printJSON(sortedCollapsedStorage(resources.Storage))
	}
	fmt.Print(renderStorageReport(resources.Storage))
	return nil
}

// renderStorageReport formats the cluster's storage table plus any
// capacity warnings from already-fetched data. It performs no I/O, so
// it's directly unit-testable — same pattern as renderStatus/renderNodes.
func renderStorageReport(storages []api.StorageResource) string {
	storage := sortedCollapsedStorage(storages)

	var buf strings.Builder
	renderStorageTable(&buf, storage)
	if warnings := renderStorageWarnings(storage); warnings != "" {
		buf.WriteString("\n")
		buf.WriteString(warnings)
	}
	return buf.String()
}
