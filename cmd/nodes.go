package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/davegallant/pvectl/internal/api"
	"github.com/spf13/cobra"
)

var nodesCmd = &cobra.Command{
	Use:   "nodes",
	Short: "Manage Proxmox cluster nodes",
}

var nodesRebootYes bool

var nodesRebootCmd = &cobra.Command{
	Use:         "reboot <node>",
	Short:       "Reboot a Proxmox node",
	Args:        cobra.ExactArgs(1),
	Annotations: mutationAnnotation(mutationDestructive),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := loadClient()
		if err != nil {
			return friendlySetupError(err)
		}
		return runRebootNode(client, args[0], nodesRebootYes)
	},
}

var nodesListCmd = &cobra.Command{
	Use:         "list",
	Aliases:     []string{"ls"},
	Short:       "List Proxmox cluster nodes",
	Annotations: mutationAnnotation(mutationSafe),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := loadClient()
		if err != nil {
			return friendlySetupError(err)
		}
		return runNodes(client)
	},
}

func init() {
	rootCmd.AddCommand(nodesCmd)
	nodesCmd.AddCommand(nodesListCmd, nodesRebootCmd)
	nodesRebootCmd.Flags().BoolVarP(&nodesRebootYes, "yes", "y", false, "skip the confirmation prompt")
}

func runRebootNode(client *api.Client, node string, skipConfirm bool) error {
	status, err := client.ClusterStatus(context.Background())
	if err != nil {
		return fmt.Errorf("validating node %s: %w", node, err)
	}
	if _, ok := status.Nodes[node]; !ok {
		return fmt.Errorf("node %q not found", node)
	}

	fmt.Printf("about to reboot node %s — every guest on this node will be interrupted\n", node)
	if !skipConfirm {
		fmt.Print("type 'yes' to confirm: ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(line) != "yes" {
			fmt.Println("aborted, node not rebooted")
			return nil
		}
	}

	if err := client.RebootNode(context.Background(), node); err != nil {
		return fmt.Errorf("rebooting node %s: %w", node, err)
	}
	fmt.Printf("reboot requested for node %s\n", node)
	return nil
}

// runNodes fetches the two independent endpoints `pvectl nodes list` needs
// (ClusterStatus, ClusterResources) concurrently rather than
// sequentially, halving the one-shot latency (~2 round trips → ~1). Same
// shared-Client/concurrency-safety reasoning as runStatus in status.go;
// errors are checked in the original ClusterStatus → ClusterResources
// order so a failure still reports the first endpoint that failed.
func runNodes(client *api.Client) error {
	ctx := context.Background()

	var (
		status    api.ClusterStatus
		resources api.ClusterResources
		statusErr error
		resErr    error
	)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); status, statusErr = client.ClusterStatus(ctx) }()
	go func() { defer wg.Done(); resources, resErr = client.ClusterResources(ctx) }()
	wg.Wait()

	if statusErr != nil {
		return fmt.Errorf("fetching cluster status: %w", statusErr)
	}
	if resErr != nil {
		return fmt.Errorf("fetching cluster resources: %w", resErr)
	}

	if jsonOutput {
		return printJSON(nodesJSON(status, resources.Nodes))
	}
	fmt.Print(renderNodes(status, resources.Nodes))
	return nil
}

// nodeJSON is one node's `nodes list --json` entry, joining the IP from
// ClusterStatus with the CPU/mem usage from ClusterResources — the same
// two sources renderNodesTable's columns come from.
type nodeJSON struct {
	Name   string  `json:"name"`
	IP     string  `json:"ip,omitempty"`
	Status string  `json:"status"`
	CPU    float64 `json:"cpu"` // fraction 0-1
	Mem    int64   `json:"mem"`
	MaxMem int64   `json:"maxMem"`
}

// nodesJSON builds nodeJSON entries from already-fetched data, sorted by
// name — mirrors renderNodes's sort so JSON and table output agree on
// order.
func nodesJSON(status api.ClusterStatus, nodeResources []api.NodeResource) []nodeJSON {
	nodes := append([]api.NodeResource(nil), nodeResources...)
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Name < nodes[j].Name
	})

	out := make([]nodeJSON, 0, len(nodes))
	for _, n := range nodes {
		ip := ""
		if ns, ok := status.Nodes[n.Name]; ok {
			ip = ns.IP
		}
		out = append(out, nodeJSON{
			Name:   n.Name,
			IP:     ip,
			Status: n.Status,
			CPU:    n.CPU,
			Mem:    n.Mem,
			MaxMem: n.MaxMem,
		})
	}
	return out
}

// renderNodes formats the cluster's node table (NAME/IP/STATUS/CPU/MEM)
// from already-fetched data. It performs no I/O, so it's directly
// unit-testable.
func renderNodes(status api.ClusterStatus, nodeResources []api.NodeResource) string {
	nodes := append([]api.NodeResource(nil), nodeResources...)
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Name < nodes[j].Name
	})

	var buf strings.Builder
	renderNodesTable(&buf, status, nodes)
	return buf.String()
}
