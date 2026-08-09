# Changelog

## Unreleased

- Added cloud-init support to `pvectl qm create`: `--ciuser`, `--cipassword` (pass `-` to read it from stdin without echo), `--sshkeys <file>`, and `--ipconfig0`. Any of these provisions a cloud-init drive on `ide2`, which is why they cannot be combined with `--iso` — both want the same bus slot, and the conflict is rejected up front by flag name
- Added tag support: `--tags` on `pvectl ct create`/`pvectl qm create`, a `Tags` row in `ct summary`/`qm summary`, and a `TAGS` column in `ct list`/`qm list` shown only when something in the list is actually tagged. `-o json` always emits a `tags` array, empty when untagged. Tags are display-and-set only — there's deliberately no tag filter on list (see AGENTS.md on filtering flags). Setting or changing tags on an existing guest already works through `ct config edit`/`qm config edit`; *clearing* them does not, since deleting a line in `$EDITOR` is warned about rather than sent as a removal (a pre-existing limitation, see AGENTS.md)
- Added `--target-storage` to `pvectl ct migrate`/`pvectl qm migrate`: migrate to a node whose storage IDs differ from the source (e.g. `pvectl ct migrate 101 --target pve-apollo --target-storage local-lvm`), accepting either a single storage ID or a `source:target,...` mapping. Matches `pct migrate --target-storage`/`qm migrate --targetstorage`; for a running VM, `with-local-disks` is sent automatically so its local disks migrate live
- Added `pvectl qm config view`/`pvectl ct config view`: print a VM's/container's live config without opening `$EDITOR` (supports `-o json`)

## 0.3.0

- Added `pvectl api get/post/put/delete <path>`: a raw Proxmox API escape hatch for endpoints with no dedicated pvectl command (e.g. `pvectl api get /access/users`), with a repeatable `--data key=value` flag for parameters (query string on `get`, form body otherwise) and the response printed as raw JSON
- Added agent/scripting-friendly JSON output: a global `--output`/`-o` flag (`-o json`) makes `ct list`/`qm list`, `nodes list`, `storage list`, `tasks list`, `ct backups list`/`qm backups list`, `ct snapshots list`/`qm snapshots list`, and `ct summary`/`qm summary` print their data as JSON instead of a table/text; `pvectl schema` prints pvectl's full command tree (names, flags, descriptions) as JSON for introspection, with each command classified as `safe`, `mutating`, or `destructive` so an agent can gauge risk before calling something
- Added `pvectl ct template`/`pvectl qm template`: convert a container/VM to a template (irreversible — requires typing `yes` to confirm, or `-y`/`--yes` to skip), matching `pct template`/`qm template`
- Added `pvectl ct unlock`/`pvectl qm unlock`: clear a container's/VM's lock, left behind by a crashed or interrupted task. Runs over SSH (like `ct enter`/`ct config append`), since Proxmox's REST API has no way to remove a lock — `pct unlock`/`qm unlock` only ever run locally on the node
- Added `pvectl ct clone`/`pvectl qm clone`: clone a container/VM (full or linked), with flags for `--newid`, `--hostname`/`--name`, `--storage`, `--full`, `--target`, `--pool`, `--description`, and `--snapname`
- Added `pvectl ct resize`: grow a container disk (e.g. `pvectl ct resize myct --size +2G` to grow the rootfs)
- Added `pvectl qm resize`: grow a VM disk (e.g. `pvectl qm resize myvm --size +2G` to grow scsi0)
- Added --node flag to `pvectl ct list`/`pvectl qm list` to show only containers/VMs on a single node
- `pvectl ct delete`/`pvectl qm delete` renamed to `pvectl ct destroy`/`pvectl qm destroy`, matching native `pct destroy`/`qm destroy`; `delete` remains as an alias

## 0.2.0

- **BREAKING:** Removed the interactive fuzzy-picker/action-menu (`ct select`/`qm select` and the "no argument falls back to the picker" behavior everywhere it existed). `pvectl` is now a strict CLI — every `ct`/`qm` command that acts on a guest (`enter`, `edit`, `start`, `stop`, `reboot`, `backups create/list/delete/restore`, `snapshots create/list/delete/rollback`, `migrate`) now requires a `<name-or-vmid>` argument instead of accepting an optional one.
- **BREAKING:** `pvectl storage`, `pvectl nodes`, and `pvectl tasks` now map to `pvectl storage list`, `pvectl nodes list`, and `pvectl tasks list` respectively (each aliased `ls`)
- **BREAKING:** `ct stop`/`qm stop` are now an immediate hard power-off (Proxmox's `"stop"` action) instead of a graceful shutdown — matching what native `pct`/`qm stop` do. The previous graceful behavior is now `ct shutdown`/`qm shutdown` (Proxmox's `"shutdown"` action), which waits on the guest and times out if it never responds.
- Added `ct backups restore`/`qm backups restore`: restore a container or VM from a backup, either in place (from one of its own backups, always confirmed) or, with `--node`, from any backup found on a node for disaster recovery when the original guest no longer exists.
- Shell completion (`pvectl completion`) now suggests VM/container names for every `ct`/`qm` command's `<name-or-vmid>` argument, fetched live from the cluster on each Tab press.
- `pvectl ct enter`/`pvectl qm enter` gain an API-based console method (`--method api`, or set as default with `pvectl setup`) as an alternative to the default SSH path — opens Proxmox's termproxy websocket directly over the stored API token, so no SSH access to the node is required.
- Added `pvectl config view`: prints the on-disk config as YAML.
- `pvectl ct exec`: run a command inside a container non-interactively over SSH (`pvectl ct exec <name-or-vmid> -- <command...>`). Tab completion for the command's own arguments (e.g. `pvectl ct exec <ct> -- cat docker-comp<TAB>`) SSHes into the container to list matching remote paths.
- Added `pvectl ct summary` and `pvectl qm summary`
- Added `pvectl qm create`: provision a new QEMU VM, with flags for name, node, storage, disk/memory/cores, network, SCSI controller, OS type, optional ISO install media, and `--start`; prompts interactively for anything not passed as a flag.

## 0.1.0

- Initial release. `pvectl` talks to the Proxmox VE REST API from any machine (not just the Proxmox host) to fuzzy-find and manage LXC containers (`ct`) and QEMU VMs (`qm`).
- `ct`/`qm` actions: `start`, `stop`, `reboot`, `enter`, `migrate`, `rename`, `delete` (`-f/--force`, `--purge`, `-y/--yes`). Every action takes an optional `[name-or-vmid]` argument that skips the interactive picker.
- `ct create`: provision a new LXC container from a template, with flags for hostname, node, storage, disk/memory/swap/cores, network, SSH key/password, and `--start`; prompts interactively for anything not passed as a flag.
- `config edit`: round-trip a container's/VM's config in `$EDITOR`
- `snapshots create`/`list`/`delete`/`rollback` and `backups create`/`list`/`delete`.
- Read-only cluster commands: `status` (with `--watch`), `nodes`, `storage`, `tasks`.
- Interactive fuzzy picker + action menu (searchable, full-screen) backed by the Proxmox live config preview.
- Live spinner progress for async Proxmox tasks, polling to completion in both interactive and scripted (non-TTY) use; failed tasks exit non-zero.
- Secrets stored in the OS keychain, with a file-based fallback; non-secret config in `~/.config/pvectl/config.yaml`.
- `--debug` API request/response logging (never logs the token).
