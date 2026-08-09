package cmd

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/davegallant/pvectl/internal/api"
	"github.com/spf13/cobra"
)

var (
	templatesListNode        string
	templatesListDownloaded  bool
	templatesDownloadNode    string
	templatesDownloadStorage string
)

var templatesCmd = &cobra.Command{
	Use:     "templates",
	Aliases: []string{"template"},
	Short:   "Manage LXC OS templates",
}

var templatesListCmd = &cobra.Command{
	Use:         "list",
	Aliases:     []string{"ls"},
	Short:       "List LXC templates available to download (or --downloaded for those already on storage)",
	Annotations: mutationAnnotation(mutationSafe),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := loadClient()
		if err != nil {
			return friendlySetupError(err)
		}
		return runTemplatesList(client)
	},
}

var templatesDownloadCmd = &cobra.Command{
	Use:         "download <template>",
	Short:       "Download an LXC OS template onto a storage",
	Annotations: mutationAnnotation(mutationMutating),
	Args:        requireArgs("template"),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return completeApplianceNames(toComplete)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := loadClient()
		if err != nil {
			return friendlySetupError(err)
		}
		return runTemplatesDownload(client, args[0])
	},
}

func init() {
	rootCmd.AddCommand(templatesCmd)
	templatesCmd.AddCommand(templatesListCmd)
	templatesCmd.AddCommand(templatesDownloadCmd)

	templatesListCmd.Flags().StringVar(&templatesListNode, "node", "", "node to query (defaults to any node — the catalog is cluster-wide)")
	templatesListCmd.Flags().BoolVar(&templatesListDownloaded, "downloaded", false, "list templates already present on storage instead of the download catalog")
	templatesDownloadCmd.Flags().StringVar(&templatesDownloadNode, "node", "", "node to run the download on (defaults to any node)")
	templatesDownloadCmd.Flags().StringVar(&templatesDownloadStorage, "storage", "", "storage to download onto (prompts if omitted)")
}

func runTemplatesList(client *api.Client) error {
	node, err := resolveImageNode(client, templatesListNode)
	if err != nil {
		return err
	}

	if templatesListDownloaded {
		templates, err := client.ListTemplates(context.Background(), node, storageNamesForNode(client, node))
		if err != nil {
			return fmt.Errorf("listing downloaded templates on %s: %w", node, err)
		}
		if jsonOutput {
			return printJSON(templates)
		}
		fmt.Print(renderDownloadedTemplates(templates))
		return nil
	}

	appliances, err := client.ListAppliances(context.Background(), node)
	if err != nil {
		return fmt.Errorf("listing available templates on %s: %w", node, err)
	}
	if jsonOutput {
		return printJSON(appliances)
	}
	fmt.Print(renderAppliances(appliances))
	return nil
}

func runTemplatesDownload(client *api.Client, template string) error {
	node, err := resolveImageNode(client, templatesDownloadNode)
	if err != nil {
		return err
	}

	storage := templatesDownloadStorage
	if storage == "" {
		storage, err = promptContentStorage(client, node, "vztmpl")
		if err != nil {
			return err
		}
	}

	upid, err := client.DownloadAppliance(context.Background(), node, storage, template)
	if err != nil {
		return fmt.Errorf("downloading %s to %s: %w", template, storage, err)
	}
	return runProgressAction(client, node, upid,
		fmt.Sprintf("downloading %s to %s", template, storage),
		fmt.Sprintf("downloaded %s to %s", template, storage))
}

// renderAppliances formats the download catalog as a
// TEMPLATE/OS/VERSION/DESCRIPTION table. It performs no I/O, so it's
// directly unit-testable — same pattern as renderContainerList.
func renderAppliances(appliances []api.Appliance) string {
	var buf strings.Builder
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "TEMPLATE\tOS\tVERSION\tDESCRIPTION")
	for _, a := range appliances {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", a.Template, a.OS, a.Version, a.Headline)
	}
	_ = tw.Flush()
	return buf.String()
}

// renderDownloadedTemplates formats already-downloaded templates as a
// VOLID/STORAGE/SIZE table — the --downloaded counterpart to
// renderAppliances.
func renderDownloadedTemplates(templates []api.Template) string {
	var buf strings.Builder
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "VOLID\tSTORAGE\tSIZE")
	for _, t := range templates {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", t.VolID, t.Storage, formatBytes(t.Size))
	}
	_ = tw.Flush()
	return buf.String()
}

// completeApplianceNames completes `templates download`'s argument from
// the live catalog. Unlike the guest-name completions, this is a single
// node-scoped call with no vmid resolution, so it's cheap enough to run on
// every Tab press without caching.
func completeApplianceNames(toComplete string) ([]string, cobra.ShellCompDirective) {
	client, err := loadClient()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	node, err := resolveImageNode(client, templatesDownloadNode)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	appliances, err := client.ListAppliances(context.Background(), node)
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var names []string
	for _, a := range appliances {
		if strings.HasPrefix(a.Template, toComplete) {
			names = append(names, a.Template)
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}
