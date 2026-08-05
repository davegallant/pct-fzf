package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/davegallant/pvectl/internal/api"
	"github.com/davegallant/pvectl/internal/ssh"
	"github.com/spf13/cobra"
)

func runStart(client *api.Client, c api.Container) error {
	upid, err := client.Start(context.Background(), c.Node, c.VMID)
	if err != nil {
		return fmt.Errorf("starting %s (%d): %w", c.Name, c.VMID, err)
	}
	return runProgressAction(client, c.Node, upid,
		fmt.Sprintf("starting %s (%d)", c.Name, c.VMID),
		fmt.Sprintf("started %s (%d)", c.Name, c.VMID))
}

func runStop(client *api.Client, c api.Container) error {
	upid, err := client.Stop(context.Background(), c.Node, c.VMID)
	if err != nil {
		return fmt.Errorf("stopping %s (%d): %w", c.Name, c.VMID, err)
	}
	return runProgressAction(client, c.Node, upid,
		fmt.Sprintf("stopping %s (%d)", c.Name, c.VMID),
		fmt.Sprintf("stopped %s (%d)", c.Name, c.VMID))
}

// runShutdown is runStop's graceful counterpart — see api.Shutdown for the
// stop-vs-shutdown distinction.
func runShutdown(client *api.Client, c api.Container) error {
	upid, err := client.Shutdown(context.Background(), c.Node, c.VMID)
	if err != nil {
		return fmt.Errorf("shutting down %s (%d): %w", c.Name, c.VMID, err)
	}
	return runProgressAction(client, c.Node, upid,
		fmt.Sprintf("shutting down %s (%d)", c.Name, c.VMID),
		fmt.Sprintf("shut down %s (%d)", c.Name, c.VMID))
}

func runReboot(client *api.Client, c api.Container) error {
	upid, err := client.Reboot(context.Background(), c.Node, c.VMID)
	if err != nil {
		return fmt.Errorf("rebooting %s (%d): %w", c.Name, c.VMID, err)
	}
	return runProgressAction(client, c.Node, upid,
		fmt.Sprintf("rebooting %s (%d)", c.Name, c.VMID),
		fmt.Sprintf("rebooted %s (%d)", c.Name, c.VMID))
}

// runUnlock clears c's lock unconditionally — unlike destroy there's
// nothing to confirm, matching pct's own unlock (a plain "clear whatever's
// stuck" operation, not a destructive one). Goes over SSH rather than the
// API client (unused here, like runAppendConfig's _ *api.Client) — see
// ssh.Unlock's doc comment for why there's no API path.
func runUnlock(_ *api.Client, c api.Container) error {
	if err := ssh.Unlock(c.Node, c.VMID); err != nil {
		return fmt.Errorf("unlocking %s (%d): %w", c.Name, c.VMID, err)
	}
	fmt.Printf("unlocked %s (%d)\n", c.Name, c.VMID)
	return nil
}

// ctTemplateYes backs `ct template`'s `-y`/`--yes` flag, same convention
// as ctDeleteYes.
var ctTemplateYes bool

// runTemplate converts c into a template. Like runDeleteContainer, this
// requires the user to type "yes" before proceeding — there's no
// supported way back to a regular container (see api.TemplateContainer) —
// unless skipped via -y/--yes. Prints its own success line rather than
// going through runProgressAction since the underlying API call is
// synchronous, not a pollable Proxmox task.
func runTemplate(client *api.Client, c api.Container) error {
	fmt.Printf("about to convert container %s (%d) to a template — this cannot be undone\n", c.Name, c.VMID)
	if !ctTemplateYes {
		fmt.Print("type 'yes' to confirm: ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		if strings.TrimSpace(line) != "yes" {
			fmt.Println("aborted, container not converted")
			return nil
		}
	}
	if err := client.TemplateContainer(context.Background(), c.Node, c.VMID); err != nil {
		return fmt.Errorf("converting %s (%d) to a template: %w", c.Name, c.VMID, err)
	}
	fmt.Printf("converted %s (%d) to a template\n", c.Name, c.VMID)
	return nil
}

// ctSnapshotName backs `ct snapshots create`'s `--name` flag, which skips
// the interactive name prompt.
var ctSnapshotName string

func runSnapshot(client *api.Client, c api.Container) error {
	name := ctSnapshotName
	if name == "" {
		fmt.Print("snapshot-name: ")
		reader := bufio.NewReader(os.Stdin)
		nameLine, _ := reader.ReadString('\n')
		name = strings.TrimSpace(nameLine)
	}
	if name == "" {
		return fmt.Errorf("snapshot name required")
	}

	upid, err := client.Snapshot(context.Background(), c.Node, c.VMID, name)
	if err != nil {
		return fmt.Errorf("snapshotting %s (%d): %w", c.Name, c.VMID, err)
	}
	return runProgressAction(client, c.Node, upid,
		fmt.Sprintf("snapshotting %s (%d)", c.Name, c.VMID),
		fmt.Sprintf("snapshotted %s (%d)", c.Name, c.VMID))
}

// ctBackupStorage backs `ct backups create`'s `--storage` flag, which
// skips the interactive storage prompt.
var ctBackupStorage string

func runBackup(client *api.Client, c api.Container) error {
	storage := ctBackupStorage
	if storage == "" {
		storage = promptStorage(client, c.Node)
	}

	upid, err := client.Backup(context.Background(), c.Node, c.VMID, storage)
	if err != nil {
		return fmt.Errorf("backing up %s (%d): %w", c.Name, c.VMID, err)
	}
	return runProgressAction(client, c.Node, upid,
		fmt.Sprintf("backing up %s (%d)", c.Name, c.VMID),
		fmt.Sprintf("backed up %s (%d)", c.Name, c.VMID))
}

// runMigrateWithPrompt prompts for a target node interactively, then
// migrates c — used by `ct migrate <name-or-vmid>` when no `--target` is
// given. The direct `ct migrate <name-or-vmid> --target <node>` form
// (ctMigrateCmd in migrate.go) resolves the target some other way and
// calls runMigrate directly instead, so it never touches stdin.
func runMigrateWithPrompt(client *api.Client, c api.Container, targetStorage string) error {
	printRestartNotice(c)
	target, err := promptTargetNode(client, c.Node)
	if err != nil {
		return err
	}
	return runMigrate(client, c, target, targetStorage)
}

// runMigrate migrates c to target, optionally onto different storage
// there (targetStorage, empty for Proxmox's same-storage-ID default). The
// storage only shows up in the progress label when it was asked for, so
// the common same-storage migrate reads exactly as it did before.
func runMigrate(client *api.Client, c api.Container, target, targetStorage string) error {
	restart := c.Status == "running"
	label := fmt.Sprintf("migrating %s (%d) to %s", c.Name, c.VMID, target)
	if targetStorage != "" {
		label += fmt.Sprintf(" (storage %s)", targetStorage)
	}

	upid, err := client.Migrate(context.Background(), c.Node, c.VMID, target, restart, targetStorage)
	if err != nil {
		return fmt.Errorf("migrating %s (%d): %w", c.Name, c.VMID, err)
	}
	progressErr := runProgressAction(client, c.Node, upid, label,
		fmt.Sprintf("migrated %s (%d) to %s", c.Name, c.VMID, target))
	printTaskLogIfVerbose(client, c.Node, upid)
	return progressErr
}

// ctResizeDisk/ctResizeSize back `ct resize`'s `--disk`/`--size` flags.
var ctResizeDisk string
var ctResizeSize string

func runResize(client *api.Client, c api.Container) error {
	if ctResizeSize == "" {
		return fmt.Errorf("--size is required (e.g. +2G to grow by 2GB, or 10G to set the total size)")
	}

	upid, err := client.ResizeContainer(context.Background(), c.Node, c.VMID, ctResizeDisk, ctResizeSize)
	if err != nil {
		return fmt.Errorf("resizing %s (%d) disk %s: %w", c.Name, c.VMID, ctResizeDisk, err)
	}
	return runProgressAction(client, c.Node, upid,
		fmt.Sprintf("resizing %s (%d) disk %s to %s", c.Name, c.VMID, ctResizeDisk, ctResizeSize),
		fmt.Sprintf("resized %s (%d) disk %s to %s", c.Name, c.VMID, ctResizeDisk, ctResizeSize))
}

func runBackups(client *api.Client, c api.Container) error {
	return runListBackups(client, c.Node, c.VMID, c.Name)
}

// ctBackupsDeleteVolID/ctBackupsDeleteYes and ctSnapshotsDeleteName/
// ctSnapshotsDeleteYes back the `--volid`/`--name` and `-y`/`--yes` flags
// on `ct backups delete`/`ct snapshots delete`. Given together they skip
// the interactive listing/prompt and the "type yes" confirmation
// entirely, making deletion scriptable.
var ctBackupsDeleteVolID string
var ctBackupsDeleteYes bool
var ctSnapshotsDeleteName string
var ctSnapshotsDeleteYes bool
var ctSnapshotsRollbackName string
var ctSnapshotsRollbackYes bool

func runDeleteBackupAction(client *api.Client, c api.Container) error {
	return runDeleteBackup(client, c.Node, c.VMID, c.Name, ctBackupsDeleteVolID, ctBackupsDeleteYes)
}

// ctRestoreVolID/ctRestoreStorage/ctRestoreYes back `ct backups
// restore`'s `--volid`/`--storage`/`-y`/`--yes` flags, same convention as
// the backup/snapshot delete flags above.
var ctRestoreVolID string
var ctRestoreStorage string
var ctRestoreYes bool

// runRestoreBackupAction restores c in place from one of its own
// backups, resolving the target storage from ctRestoreStorage or an
// interactive prompt (content-filtered to "rootdir", like ct create's
// own storage prompt) first.
func runRestoreBackupAction(client *api.Client, c api.Container) error {
	storage := ctRestoreStorage
	if storage == "" {
		var err error
		storage, err = promptRootfsStorage(client, c.Node)
		if err != nil {
			return err
		}
	}
	return runRestoreBackup(client, c.Node, c.VMID, c.Name, "container", ctRestoreVolID, storage, ctRestoreYes, client.RestoreContainer)
}

// ctRestoreNode/ctRestoreVMID back `ct backups restore`'s `--node`/
// `--vmid` flags. --node's presence (not merely being non-empty — see
// cmd.Flags().Changed in runCtBackupsRestore) is the switch into
// disaster-recovery mode: restoring from any backup found on that node
// rather than an existing guest's own backups, for a guest that no
// longer exists to be resolved via resolveContainer.
var ctRestoreNode string
var ctRestoreVMID int

// runCtBackupsRestore is `ct backups restore [name-or-vmid]`'s RunE.
// Unlike every other simple action it can't use newSimpleActionCmd,
// since disaster-recovery mode deliberately has no existing guest to
// resolve — --node's presence branches before resolveContainer is ever
// called.
func runCtBackupsRestore(cmd *cobra.Command, args []string) error {
	nodeMode := cmd.Flags().Changed("node")
	if nodeMode && len(args) > 0 {
		return fmt.Errorf("cannot combine a name-or-vmid argument with --node")
	}

	client, err := loadClient()
	if err != nil {
		return friendlySetupError(err)
	}

	if nodeMode {
		return runRestoreFromNodeCT(client, ctRestoreNode)
	}

	c, err := resolveContainer(client, args)
	if err != nil {
		return err
	}
	return runRestoreBackupAction(client, c)
}

// runRestoreFromNodeCT is runCtBackupsRestore's disaster-recovery branch:
// lists every backup on node, filtered to LXC archives
// (filterBackupsByGuestType), and restores the chosen one via
// runRestoreFromNode.
func runRestoreFromNodeCT(client *api.Client, node string) error {
	backups, err := fetchAllBackups(client, node)
	if err != nil {
		return fmt.Errorf("listing backups on %s: %w", node, err)
	}
	backups = filterBackupsByGuestType(backups, "lxc")

	storage := ctRestoreStorage
	if storage == "" {
		var err error
		storage, err = promptRootfsStorage(client, node)
		if err != nil {
			return err
		}
	}

	return runRestoreFromNode(client, node, "container", backups, ctRestoreVMID, ctRestoreVolID, storage, ctRestoreYes, client.RestoreContainer,
		func(vmid int) (bool, error) { return containerExists(client, vmid) })
}

func runSnapshotsAction(client *api.Client, c api.Container) error {
	return runListSnapshots(client, c.Node, c.VMID, c.Name)
}

func runDeleteSnapshotAction(client *api.Client, c api.Container) error {
	return runDeleteSnapshot(client, c.Node, c.VMID, c.Name, ctSnapshotsDeleteName, ctSnapshotsDeleteYes)
}

func runRollbackSnapshotAction(client *api.Client, c api.Container) error {
	return runRollbackSnapshot(client, c.Node, c.VMID, c.Name, ctSnapshotsRollbackName, ctSnapshotsRollbackYes)
}

// newSimpleActionCmd builds a `ct <use> <name-or-vmid>` command: it acts
// directly on the named/vmid'd container (via resolveContainer/
// findContainer). level is required (not defaulted) so a newly added
// action can't accidentally ship unclassified in `pvectl schema` — see
// mutationSafe/mutationMutating/mutationDestructive in schema.go.
func newSimpleActionCmd(use, short, level string, run func(*api.Client, api.Container) error) *cobra.Command {
	return &cobra.Command{
		Use:               use + " <name-or-vmid>",
		Short:             short,
		Annotations:       mutationAnnotation(level),
		Args:              requireArgs("name-or-vmid"),
		ValidArgsFunction: completeContainerNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := loadClient()
			if err != nil {
				return friendlySetupError(err)
			}
			c, err := resolveContainer(client, args)
			if err != nil {
				return err
			}
			return run(client, c)
		},
	}
}

func init() {
	ctCmd.AddCommand(newSimpleActionCmd("start", "Start a container", mutationMutating, runStart))
	ctCmd.AddCommand(newSimpleActionCmd("stop", "Stop a container immediately (hard power-off, no graceful attempt)", mutationMutating, runStop))
	ctCmd.AddCommand(newSimpleActionCmd("shutdown", "Gracefully shut down a container (waits on the guest, times out if it never responds)", mutationMutating, runShutdown))
	ctCmd.AddCommand(newSimpleActionCmd("reboot", "Reboot a container", mutationMutating, runReboot))
	ctCmd.AddCommand(newSimpleActionCmd("unlock", "Clear a container's lock, left behind by a crashed or interrupted task (requires SSH)", mutationMutating, runUnlock))

	ctResizeCmd := newSimpleActionCmd("resize", "Grow a container disk (cannot shrink)", mutationMutating, runResize)
	ctResizeCmd.Flags().StringVar(&ctResizeDisk, "disk", "rootfs", `disk to resize (e.g. "rootfs", "mp0")`)
	ctResizeCmd.Flags().StringVar(&ctResizeSize, "size", "", `new size: "+2G" to grow by 2GB, or "10G" to set the total size (required)`)
	ctCmd.AddCommand(ctResizeCmd)

	ctTemplateCmd := newSimpleActionCmd("template", "Convert a container to a template (irreversible)", mutationDestructive, runTemplate)
	ctTemplateCmd.Flags().BoolVarP(&ctTemplateYes, "yes", "y", false, "skip the confirmation prompt")
	ctCmd.AddCommand(ctTemplateCmd)

	// Every other multi-verb resource (backups, snapshots) nests all of
	// its verbs — including creation — under the plural group command
	// (`ct backups create`/`ct snapshots create`), rather than exposing
	// creation as a bare top-level `ct backup`/`ct snapshot`. Keep that
	// pattern consistent here: there is no top-level `ct backup`/
	// `ct snapshot` command.
	backupsCmd := &cobra.Command{
		Use:   "backups",
		Short: "Manage a container's backups",
	}
	ctBackupCreateCmd := newSimpleActionCmd("create", "Create a backup", mutationMutating, runBackup)
	ctBackupCreateCmd.Flags().StringVar(&ctBackupStorage, "storage", "", "backup storage target (skips the interactive prompt when set, along with the name-or-vmid argument)")
	backupsCmd.AddCommand(ctBackupCreateCmd)
	backupsCmd.AddCommand(newSimpleActionCmd("list", "List a container's backups", mutationSafe, runBackups))
	ctBackupsDeleteCmd := newSimpleActionCmd("delete", "Delete one of a container's backups", mutationDestructive, runDeleteBackupAction)
	ctBackupsDeleteCmd.Flags().StringVar(&ctBackupsDeleteVolID, "volid", "", "backup volid to delete (skips the interactive listing/prompt when set, along with the name-or-vmid argument)")
	ctBackupsDeleteCmd.Flags().BoolVarP(&ctBackupsDeleteYes, "yes", "y", false, "skip the confirmation prompt")
	backupsCmd.AddCommand(ctBackupsDeleteCmd)

	ctBackupRestoreCmd := &cobra.Command{
		Use:               "restore [name-or-vmid]",
		Short:             "Restore a container from a backup",
		Annotations:       mutationAnnotation(mutationDestructive),
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeContainerNames,
		RunE:              runCtBackupsRestore,
	}
	ctBackupRestoreCmd.Flags().StringVar(&ctRestoreVolID, "volid", "", "backup volid to restore (skips the interactive listing/prompt when set)")
	ctBackupRestoreCmd.Flags().StringVar(&ctRestoreStorage, "storage", "", "target storage for the restored container (skips the interactive prompt when set)")
	ctBackupRestoreCmd.Flags().BoolVarP(&ctRestoreYes, "yes", "y", false, "skip the confirmation prompt")
	ctBackupRestoreCmd.Flags().StringVar(&ctRestoreNode, "node", "", "restore from any backup on this node instead of an existing container's own backups (disaster recovery — cannot be combined with a name-or-vmid argument)")
	ctBackupRestoreCmd.Flags().IntVar(&ctRestoreVMID, "vmid", 0, "target vmid for --node mode (default: the vmid recorded in the chosen backup)")
	backupsCmd.AddCommand(ctBackupRestoreCmd)

	ctCmd.AddCommand(backupsCmd)

	snapshotsCmd := &cobra.Command{
		Use:   "snapshots",
		Short: "Manage a container's snapshots",
	}
	ctSnapshotCreateCmd := newSimpleActionCmd("create", "Create a snapshot", mutationMutating, runSnapshot)
	ctSnapshotCreateCmd.Flags().StringVar(&ctSnapshotName, "snapshot-name", "", "snapshot name (skips the interactive prompt when set, along with the name-or-vmid argument)")
	snapshotsCmd.AddCommand(ctSnapshotCreateCmd)
	snapshotsCmd.AddCommand(newSimpleActionCmd("list", "List a container's snapshots", mutationSafe, runSnapshotsAction))
	ctSnapshotsDeleteCmd := newSimpleActionCmd("delete", "Delete one of a container's snapshots", mutationDestructive, runDeleteSnapshotAction)
	ctSnapshotsDeleteCmd.Flags().StringVar(&ctSnapshotsDeleteName, "snapshot-name", "", "snapshot name to delete (skips the interactive listing/prompt when set, along with the name-or-vmid argument)")
	ctSnapshotsDeleteCmd.Flags().BoolVarP(&ctSnapshotsDeleteYes, "yes", "y", false, "skip the confirmation prompt")
	snapshotsCmd.AddCommand(ctSnapshotsDeleteCmd)
	ctSnapshotsRollbackCmd := newSimpleActionCmd("rollback", "Roll back a container to one of its snapshots", mutationDestructive, runRollbackSnapshotAction)
	ctSnapshotsRollbackCmd.Flags().StringVar(&ctSnapshotsRollbackName, "snapshot-name", "", "snapshot name to roll back to (skips the interactive listing/prompt when set, along with the name-or-vmid argument)")
	ctSnapshotsRollbackCmd.Flags().BoolVarP(&ctSnapshotsRollbackYes, "yes", "y", false, "skip the confirmation prompt")
	snapshotsCmd.AddCommand(ctSnapshotsRollbackCmd)
	ctCmd.AddCommand(snapshotsCmd)
}
