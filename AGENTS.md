# AGENTS.md

Guidance for AI agents (and humans) working in this repo.

## What this is

`pvectl` (Proxmox VE control) is a Go CLI that talks to the Proxmox VE
REST API directly to manage Proxmox resources from any
machine — not just the Proxmox host. It replaced an earlier 77-line bash
script that wrapped `pct`/`fzf` and only worked locally on the Proxmox
host. Resource types live under their own subcommand: `ct` for LXC
containers, `qm` for QEMU VMs. Other resource types can be added later the
same way, without reshaping the top-level command.

Module path `github.com/davegallant/pvectl`, binary name `pvectl` (built from
`./cmd/pvectl`).

## Key documents

- `README.md` — user-facing install/setup/usage.
- `docs/cli/` — auto-generated flag-by-flag reference for every command
  (`cobra/doc`, via `tools/gendocs`). Regenerate with `just docs` after
  adding/changing any command or flag; don't hand-edit these files, they
  get overwritten. Commit the regenerated files alongside the code change
  that caused them, same as any other generated-and-committed artifact.

## History & design rationale

- Started as a 77-line bash script wrapping `pct`/`fzf` — worked
  only on the Proxmox host itself. Rewritten in Go against the REST API so
  it runs from any machine.
- REST API instead of shelling out to `pct`/`qm`: works from anywhere, not
  just the Proxmox host. `enter` is the one exception — there's no REST
  equivalent for an interactive console, so it still shells out to `ssh
  <node> pct enter <vmid>` / `qm terminal <vmid>`.
- `setup` validates credentials (`GET /version`) before writing anything to
  disk/keychain. Every other command fails fast pointing to `pvectl setup`
  if config/secret is missing, rather than auto-invoking setup itself.
- `edit` round-trips the *entire* returned config (not a curated subset),
  diffs it against the original, and `PUT`s with the original `digest` for
  optimistic concurrency — a digest mismatch surfaces as "config changed
  elsewhere, re-run edit," and the temp file is preserved on any failure so
  edits are never silently lost.
- `ct`/`qm` are a deliberate parallel tree, not a shared abstraction —
  trading some duplication for lower risk to the already-shipped, tested
  `ct` path when `qm` was added: `qm_select.go` parallels `select.go`
  (sharing only package-level helpers), `qemu.go` parallels `lxc.go` (same
  REST shapes, different paths), and `qm enter` uses `qm terminal` (works
  only with a serial console configured, its own Ctrl-O detach key, hinted
  before attaching — a real UX gap vs. `ct enter`, not something pvectl can
  paper over). `internal/editconf` is fully shared, since it has no
  guest-type-specific coupling. `VMConfig` has no
  `RawLXC`-equivalent field, since flat QEMU configs have nothing to strip
  before diffing.
- `status` hits two endpoints — `/cluster/resources` (per-node CPU/mem,
  containers/VMs/storage) and `/cluster/status` (node IPs, cluster
  name/quorum, absent for a standalone node) — merged by node name; all
  calls must succeed or `status` hard-errors, no partial report. Byte sizes
  use binary units with single-letter suffixes (`120G`, not `120GiB`).
- The storage table collapses same-named entries to one row only when
  `StorageResource.Shared` is true (genuinely cluster-wide storage like
  NFS/Ceph). Non-shared storage that merely shares a name across nodes
  (default `local`/`local-lvm`) is never collapsed and is disambiguated as
  `name@node` — an earlier name-only collapse silently dropped every node
  past the first for non-shared storage, reported by a user as
  `local`/`local-lvm` going missing on a multi-node cluster.
## Build/dev commands

Use the `justfile` recipes rather than raw `go` invocations where possible:

- `just build` — `go build -o pvectl ./cmd/pvectl`
- `just run *args` — `go run ./cmd/pvectl {{args}}`
- `just test` — `go test ./...`
- `just vet` — `go vet ./...`
- `just fmt` — `gofmt -l -w .`
- `just tidy` — `go mod tidy`
- `just install` — `go install ./cmd/pvectl`
- `just lint` — `golangci-lint run` (needs `golangci-lint` on `PATH`;
  provided by the Nix dev shell)
- `just check` — vet + lint + test + build; run this before considering a
  change done. `go vet`/`go build`/`go test` passing does NOT mean lint
  passes (e.g. errcheck catches unchecked return values vet misses) — CI
  (`.github/workflows/golangci-lint.yml`) lints every push/PR to `main`,
  so a lint failure caught late fails CI, not just review
- `just docs` — regenerate `docs/cli/` from the command tree; run this
  after adding/changing any command or flag
- `just clean` — remove build artifacts

`CGO_ENABLED=0` is exported repo-wide in the justfile — `pvectl` has no cgo
dependencies, so builds shouldn't need a C compiler.

If a sandbox/CI environment has a read-only default `GOPATH` or no C
compiler, prefix `go` commands with
`GOPATH=<writable-dir> GOMODCACHE=<writable-dir> CGO_ENABLED=0`. Only reach
for this if you actually hit a read-only-filesystem or missing-gcc error —
it's not needed on a normal dev machine.

## Testing philosophy

- `internal/api` — table-driven tests against `httptest.Server`, covering
  auth headers, resource listing, start/stop/reboot/snapshot calls, and
  the config GET/PUT digest flow including a simulated digest mismatch.
- `internal/config` / `internal/secrets` — config load/save tested against
  a temp directory; secrets tested via an in-memory fake keyring behind
  the `internal/secrets` interface, never the real OS keychain.
- `edit`'s temp-file parse/diff logic is unit tested directly as pure
  functions, independent of the actual `$EDITOR` exec call.
- `internal/ssh` and `$EDITOR` exec calls are integration-level and only
  practically verifiable against a real cluster/editor — there's no
  end-to-end CI test against a live Proxmox cluster. Don't try to make
  these pure-unit-testable at the cost of real coverage; keep the seam
  (interface/exec wrapper) mockable instead.

## Gotchas and non-obvious behavior

These were each found via live debugging against a real Proxmox cluster
(2-node, 42 LXC containers) and fixed — don't reintroduce them:

- **Nothing has a default timeout.** An unreachable host or a hung OS
  keychain call will otherwise block the CLI forever with no feedback.
  Current values: 30s HTTP client timeout (`internal/api`), 3s keyring
  timeout (`internal/secrets`). Preserve these when touching either
  package.
- **OS keychain is not guaranteed to be available** (e.g. no D-Bus Secret
  Service on some NixOS/KDE setups) — `pvectl setup` must fall back to a
  local file (`~/.config/pvectl/secrets.json`, mode `0600`) after the keyring
  times out, and record which backend was used in `config.yaml`
  (`secret_backend`) so later commands read from the right place directly
  instead of re-probing a keyring known not to work.
- **`GET /cluster/resources?type=vm`'s server-side filter is unreliable**
  — it returned an empty list on a real cluster despite real containers
  existing. Don't reintroduce this filter; `ListContainers` filters
  client-side on each resource's own `type` field instead.
- **Proxmox "Privilege Separation" token gotcha**: a freshly created API
  token (even one tied to `root`) has zero permissions until an ACL is
  explicitly granted, or Privilege Separation is unchecked at creation.
  This silently returns an empty resource list (not an auth error), so
  `pvectl setup` succeeds but every command shows "no containers found."
  `pvectl setup` prints a warning about this before prompting for anything;
  the README documents it too. Keep both in sync if this flow changes.
- **Raw `lxc.*` passthrough config lines** (cgroup rules, bind mounts —
  anything without a dedicated named Proxmox API parameter) come back
  from Proxmox as a nested array under one `lxc` field. `api.Config.RawLXC`
  renders these as individual `lxc.subkey: value` lines (matching the real
  conf file) in the `$EDITOR` view. **They are
  display-only, not editable** — `applyEdit` strips any `lxc.`-prefixed key
  before diffing so these lines are never sent back to `PutConfig` even if
  present in the edited text. See "Known limitations" below before
  changing this.
- **`ssh host 'command'` does not allocate a pty by default**, and
  `pct enter` needs a real pty to attach the container console. This is a
  general SSH behavior, not pvectl-specific — confirmed by reproducing with
  plain `ssh` directly. `internal/ssh` runs `ssh -t <node> pct enter <vmid>`
  to force pty allocation; don't drop the `-t`.
- **Storage content listing returns numeric fields as JSON strings for
  some storage types** — confirmed on `local-lvm` (LVM-thin):
  `GET /nodes/{node}/storage/{storage}/content`'s `ctime`/`size`/`vmid`
  came back as quoted strings instead of numbers, breaking a strict
  `int64`/`int` struct field (`json: cannot unmarshal string into...`).
  `internal/api/backup.go`'s `looseInt64` type accepts either a JSON
  number or a numeric string; reuse this pattern for any other numeric
  field from Proxmox's API that turns out to have the same inconsistency,
  rather than assuming a field's JSON type is stable across storage
  plugins.
- **The migrate API spells target-storage differently per guest type**:
  LXC's `/nodes/{node}/lxc/{vmid}/migrate` takes `target-storage`, QEMU's
  `/nodes/{node}/qemu/{vmid}/migrate` takes `targetstorage` (no dash) —
  matching `pct migrate --target-storage` vs `qm migrate --targetstorage`.
  pvectl exposes one `--target-storage` flag on both and translates.
  Moving a *running* VM's local disks to different storage also needs
  `with-local-disks=1`; `MigrateVM` sends it automatically alongside
  `targetstorage` when the VM is online (and must not send it when the VM
  is stopped, where Proxmox rejects it). The flag value is passed through
  unvalidated on purpose — Proxmox accepts both a bare storage ID and a
  `source:target,...` mapping, so the "is this in the target node's
  storage list" check that guards `--target` can't be applied here.
- **A Proxmox task's `exitstatus` can be a bare, undescriptive string**
  (e.g. `WARNINGS: 1` from a `vzcreate` task, no further detail in the
  status reply itself) — confirmed via a real `ct create` whose only
  "problem" was a systemd/nesting hint on an otherwise fully-created
  container. `renderTaskOutcome` (`cmd/progress.go`) now fetches and
  prints the task's real log (`printTaskLogLines`) whenever
  `status.ExitStatus != "OK"`, unconditionally — not gated on `--verbose`
  like migrate's own `printTaskLogIfVerbose` (which now calls the same
  shared helper for its success-path log dump). This applies to every
  action that goes through `runProgressAction`, not just `ct create`.
- **`WARNINGS: N` is Proxmox's non-fatal exit status, not a failure** —
  same real `ct create` above: the task's `exitstatus` was `WARNINGS: 1`
  rather than `OK`, but the container was fully created; Proxmox's own
  GUI shows this as a completed task with a warning icon, not a red
  failure. `TaskStatus.Failed()` (`internal/api/tasks.go`) used to treat
  *any* non-`OK` exit status as a failure everywhere (start/stop/backup/
  migrate/snapshot/create) — it now excludes `WARNINGS: N` specifically
  via the shared `TaskCompletedWithWarnings` helper, which
  `cmd/tasks.go`'s `taskStatusLabel` also uses (`warning: <reason>`
  instead of `failed: <reason>` in `pvectl tasks list`'s STATUS column) so the
  two "is this really a failure" checks can't drift apart. The log is
  still printed either way (see above), so the warning itself is never
  silently hidden — only the ✓/✗ and exit-code verdict changed.
- **A Proxmox API error can set both `message` and a structured `errors`
  map** — e.g. `Parameter verification failed.` as the message, with the
  actual "which field, why" detail only in `errors`. `apiError.Error()`
  used to return `message` alone whenever it was non-empty, silently
  dropping the actually-useful per-field detail (found via a real
  `ct create` that only ever showed the generic message). It now renders
  both when present, sorted by field for deterministic output.

## Known limitations / accepted trade-offs (deliberate, not bugs)

- Single Proxmox cluster — deliberate non-goal, no multi-cluster or
  standalone-host fan-out. (Resource-type coverage is not on this list —
  both `ct` and `qm` are supported, see "What this is" above.)
- No generic Proxmox config editor beyond what `edit` needs — no
  node-level or storage-level configuration.
- No filtering/sorting flags anywhere — add only if an actual need shows up.
- No packaging/distribution automation (Homebrew formula, GitHub releases).
- `status` only counts VMs; managing them is `qm`'s job.
- `pvectl ct config edit` cannot add/remove raw `lxc.*` passthrough lines
  (see above) — would require `Config.Fields` to support ordered/repeating
  keys (it's currently a plain map) to do this properly. Revisit only if
  actually needed, not opportunistically. `pvectl qm config edit` has no
  such limitation to begin with — `VMConfig` has no raw-passthrough block.
- `pvectl ct config edit` and `pvectl qm config edit` both cannot remove regular fields —
  deleting a line in `$EDITOR` is detected and warned about, but not sent
  as a removal (map-based-`Fields` limitation, plus Proxmox's PUT API
  handles field removal differently than a value change).
- `pvectl qm enter` only works if the VM has a serial console device
  configured — `qm terminal` itself errors otherwise, and that error is
  surfaced as-is (same "let SSH's own error output speak for itself"
  policy as `ct enter`). This is a real UX gap versus `ct enter` (which
  always works for LXC), not something pvectl can paper over.
- `pvectl qm exec` is deliberately not `ct exec`'s equal. `ct exec` streams
  live over SSH with a tty; `qm exec` goes through the QEMU guest agent,
  whose API is fire-and-poll (`POST .../agent/exec` returns a guest pid,
  then `GET .../agent/exec-status?pid=` is polled until `exited`). That
  means no stdin, no tty, and no output at all until the command exits —
  a property of Proxmox's API, not a pvectl gap, and documented rather
  than papered over with a fake stream. Two non-obvious details confirmed
  against PVE 9.2 rather than assumed: `command` must be sent as repeated
  `command=` params (one per argv element — a single joined string is
  rejected with HTTP 596), and the agent's *info* commands are `GET
  /agent/{cmd}` while `exec` alone is a POST. There's also no shell in the
  loop, so shell syntax needs an explicit `sh -c`; and no remote-path
  argument completion (ct exec gets that by SSHing to the node and running
  `ls`, which has no agent equivalent worth a round trip per Tab press).
- Guest-agent detection parses the config's `agent` property string via
  `api.AgentEnabled`, not a prefix check. Proxmox writes it three ways —
  `1`, `enabled=1`, or either followed by options like
  `1,fstrim_cloned_disks=1` — and its own GUI toggle produces `enabled=1`,
  which an earlier `strings.HasPrefix(field, "1")` in `qm summary` missed,
  reporting `Guest Agent no` for VMs that had it on. Both `qm summary` and
  `qm exec` share the one parser so the two can't drift.
- Tags are display-and-set only: `--tags` at create time, a row in
  `summary`, a column in `list`, and editable through `config edit` (which
  round-trips the whole config already). There is deliberately no
  `--tag` filter on `list` or any bulk operation — that would cross the
  "no filtering/sorting flags" line above. The `TAGS` column is rendered
  only when at least one guest in the result set is tagged, since most
  guests on a typical cluster are untagged and an always-on column would
  be near-empty whitespace.
- `pvectl qm create`'s cloud-init flags cannot be combined with `--iso`:
  the cloud-init drive and an install ISO both occupy `ide2`. Rejected up
  front by flag name rather than silently reassigning the bus, because a
  cloud-init VM boots an imported cloud image rather than installing from
  ISO — the two are different workflows, not two options on one. Note
  `sshkeys` is URL-encoded by `CreateVM` *before* form encoding escapes it
  again; Proxmox stores and expects that field double-encoded, and sending
  the raw key round-trips to a mangled `authorized_keys`. `escapeSSHKeys`
  exists rather than a bare `url.QueryEscape` because QueryEscape encodes a
  space as `+`, and Proxmox decodes this field with Perl's
  `URI::Escape::uri_unescape`, which expands `%XX` but leaves `+`
  alone — so every space in a key would arrive as a literal plus sign.
  Base64 `+` inside key material is already `%2B` by then, so rewriting
  `+`→`%20` is safe.
- The two `create` commands' SSH-key flags are named differently on
  purpose: `ct create --ssh-public-key-file` vs `qm create --sshkeys`.
  Each matches its own Proxmox API parameter (`ssh-public-keys` for LXC,
  `sshkeys` for QEMU), which are themselves differently named upstream.
  Matching Proxmox won out over ct/qm mirroring here, since a user
  cross-referencing Proxmox docs is the likelier case.
- `enter`'s SSH target is whatever Proxmox reports as the node's name
  (e.g. `pve-g3-2`) — pvectl does not store or manage SSH connection details
  itself; it relies entirely on the user's own `~/.ssh/config` (or DNS)
  resolving that name. This was an explicit design choice: reuse the
  user's SSH config rather than have pvectl manage per-node SSH settings.
- `pvectl ct create` still has no raw `lxc.*` config passthrough at create
  time (e.g. TUN/TAP device rules) — that's a separate, later step: `pvectl ct config append
  <name-or-vmid> --line "..."` (repeatable `--line`, one or more raw
  "lxc.subkey: value" lines). This SSHes `cat >> /etc/pve/lxc/<vmid>.conf`
  on the node (`ssh.AppendRawConfig`, `internal/ssh/ssh.go`) instead of
  going through `PutConfig`, because Proxmox's REST API does not expose
  raw `lxc.*` directives at all — confirmed by Proxmox maintainers on the
  forum, not a pvectl gap — the same reason `ct config edit` can only
  display (not save) these lines (see above). Lines are written over the SSH
  command's stdin rather than embedded in the command string, so no
  shell-quoting is needed for arbitrary raw config text. No digest/
  concurrency check (unlike `PutConfig`) since it's a raw file append, not
  a structured field update; changes need a container restart to take
  effect, same as editing the conf file by hand would. `edit` (`edit.go`/
  `qm_edit.go`) used to be a bare top-level `ct edit`/`qm edit` until
  `config append` was added and nested under `ct config`/`qm config` per
  explicit user request ("pvectl ct edit should be pvectl ct config edit
  ... same with pvectl qm edit") — `ctConfigCmd`/`qmConfigCmd` are
  package-level vars (`ct_config.go`/`qm_config.go`) specifically so
  `edit.go`/`qm_edit.go`'s own `init()` can register into them from a
  different file. `qmConfigCmd` exists purely to hold `edit` today (no
  `append` mirror — see above), not because `qm` needs its own config
  group otherwise.
- `qm destroy` (`cmd/qm_delete.go`, aliased `delete`) mirrors `ct destroy`
  (`--purge`, `-y`/`--yes`) but deliberately has
  no `--force`: Proxmox's own `DELETE .../qemu/{vmid}` has no
  force-destroy-while-running param the way `DELETE .../lxc/{vmid}` does
  (see `api.DeleteVM`'s comment) — a running VM must be stopped first,
  same as an LXC container deleted without `--force`.

## Conventions

- Prefer `just <recipe>` over raw `go` commands for build/test/vet/fmt so
  behavior (like `CGO_ENABLED=0`) stays consistent.
- `--debug` (persistent root flag) logs method/path/status/duration for
  every Proxmox API call to stderr. Never log the `Authorization` header
  or request body — preserve this if extending debug logging.
- Keep `internal/secrets` behind its current interface (`KeyringStore`/
  `FileStore`/fake) rather than calling the OS keychain directly, so tests
  can keep substituting an in-memory fake.
- Error handling: Proxmox's JSON error body is unwrapped into a plain Go
  error, not a raw HTTP status. A TLS certificate failure produces a
  specific hint to re-run `pvectl setup --insecure-skip-verify` for
  self-signed clusters. Missing credentials are checked up front for every
  command except `setup`, before any network call. SSH-based commands
  (`ct enter`, `qm enter`) propagate SSH's own exit code and let its error
  output speak for itself — no custom wrapping.
- Any command that unconditionally shells out to SSH (rather than SSH being
  just the default `--method`, like `ct enter`/`qm enter` which already say
  "via SSH" in their `Short`) must say so in its `Short` text, suffixed
  `(requires SSH)` — e.g. `ct exec`, `ct config append`, `ct unlock`,
  `qm unlock`. This is the only place these commands are labeled; there's no
  `Annotations`-based machine-readable tag or `--ssh-only` filter, since
  nothing consumes one today. Keep any new SSH-only command consistent with
  this rather than leaving it unlabeled.
- Parent commands never do implicit work — `ctCmd`, `qmCmd`, and
  `backupsCmd` (under each) have no `RunE` of their own and just print
  help when run bare. Every action, including listing, is an explicit
  child subcommand (`ct list`, `ct backups list`, etc.). This was an
  explicit user preference (`backups` used to list by default) — keep new
  resource types and new backup-like sub-trees consistent with this rather
  than reverting to a default action on the parent.
- Creation for a plural sub-tree nests under the group too — `ct/qm
  snapshots create`, `ct/qm backups create` — there is no bare top-level
  `ct snapshot`/`ct backup`. These used to be exceptions (creation lived
  at the top level while list/delete/rollback lived under the plural
  group) until an explicit user request to fix the inconsistency ("ct
  snapshot creates a snapshot but everything else is under ct snapshots
  ... fix this pattern in all commands"). Keep any new plural sub-tree's
  `create` nested from the start.
- `list` means a static, non-interactive listing. `pvectl` is a strict
  CLI with no full-screen interactive picker or action menu — there used
  to be one (`ct select`/`qm select`, backed by a bubbletea TUI in
  `internal/tui`), removed per explicit user request ("no full-screen
  interactive picker/menu, ever"). Don't reintroduce an interactive
  picker/menu for any future resource type; a plain `list` table is the
  only listing form.
- `stop` and `shutdown` are separate commands on both `ct` and `qm`,
  matching Proxmox's own `pct`/`qm` CLI split: `stop` (`api.Stop`/
  `api.StopVM`, API action `"stop"`) is an immediate hard power-off with no
  guest involvement; `shutdown` (`api.Shutdown`/`api.ShutdownVM`, API
  action `"shutdown"`) is graceful and waits on the guest, timing out if
  nothing acknowledges it (no OS, no ACPI support, unresponsive guest —
  common for a freshly-`qm create`d VM). Before this split, `stop` called
  the `"shutdown"` action, so a non-responsive guest's `stop` would hang
  and time out with no hard-power-off alternative in pvectl.
- Every `ct`/`qm` action command (`start`/`stop`/`shutdown`/`reboot`/`rename`/
  `snapshots create`/`snapshots list`/`snapshots delete`/
  `snapshots rollback`/`backups create`/`backups list`/`backups delete`/
  `config edit`/`config append`/`enter`/`migrate`) requires a
  `<name-or-vmid>` argument (`cobra.ExactArgs(1)`), resolved through
  `resolveContainer`/`resolveVM` (`select.go`/`qm_select.go`) →
  `findContainer`/`findVM`. `resolveContainer`/`resolveVM` are kept as
  their own functions (not inlined at every call site) specifically so
  one guard protects every caller, including `backups restore`'s
  non-`--node` branch — keep any new `ct`/`qm` action consistent with
  this rather than calling `findContainer`/`findVM` directly. `backups
  restore` is the one exception: it keeps `cobra.MaximumNArgs(1)` since
  its `--node` disaster-recovery mode legitimately takes zero args (see
  below). Downstream prompts specific to one action (backup's storage
  prompt, snapshot's name prompt) are a separate concern this convention
  doesn't cover.
- `ct create` (`cmd/ct_create.go`) is a deliberate exception to the above:
  there's no existing guest to act on, so it takes no `<name-or-vmid>`
  argument and never calls `resolveContainer`. Every other input
  (`--node`/`--template`/`--storage`/`--hostname`) instead follows the
  same flag-first, prompt-if-omitted shape as backup's storage prompt and
  migrate's target-node prompt — `--node`/`--template` list valid choices
  with the first shown as the default (`promptChoice` in `cmd/ct_create.go`,
  same bracket-default styling `migrate.go`'s `promptTargetNode` uses),
  `--storage` reuses `promptStorage` directly. `--vmid` defaults to
  Proxmox's next free ID (`Client.NextID`) rather than prompting.
- `qm create` (`cmd/qm_create.go`) mirrors `ct create` with one deliberate
  difference: QEMU has no template concept, so there's no required
  `--template` prompt step. `--iso` is a plain optional flag instead — when
  given, it's attached as `ide2` (cdrom) and the boot order tries it before
  the disk (`scsi0`); when omitted, no prompt fires and the VM is created
  disk-only (e.g. for a later disk import or cloud image workflow), unlike
  `ct create`'s template which is always required. `--storage` reuses
  `promptImagesStorage` (`cmd/qm_actions.go`, "images" content — already
  existed for `qm backups restore`) rather than duplicating
  `promptRootfsStorage`'s "rootdir"-content filtering, since a QEMU disk
  and an LXC rootfs need different storage content types.
- `ct destroy` (`cmd/ct_delete.go`, aliased `delete` — matches native
  `pct destroy`) follows the `<name-or-vmid>`/`resolveContainer`
  convention normally (unlike `ct create`, it acts on an existing guest),
  via `newSimpleActionCmd` same as start/stop/backup/snapshot. Where
  `ct destroy` deviates from the rest: its own `-y`/`--yes` + `--purge`
  flags follow ctBackupsDeleteYes/ctSnapshotsDeleteYes's "only the direct
  subcommand registers these, so the flag vars stay zero-valued when
  unused" pattern. Confirmation is the same "type 'yes', no bare 'y', no
  default-yes" discipline as `backups delete`/`snapshots delete`.
- Every new feature gets a bullet under `## Unreleased` in `CHANGELOG.md`
  (create that heading above the latest version if it doesn't exist yet).
  Bug fixes/internal refactors don't need an entry unless user-visible.
- Default to direct implementation over planning. When asked to build a
  feature, make a change, or answer a question, take the concrete action
  immediately and verify it works — do not write a design spec, launch a
  slow background research agent, or defer to a CI workflow unless
  explicitly asked for those. If you truly hit an ambiguous fork, ask one
  crisp question rather than exploring on your own. For simple factual
  questions (like config flags), answer directly from your knowledge or a
  single targeted lookup, not a broad filesystem grep.
