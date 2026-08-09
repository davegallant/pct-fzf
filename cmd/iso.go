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
	isoListNode                  string
	isoDownloadNode              string
	isoDownloadStorage           string
	isoDownloadFilename          string
	isoDownloadChecksum          string
	isoDownloadChecksumAlgorithm string
)

var isoCmd = &cobra.Command{
	Use:   "iso",
	Short: "Manage ISO images",
}

var isoListCmd = &cobra.Command{
	Use:         "list",
	Aliases:     []string{"ls"},
	Short:       "List ISO images already on storage",
	Annotations: mutationAnnotation(mutationSafe),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := loadClient()
		if err != nil {
			return friendlySetupError(err)
		}
		return runISOList(client)
	},
}

var isoDownloadCmd = &cobra.Command{
	Use:   "download <url>",
	Short: "Download an ISO image onto a storage",
	Long: `Download an ISO image onto a storage.

The Proxmox node fetches the URL, not pvectl — so the URL must be
reachable from the cluster, and the transfer runs as a background task.`,
	Annotations: mutationAnnotation(mutationMutating),
	Args:        requireArgs("url"),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := loadClient()
		if err != nil {
			return friendlySetupError(err)
		}
		return runISODownload(client, args[0])
	},
}

func init() {
	rootCmd.AddCommand(isoCmd)
	isoCmd.AddCommand(isoListCmd)
	isoCmd.AddCommand(isoDownloadCmd)

	isoListCmd.Flags().StringVar(&isoListNode, "node", "", "node whose storages to list (defaults to any node)")
	isoDownloadCmd.Flags().StringVar(&isoDownloadNode, "node", "", "node to run the download on (defaults to any node)")
	isoDownloadCmd.Flags().StringVar(&isoDownloadStorage, "storage", "", "storage to download onto (prompts if omitted)")
	isoDownloadCmd.Flags().StringVar(&isoDownloadFilename, "filename", "", "name to store the ISO under (defaults to the URL's last path segment)")
	isoDownloadCmd.Flags().StringVar(&isoDownloadChecksum, "checksum", "", "expected checksum of the downloaded file (requires --checksum-algorithm)")
	isoDownloadCmd.Flags().StringVar(&isoDownloadChecksumAlgorithm, "checksum-algorithm", "", "checksum algorithm, e.g. sha256 (requires --checksum)")
}

func runISOList(client *api.Client) error {
	node, err := resolveImageNode(client, isoListNode)
	if err != nil {
		return err
	}

	isos, err := client.ListISOs(context.Background(), node, storageNamesForNode(client, node))
	if err != nil {
		return fmt.Errorf("listing ISOs on %s: %w", node, err)
	}
	if jsonOutput {
		return printJSON(isos)
	}
	fmt.Print(renderISOs(isos))
	return nil
}

func runISODownload(client *api.Client, rawURL string) error {
	if err := validateChecksum(isoDownloadChecksum, isoDownloadChecksumAlgorithm); err != nil {
		return err
	}

	filename := isoDownloadFilename
	if filename == "" {
		var err error
		filename, err = filenameFromURL(rawURL)
		if err != nil {
			return err
		}
	}

	node, err := resolveImageNode(client, isoDownloadNode)
	if err != nil {
		return err
	}

	storage := isoDownloadStorage
	if storage == "" {
		storage, err = promptContentStorage(client, node, "iso")
		if err != nil {
			return err
		}
	}

	upid, err := client.DownloadURL(context.Background(), node, api.DownloadURLParams{
		Storage:           storage,
		Content:           "iso",
		Filename:          filename,
		URL:               rawURL,
		Checksum:          isoDownloadChecksum,
		ChecksumAlgorithm: isoDownloadChecksumAlgorithm,
	})
	if err != nil {
		return fmt.Errorf("downloading %s to %s: %w", filename, storage, err)
	}
	return runProgressAction(client, node, upid,
		fmt.Sprintf("downloading %s to %s", filename, storage),
		fmt.Sprintf("downloaded %s to %s", filename, storage))
}

// renderISOs formats ISO images as a VOLID/STORAGE/SIZE table. It performs
// no I/O, so it's directly unit-testable — same pattern as
// renderDownloadedTemplates.
func renderISOs(isos []api.ISO) string {
	var buf strings.Builder
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "VOLID\tSTORAGE\tSIZE")
	for _, i := range isos {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", i.VolID, i.Storage, formatBytes(i.Size))
	}
	_ = tw.Flush()
	return buf.String()
}
