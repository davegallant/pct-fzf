package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/davegallant/pvectl/internal/api"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	qmCreateNode       string
	qmCreateName       string
	qmCreateVMID       int
	qmCreateCores      int
	qmCreateMemory     int
	qmCreateStorage    string
	qmCreateDiskSize   int
	qmCreateNet0       string
	qmCreateSCSIHW     string
	qmCreateOSType     string
	qmCreateISO        string
	qmCreateTags       string
	qmCreateCIUser     string
	qmCreateCIPassword string
	qmCreateSSHKeyFile string
	qmCreateIPConfig0  string
	qmCreateStart      bool
)

var qmCreateCmd = &cobra.Command{
	Use:         "create",
	Short:       "Create a new QEMU VM",
	Annotations: mutationAnnotation(mutationMutating),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := loadClient()
		if err != nil {
			return friendlySetupError(err)
		}
		return runQmCreate(client, cmd.Flags().Changed("start"))
	},
}

func init() {
	qmCreateCmd.Flags().StringVar(&qmCreateNode, "node", "", "node to create the VM on (prompts if omitted)")
	qmCreateCmd.Flags().StringVar(&qmCreateName, "name", "", "VM name (prompts if omitted)")
	qmCreateCmd.Flags().IntVar(&qmCreateVMID, "vmid", 0, "VM ID (0 = auto-assign the next free ID)")
	qmCreateCmd.Flags().IntVar(&qmCreateCores, "cores", 1, "CPU cores")
	qmCreateCmd.Flags().IntVar(&qmCreateMemory, "memory", 2048, "memory in MB")
	qmCreateCmd.Flags().StringVar(&qmCreateStorage, "storage", "", "storage for the VM's disk (prompts if omitted)")
	qmCreateCmd.Flags().IntVar(&qmCreateDiskSize, "disk-size", 32, "disk size in GB")
	qmCreateCmd.Flags().StringVar(&qmCreateNet0, "net0", "virtio,bridge=vmbr0", "network interface config (Proxmox net0 syntax)")
	qmCreateCmd.Flags().StringVar(&qmCreateSCSIHW, "scsihw", "virtio-scsi-pci", "SCSI controller type")
	qmCreateCmd.Flags().StringVar(&qmCreateOSType, "ostype", "l26", "guest OS type, e.g. l26, win11 (see Proxmox docs)")
	qmCreateCmd.Flags().StringVar(&qmCreateISO, "iso", "", "ISO volid to attach as install media, e.g. local:iso/ubuntu-24.04.iso (optional; prompts if omitted — press enter to skip and create a disk-only VM)")
	qmCreateCmd.Flags().StringVar(&qmCreateTags, "tags", "", "comma-separated tags to apply, e.g. media,arr (optional)")
	qmCreateCmd.Flags().StringVar(&qmCreateCIUser, "ciuser", "", "cloud-init user to create (optional; implies a cloud-init drive, cannot be combined with --iso)")
	qmCreateCmd.Flags().StringVar(&qmCreateCIPassword, "cipassword", "", "cloud-init user's password; pass \"-\" to read it from stdin without echo (optional)")
	qmCreateCmd.Flags().StringVar(&qmCreateSSHKeyFile, "sshkeys", "", "path to an SSH public key file to authorize for the cloud-init user (optional)")
	qmCreateCmd.Flags().StringVar(&qmCreateIPConfig0, "ipconfig0", "", "cloud-init network config, e.g. ip=dhcp or ip=10.0.0.5/24,gw=10.0.0.1 (optional)")
	qmCreateCmd.Flags().BoolVar(&qmCreateStart, "start", false, "start the VM after creating it (prompts if omitted)")
	qmCmd.AddCommand(qmCreateCmd)
}

// promptVMName prompts for a required VM name — ct create's
// promptHostname mirror; same "no list to show, reject empty" shape.
func promptVMName() (string, error) {
	fmt.Print("name: ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	name := strings.TrimSpace(line)
	if name == "" {
		return "", fmt.Errorf("name is required")
	}
	return name, nil
}

// promptISO lists iso content found across node's storages and lets the
// user pick one to attach as install media — promptTemplate's (ct_create.go)
// mirror, but unlike a container's required template, a VM's ISO is
// optional: an empty reply skips it (disk-only VM) instead of selecting a
// default, and no ISOs on the node skips the prompt entirely rather than
// erroring like promptChoice would on an empty choice list.
func promptISO(client *api.Client, node string) (string, error) {
	storages := storageNamesForNode(client, node)
	isos, err := client.ListISOs(context.Background(), node, storages)
	if err != nil {
		return "", fmt.Errorf("listing ISOs on %s: %w", node, err)
	}
	if len(isos) == 0 {
		return "", nil
	}

	var volids []string
	for _, iso := range isos {
		volids = append(volids, iso.VolID)
	}

	fmt.Printf("iso [none] (%s): ", strings.Join(volids, ", "))
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	val := strings.TrimSpace(line)
	if val == "" {
		return "", nil
	}
	for _, v := range volids {
		if v == val {
			return val, nil
		}
	}
	return "", fmt.Errorf("%q is not a valid iso (choices: %s)", val, strings.Join(volids, ", "))
}

// setCloudInitFlags returns the names of whichever cloud-init flags are
// set, in flag-declaration order. Split out as a pure function so the
// --iso conflict message (which names the offending flags) is unit-
// testable without a client or a terminal.
func setCloudInitFlags(user, password, sshKeyFile, ipconfig string) []string {
	var set []string
	for _, f := range []struct{ name, value string }{
		{"--ciuser", user},
		{"--cipassword", password},
		{"--sshkeys", sshKeyFile},
		{"--ipconfig0", ipconfig},
	} {
		if f.value != "" {
			set = append(set, f.name)
		}
	}
	return set
}

// resolveCIPassword implements --cipassword's "-" convention: read the
// password from the terminal without echo, so it never lands in shell
// history. Any other value is taken literally.
func resolveCIPassword(value string) (string, error) {
	if value != "-" {
		return value, nil
	}
	fmt.Print("cloud-init password: ")
	secret, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("reading cloud-init password: %w", err)
	}
	return string(secret), nil
}

// runQmCreate is runCtCreate's mirror for QEMU VMs: resolves node/storage/
// name/vmid/iso (flag first, prompt if omitted), creates the VM, and
// optionally starts it. promptImagesStorage (qm_actions.go, "images"
// content) is reused as-is rather than duplicated, since it already lists
// exactly the storages a QEMU disk can live on.
func runQmCreate(client *api.Client, startFlagSet bool) error {
	// Checked before any prompting or API call so a bad flag combination
	// fails instantly rather than after the user has answered prompts.
	ciFlags := setCloudInitFlags(qmCreateCIUser, qmCreateCIPassword, qmCreateSSHKeyFile, qmCreateIPConfig0)
	if qmCreateISO != "" && len(ciFlags) > 0 {
		return fmt.Errorf("--iso cannot be combined with %s: an ISO and a cloud-init drive both occupy ide2, and a cloud-init VM boots an imported cloud image rather than installing from an ISO",
			strings.Join(ciFlags, ", "))
	}

	node := qmCreateNode
	if node == "" {
		var err error
		node, err = promptNode(client)
		if err != nil {
			return err
		}
	}

	storage := qmCreateStorage
	if storage == "" {
		var err error
		storage, err = promptImagesStorage(client, node)
		if err != nil {
			return err
		}
	}

	name := qmCreateName
	if name == "" {
		var err error
		name, err = promptVMName()
		if err != nil {
			return err
		}
	}

	vmid := qmCreateVMID
	if vmid == 0 {
		var err error
		vmid, err = client.NextID(context.Background())
		if err != nil {
			return fmt.Errorf("assigning next free vmid: %w", err)
		}
	}

	// The ISO prompt is skipped entirely when cloud-init flags are in
	// play — the two are mutually exclusive (see above), so there'd be
	// nothing valid to pick.
	iso := qmCreateISO
	if iso == "" && len(ciFlags) == 0 {
		var err error
		iso, err = promptISO(client, node)
		if err != nil {
			return err
		}
	}

	var sshKeys string
	if qmCreateSSHKeyFile != "" {
		data, err := os.ReadFile(qmCreateSSHKeyFile)
		if err != nil {
			return fmt.Errorf("reading ssh public key file: %w", err)
		}
		sshKeys = strings.TrimSpace(string(data))
	}

	ciPassword, err := resolveCIPassword(qmCreateCIPassword)
	if err != nil {
		return err
	}

	params := api.CreateVMParams{
		VMID:       vmid,
		Name:       name,
		Cores:      qmCreateCores,
		MemoryMB:   qmCreateMemory,
		Storage:    storage,
		DiskSizeGB: qmCreateDiskSize,
		Net0:       qmCreateNet0,
		SCSIHW:     qmCreateSCSIHW,
		OSType:     qmCreateOSType,
		ISO:        iso,
		Tags:       api.NormalizeTags(qmCreateTags),
		CIUser:     qmCreateCIUser,
		CIPassword: ciPassword,
		SSHKeys:    sshKeys,
		IPConfig0:  qmCreateIPConfig0,
	}

	upid, err := client.CreateVM(context.Background(), node, params)
	if err != nil {
		return fmt.Errorf("creating VM %s (%d): %w", name, vmid, err)
	}
	if err := runProgressAction(client, node, upid,
		fmt.Sprintf("creating VM %s (%d)", name, vmid),
		fmt.Sprintf("created VM %s (%d)", name, vmid)); err != nil {
		return err
	}

	start := qmCreateStart
	if !startFlagSet {
		start = promptYesNo(fmt.Sprintf("start VM %s (%d) now? [y/N]: ", name, vmid))
	}
	if !start {
		return nil
	}

	startUpid, err := client.StartVM(context.Background(), node, vmid)
	if err != nil {
		return fmt.Errorf("starting %s (%d): %w", name, vmid, err)
	}
	return runProgressAction(client, node, startUpid,
		fmt.Sprintf("starting %s (%d)", name, vmid),
		fmt.Sprintf("started %s (%d)", name, vmid))
}
