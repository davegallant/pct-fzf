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

var (
	ctCreateNode         string
	ctCreateTemplate     string
	ctCreateStorage      string
	ctCreateHostname     string
	ctCreateVMID         int
	ctCreateCores        int
	ctCreateMemory       int
	ctCreateSwap         int
	ctCreateDiskSize     int
	ctCreateNet0         string
	ctCreateUnprivileged bool
	ctCreateFeatures     string
	ctCreateArch         string
	ctCreatePassword     string
	ctCreateSSHKeyFile   string
	ctCreateTags         string
	ctCreateStart        bool
)

var ctCreateCmd = &cobra.Command{
	Use:         "create",
	Short:       "Create a new LXC container",
	Annotations: mutationAnnotation(mutationMutating),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := loadClient()
		if err != nil {
			return friendlySetupError(err)
		}
		return runCtCreate(client, cmd.Flags().Changed("start"))
	},
}

func init() {
	ctCreateCmd.Flags().StringVar(&ctCreateNode, "node", "", "node to create the container on (prompts if omitted)")
	ctCreateCmd.Flags().StringVar(&ctCreateTemplate, "template", "", "OS template volid, e.g. local:vztmpl/ubuntu-24.04-standard_24.04-2_amd64.tar.zst (prompts if omitted)")
	ctCreateCmd.Flags().StringVar(&ctCreateStorage, "storage", "", "storage for the container's root filesystem (prompts if omitted)")
	ctCreateCmd.Flags().StringVar(&ctCreateHostname, "hostname", "", "container hostname (prompts if omitted)")
	ctCreateCmd.Flags().IntVar(&ctCreateVMID, "vmid", 0, "container ID (0 = auto-assign the next free ID)")
	ctCreateCmd.Flags().IntVar(&ctCreateCores, "cores", 1, "CPU cores")
	ctCreateCmd.Flags().IntVar(&ctCreateMemory, "memory", 512, "memory in MB")
	ctCreateCmd.Flags().IntVar(&ctCreateSwap, "swap", 512, "swap in MB")
	ctCreateCmd.Flags().IntVar(&ctCreateDiskSize, "disk-size", 8, "root filesystem disk size in GB")
	ctCreateCmd.Flags().StringVar(&ctCreateNet0, "net0", "name=eth0,bridge=vmbr0,ip=dhcp", "network interface config (Proxmox net0 syntax)")
	ctCreateCmd.Flags().BoolVar(&ctCreateUnprivileged, "unprivileged", true, "create an unprivileged container")
	ctCreateCmd.Flags().StringVar(&ctCreateFeatures, "features", "nesting=1", "container features, e.g. nesting=1")
	ctCreateCmd.Flags().StringVar(&ctCreateArch, "arch", "amd64", "container architecture")
	ctCreateCmd.Flags().StringVar(&ctCreatePassword, "password", "", "root password (optional; omit along with --ssh-public-key-file for console-only access)")
	ctCreateCmd.Flags().StringVar(&ctCreateSSHKeyFile, "ssh-public-key-file", "", "path to an SSH public key file to authorize for root (optional)")
	ctCreateCmd.Flags().StringVar(&ctCreateTags, "tags", "", "comma-separated tags to apply, e.g. media,arr (optional)")
	ctCreateCmd.Flags().BoolVar(&ctCreateStart, "start", false, "start the container after creating it (prompts if omitted)")
	ctCmd.AddCommand(ctCreateCmd)
}

// promptChoice lists choices with the first shown as the default (same
// bracket-default styling as promptTargetNode) and reads a free-text
// reply, defaulting to that first choice on an empty line. Only a value
// that actually appeared in choices is accepted, so a typo can't be sent
// straight to the create API.
func promptChoice(label string, choices []string) (string, error) {
	if len(choices) == 0 {
		return "", fmt.Errorf("no %s available", label)
	}

	def := choices[0]
	prompt := fmt.Sprintf("%s [%s]", label, def)
	if len(choices) > 1 {
		prompt += fmt.Sprintf(" (%s)", strings.Join(choices, ", "))
	}
	fmt.Print(prompt + ": ")

	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	val := strings.TrimSpace(line)
	if val == "" {
		val = def
	}

	for _, c := range choices {
		if c == val {
			return val, nil
		}
	}
	return "", fmt.Errorf("%q is not a valid %s (choices: %s)", val, label, strings.Join(choices, ", "))
}

// promptNode lists every cluster node (unlike migrate's promptTargetNode,
// there's no "current node" to exclude — this is picking where a brand
// new container will live).
func promptNode(client *api.Client) (string, error) {
	names, err := clusterNodeNames(client)
	if err != nil {
		return "", err
	}
	return promptChoice("node", names)
}

// promptTemplate lists vztmpl templates found across node's storages.
func promptTemplate(client *api.Client, node string) (string, error) {
	storages := storageNamesForNode(client, node)
	templates, err := client.ListTemplates(context.Background(), node, storages)
	if err != nil {
		return "", fmt.Errorf("listing templates on %s: %w", node, err)
	}

	var volids []string
	for _, tpl := range templates {
		volids = append(volids, tpl.VolID)
	}
	return promptChoice("template", volids)
}

// promptRootfsStorage lists node's storages that actually support
// container root filesystems ("rootdir" content) — unlike promptStorage
// (used for backup's vzdump destination), which lists every storage on
// the node regardless of content type. Proxmox rejects a storage that
// doesn't support "rootdir" with "does not support container
// directories," found the hard way against a real cluster where "local"
// only serves ISOs/templates/backups and "local-lvm" is the actual
// rootdir-capable storage — filtering here means that error can't happen
// from the interactive prompt.
func promptRootfsStorage(client *api.Client, node string) (string, error) {
	storages, err := client.ListNodeStorages(context.Background(), node)
	if err != nil {
		return "", fmt.Errorf("listing storages on %s: %w", node, err)
	}

	var names []string
	for _, s := range storages {
		if s.SupportsContent("rootdir") {
			names = append(names, s.Storage)
		}
	}
	sort.Strings(names)
	return promptChoice("storage", names)
}

// promptHostname prompts for a required hostname — no list to show, so
// unlike promptChoice this just rejects an empty reply outright.
func promptHostname() (string, error) {
	fmt.Print("hostname: ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	hostname := strings.TrimSpace(line)
	if hostname == "" {
		return "", fmt.Errorf("hostname is required")
	}
	return hostname, nil
}

// promptYesNo prints prompt, then reads a line and reports whether it was
// "y" or "yes" (case-insensitive) — anything else, including an empty
// reply, is "no". Matches the [y/N] convention (empty defaults to no).
func promptYesNo(prompt string) bool {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes"
}

// runCtCreate resolves node/template/storage/hostname/vmid (via flags,
// falling back to interactive prompts in that order — node first, since
// template and storage listings both need it), creates the container,
// and optionally starts it. There's no [name-or-vmid] argument or
// resolveContainer step here, unlike every other ct action — create has
// no existing guest to select. startFlagSet is cmd.Flags().Changed("start")
// from the caller, passed as a plain bool (rather than threading
// *cobra.Command through) so this stays directly callable from tests,
// matching runCtMigrate's plain-argument style.
func runCtCreate(client *api.Client, startFlagSet bool) error {
	node := ctCreateNode
	if node == "" {
		var err error
		node, err = promptNode(client)
		if err != nil {
			return err
		}
	}

	template := ctCreateTemplate
	if template == "" {
		var err error
		template, err = promptTemplate(client, node)
		if err != nil {
			return err
		}
	}

	storage := ctCreateStorage
	if storage == "" {
		var err error
		storage, err = promptRootfsStorage(client, node)
		if err != nil {
			return err
		}
	}

	hostname := ctCreateHostname
	if hostname == "" {
		var err error
		hostname, err = promptHostname()
		if err != nil {
			return err
		}
	}

	vmid := ctCreateVMID
	if vmid == 0 {
		var err error
		vmid, err = client.NextID(context.Background())
		if err != nil {
			return fmt.Errorf("assigning next free vmid: %w", err)
		}
	}

	var sshKeys string
	if ctCreateSSHKeyFile != "" {
		data, err := os.ReadFile(ctCreateSSHKeyFile)
		if err != nil {
			return fmt.Errorf("reading ssh public key file: %w", err)
		}
		sshKeys = strings.TrimSpace(string(data))
	}

	params := api.CreateContainerParams{
		VMID:          vmid,
		OSTemplate:    template,
		Hostname:      hostname,
		Storage:       storage,
		DiskSizeGB:    ctCreateDiskSize,
		Cores:         ctCreateCores,
		MemoryMB:      ctCreateMemory,
		SwapMB:        ctCreateSwap,
		Net0:          ctCreateNet0,
		Unprivileged:  ctCreateUnprivileged,
		Features:      ctCreateFeatures,
		Arch:          ctCreateArch,
		Password:      ctCreatePassword,
		SSHPublicKeys: sshKeys,
		Tags:          api.NormalizeTags(ctCreateTags),
	}

	upid, err := client.CreateContainer(context.Background(), node, params)
	if err != nil {
		return fmt.Errorf("creating container %s (%d): %w", hostname, vmid, err)
	}
	if err := runProgressAction(client, node, upid,
		fmt.Sprintf("creating container %s (%d)", hostname, vmid),
		fmt.Sprintf("created container %s (%d)", hostname, vmid)); err != nil {
		return err
	}

	start := ctCreateStart
	if !startFlagSet {
		start = promptYesNo(fmt.Sprintf("start container %s (%d) now? [y/N]: ", hostname, vmid))
	}
	if !start {
		return nil
	}

	startUpid, err := client.Start(context.Background(), node, vmid)
	if err != nil {
		return fmt.Errorf("starting %s (%d): %w", hostname, vmid, err)
	}
	return runProgressAction(client, node, startUpid,
		fmt.Sprintf("starting %s (%d)", hostname, vmid),
		fmt.Sprintf("started %s (%d)", hostname, vmid))
}
