package cmd

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/davegallant/pvectl/internal/api"
)

// resolveImageNode picks the node an image command talks to. Both the
// appliance catalog and a download are node-scoped calls, but neither is
// node-*specific* in a way the user should have to care about: the catalog
// is identical cluster-wide, and a download lands on a storage, which the
// user picks separately. So an omitted --node means "any node", resolved
// to the alphabetically first one for a deterministic choice rather than a
// random-feeling one.
//
// Offline nodes are skipped rather than merely sorted around: picking one
// would fail every image command whenever the alphabetically first node
// happened to be down, even with a perfectly healthy cluster behind it.
func resolveImageNode(client *api.Client, node string) (string, error) {
	if node != "" {
		return node, nil
	}
	resources, err := client.ClusterResources(context.Background())
	if err != nil {
		return "", fmt.Errorf("listing cluster nodes: %w", err)
	}
	names := onlineNodeNames(resources.Nodes)
	if len(names) == 0 {
		return "", fmt.Errorf("no online cluster nodes found")
	}
	return names[0], nil
}

// onlineNodeNames returns the names of nodes reporting status "online",
// sorted. Pure, so resolveImageNode's node-selection rule is testable
// without a cluster.
func onlineNodeNames(nodes []api.NodeResource) []string {
	var names []string
	for _, n := range nodes {
		if n.Status == "online" {
			names = append(names, n.Name)
		}
	}
	sort.Strings(names)
	return names
}

// promptContentStorage lists node's storages accepting contentType and
// prompts for one — promptImagesStorage/promptRootfsStorage generalized,
// since the image commands need "vztmpl" and "iso" rather than a fixed
// content type.
func promptContentStorage(client *api.Client, node, contentType string) (string, error) {
	storages, err := client.ListNodeStorages(context.Background(), node)
	if err != nil {
		return "", fmt.Errorf("listing storages on %s: %w", node, err)
	}

	var names []string
	for _, s := range storages {
		if s.SupportsContent(contentType) {
			names = append(names, s.Storage)
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no storage on %s accepts %q content", node, contentType)
	}
	sort.Strings(names)
	return promptChoice("storage", names)
}

// filenameFromURL derives the storage filename for a download from the
// URL's last path segment, so `iso download <url>` doesn't demand a
// --filename for the common case. Query strings and fragments are
// discarded (they're not part of the name), and a URL with no usable
// segment returns an error rather than a guess — Proxmox requires a
// filename and would reject an empty one with a less obvious message.
func filenameFromURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parsing url %q: %w", raw, err)
	}
	name := path.Base(strings.TrimSuffix(parsed.Path, "/"))
	if name == "" || name == "." || name == "/" {
		return "", fmt.Errorf("cannot derive a filename from %q — pass --filename", raw)
	}
	return name, nil
}

// validateChecksum enforces Proxmox's "both or neither" rule for the
// checksum pair, up front and by flag name, rather than letting the API
// reject the request with a less specific error.
func validateChecksum(checksum, algorithm string) error {
	switch {
	case checksum != "" && algorithm == "":
		return fmt.Errorf("--checksum requires --checksum-algorithm")
	case checksum == "" && algorithm != "":
		return fmt.Errorf("--checksum-algorithm requires --checksum")
	}
	return nil
}
