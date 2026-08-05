package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/davegallant/pvectl/internal/api"
	"github.com/spf13/cobra"
)

// clusterNodeNames returns every node in the cluster, sorted by name —
// shared by otherClusterNodes (migrate's "valid target" list) and
// ct create's node prompt, so both fetch the node list the same way
// instead of duplicating the ClusterResources call.
func clusterNodeNames(client *api.Client) ([]string, error) {
	resources, err := client.ClusterResources(context.Background())
	if err != nil {
		return nil, fmt.Errorf("listing cluster nodes: %w", err)
	}

	var names []string
	for _, n := range resources.Nodes {
		names = append(names, n.Name)
	}
	sort.Strings(names)
	return names, nil
}

// otherClusterNodes returns the cluster's nodes other than node, sorted
// by name — shared by the interactive prompt and the direct --target
// validation path so both apply the same "what's a valid target" rule.
func otherClusterNodes(client *api.Client, node string) ([]string, error) {
	all, err := clusterNodeNames(client)
	if err != nil {
		return nil, err
	}

	var others []string
	for _, n := range all {
		if n != node {
			others = append(others, n)
		}
	}
	return others, nil
}

// promptTargetNode lists the cluster's other nodes (excluding node) and
// prompts for one to migrate to — same "list valid choices inline, read
// free text" pattern as promptStorage. The first (alphabetically) node is
// shown as the default in brackets and used if the reply is empty; the
// full choice list still appears in parens when there's more than one
// option, so it scales to clusters with several other nodes instead of
// hiding them behind the default. Rejects anything that isn't one of the
// listed nodes, so a typo can't be sent straight to the migrate API (same
// discipline as runDeleteBackup's volid check). Errors immediately,
// without prompting, if there's nowhere else to migrate to. Used by the
// interactive path only — the direct `--target` path uses
// validateTargetNode instead, since it must never touch stdin.
func promptTargetNode(client *api.Client, node string) (string, error) {
	others, err := otherClusterNodes(client, node)
	if err != nil {
		return "", err
	}
	if len(others) == 0 {
		return "", fmt.Errorf("no other cluster nodes to migrate to")
	}

	def := others[0]
	prompt := fmt.Sprintf("target node [%s]", def)
	if len(others) > 1 {
		prompt += fmt.Sprintf(" (%s)", strings.Join(others, ", "))
	}
	fmt.Print(prompt + ": ")

	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	target := strings.TrimSpace(line)
	if target == "" {
		target = def
	}

	for _, n := range others {
		if n == target {
			return target, nil
		}
	}
	return "", fmt.Errorf("%q is not a valid migration target (choices: %s)", target, strings.Join(others, ", "))
}

// validateTargetNode checks that target is one of node's cluster
// siblings, without prompting — used for the non-interactive
// `migrate <name-or-vmid> --target <node>` form, so a typo'd flag value
// gets the same clear rejection a typo at the interactive prompt would.
func validateTargetNode(client *api.Client, node, target string) error {
	others, err := otherClusterNodes(client, node)
	if err != nil {
		return err
	}
	if len(others) == 0 {
		return fmt.Errorf("no other cluster nodes to migrate to")
	}
	for _, n := range others {
		if n == target {
			return nil
		}
	}
	return fmt.Errorf("%q is not a valid migration target (choices: %s)", target, strings.Join(others, ", "))
}

// printRestartNotice warns that migrating a running container causes
// real downtime (Proxmox's restart migration: stop, move, start again) —
// unlike a VM's live migration, which has none. Callers print this
// before the user's last chance to back out: in the interactive path
// that's before promptTargetNode (Ctrl-C at the prompt still works);
// in the direct --target path there's no prompt, so it's printed just
// before the migrate API call instead.
func printRestartNotice(c api.Container) {
	if c.Status == "running" {
		fmt.Println("note: migrating a running container stops and restarts it")
	}
}

var ctMigrateTarget string
var ctMigrateTargetStorage string

var ctMigrateCmd = &cobra.Command{
	Use:               "migrate <name-or-vmid>",
	Short:             "Migrate a container to another node",
	Annotations:       mutationAnnotation(mutationMutating),
	Args:              requireArgs("name-or-vmid"),
	ValidArgsFunction: completeContainerNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := loadClient()
		if err != nil {
			return friendlySetupError(err)
		}
		return runCtMigrate(client, args, ctMigrateTarget, ctMigrateTargetStorage)
	},
}

// runCtMigrate implements ctMigrateCmd's branching: the container is
// resolved directly from args[0]; --target then decides whether the node
// comes from a flag (validated, non-interactive) or an interactive prompt
// (runMigrateWithPrompt). Split out from RunE so it's testable with a
// fake client, independent of loadClient/config state.
//
// targetStorage (--target-storage) is orthogonal to that branching: it's
// carried through both paths untouched, so it works with an interactively
// picked node too. It's deliberately *not* validated against the target
// node's storage list the way target is — Proxmox also accepts a
// `source:target,...` mapping, which no name-in-a-list check would
// recognize, so an unknown storage ID is left for the API to reject.
func runCtMigrate(client *api.Client, args []string, target, targetStorage string) error {
	if len(args) == 0 {
		return fmt.Errorf("a container name or vmid is required")
	}

	c, err := findContainer(client, args[0])
	if err != nil {
		return err
	}
	if target == "" {
		return runMigrateWithPrompt(client, c, targetStorage)
	}
	if err := validateTargetNode(client, c.Node, target); err != nil {
		return err
	}
	printRestartNotice(c)
	return runMigrate(client, c, target, targetStorage)
}

var qmMigrateTarget string
var qmMigrateTargetStorage string

var qmMigrateCmd = &cobra.Command{
	Use:               "migrate <name-or-vmid>",
	Short:             "Migrate a VM to another node",
	Annotations:       mutationAnnotation(mutationMutating),
	Args:              requireArgs("name-or-vmid"),
	ValidArgsFunction: completeVMNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := loadClient()
		if err != nil {
			return friendlySetupError(err)
		}
		return runQmMigrate(client, args, qmMigrateTarget, qmMigrateTargetStorage)
	},
}

// runQmMigrate is runCtMigrate's mirror for QEMU VMs.
func runQmMigrate(client *api.Client, args []string, target, targetStorage string) error {
	if len(args) == 0 {
		return fmt.Errorf("a VM name or vmid is required")
	}

	v, err := findVM(client, args[0])
	if err != nil {
		return err
	}
	if target == "" {
		return runMigrateVMWithPrompt(client, v, targetStorage)
	}
	if err := validateTargetNode(client, v.Node, target); err != nil {
		return err
	}
	return runMigrateVM(client, v, target, targetStorage)
}

// targetStorageUsage describes --target-storage identically for ct and
// qm — the two guest types spell the underlying API parameter
// differently (`target-storage` vs `targetstorage`), but the flag and
// what it accepts are the same on both, so the help text is too.
const targetStorageUsage = "storage on the target node for the guest's volumes, e.g. local-lvm " +
	"or a source:target,... mapping (default: the same storage ID as on the source node)"

func init() {
	ctMigrateCmd.Flags().StringVar(&ctMigrateTarget, "target", "", "node to migrate to (skips the interactive prompt when set)")
	ctMigrateCmd.Flags().StringVar(&ctMigrateTargetStorage, "target-storage", "", targetStorageUsage)
	ctCmd.AddCommand(ctMigrateCmd)

	qmMigrateCmd.Flags().StringVar(&qmMigrateTarget, "target", "", "node to migrate to (skips the interactive prompt when set)")
	qmMigrateCmd.Flags().StringVar(&qmMigrateTargetStorage, "target-storage", "", targetStorageUsage)
	qmCmd.AddCommand(qmMigrateCmd)
}

// printTaskLogIfVerbose dumps upid's real task log under --verbose, even
// on a successful migrate (renderTaskOutcome's own log dump only fires on
// failure) — migrate is the one place pvectl still wants to see log text
// for a *successful* run too (to design a richer progress view against
// actual samples instead of a guessed format), so this is the instrument
// for capturing it, not a general logging feature. Shares the actual
// fetch+print with renderTaskOutcome's failure-path log dump
// (printTaskLogLines in progress.go) rather than duplicating it.
func printTaskLogIfVerbose(client *api.Client, node, upid string) {
	if !verbose {
		return
	}
	printTaskLogLines(client, node, upid)
}
