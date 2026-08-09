package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// VM is a QEMU virtual machine as returned by /cluster/resources.
type VM struct {
	VMID   int    `json:"vmid"`
	Name   string `json:"name"`
	Node   string `json:"node"`
	Status string `json:"status"`
	// Tags is never nil — see Container.Tags.
	Tags []string `json:"tags"`
}

// ListVMs returns every QEMU VM across the cluster, sorted by VMID
// ascending — mirrors ListContainers's ordering guarantee (and reason:
// /cluster/resources doesn't guarantee any particular order).
func (c *Client) ListVMs(ctx context.Context) ([]VM, error) {
	entries, err := c.fetchResources(ctx)
	if err != nil {
		return nil, err
	}

	var vms []VM
	for _, r := range entries {
		if r.Type != "qemu" {
			continue
		}
		vms = append(vms, VM{
			VMID:   r.VMID,
			Name:   r.Name,
			Node:   r.Node,
			Status: r.Status,
			Tags:   parseTags(r.Tags),
		})
	}
	sort.Slice(vms, func(i, j int) bool {
		return vms[i].VMID < vms[j].VMID
	})
	return vms, nil
}

// StartVM, StopVM, ShutdownVM, RebootVM, and SnapshotVM return the Proxmox
// task UPID for the triggered action so callers can poll TaskStatus for
// completion, instead of only firing the request and returning — mirrors
// the LXC side (lxc.go's Start/Stop/Shutdown/Reboot/Snapshot) exactly.

func (c *Client) StartVM(ctx context.Context, node string, vmid int) (string, error) {
	return c.statusVMAction(ctx, node, vmid, "start")
}

// StopVM maps to Proxmox's own `qm stop` / API "stop" action: an immediate
// hard power-off with no ACPI/guest involvement, same as pulling the plug.
// Use ShutdownVM for a graceful ACPI shutdown that waits on the guest.
func (c *Client) StopVM(ctx context.Context, node string, vmid int) (string, error) {
	return c.statusVMAction(ctx, node, vmid, "stop")
}

// ShutdownVM maps to Proxmox's own `qm shutdown` / API "shutdown" action: a
// graceful ACPI shutdown request that waits on the guest OS to power
// itself off, timing out if nothing (no OS, no ACPI support, unresponsive
// guest) ever acknowledges it. Use StopVM for an immediate hard power-off.
func (c *Client) ShutdownVM(ctx context.Context, node string, vmid int) (string, error) {
	return c.statusVMAction(ctx, node, vmid, "shutdown")
}

func (c *Client) RebootVM(ctx context.Context, node string, vmid int) (string, error) {
	return c.statusVMAction(ctx, node, vmid, "reboot")
}

func (c *Client) statusVMAction(ctx context.Context, node string, vmid int, action string) (string, error) {
	return c.postUPID(ctx, fmt.Sprintf("/nodes/%s/qemu/%d/status/%s", node, vmid, action), nil)
}

// MigrateVM moves a VM from node to target, returning the Proxmox task
// UPID. online should be true when the VM is currently running — Proxmox
// performs a true live migration (memory copied while the guest keeps
// running), unlike Migrate's LXC restart-based approach. A stopped VM
// needs no online flag; Proxmox just relocates its config/disks.
// targetStorage is `qm migrate --targetstorage` — see Migrate's comment
// for the accepted forms; QEMU spells the parameter without the LXC
// side's dash. Migrating a *running* VM's local disks onto different
// storage is a live storage migration, which Proxmox only performs when
// `with-local-disks` is set too, so that's sent alongside rather than
// left to the caller (the web UI does the same).
func (c *Client) MigrateVM(ctx context.Context, node string, vmid int, target string, online bool, targetStorage string) (string, error) {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/migrate", node, vmid)
	form := url.Values{"target": {target}}
	if online {
		form.Set("online", "1")
	}
	if targetStorage != "" {
		form.Set("targetstorage", targetStorage)
		if online {
			form.Set("with-local-disks", "1")
		}
	}
	return c.postUPID(ctx, path, strings.NewReader(form.Encode()))
}

// CreateVMParams holds the qm-create parameters `pvectl qm create`
// exposes. Unlike CreateContainerParams's OSTemplate, ISO is optional — a
// VM can be created disk-only with no boot media attached (e.g. for a
// later disk import or cloud image), so an empty ISO means no ide2 device
// and a boot order with just the disk.
type CreateVMParams struct {
	VMID       int
	Name       string
	Cores      int
	MemoryMB   int
	Storage    string
	DiskSizeGB int
	Net0       string
	SCSIHW     string
	OSType     string
	ISO        string
	// Tags is the semicolon-separated form Proxmox expects; callers pass
	// user input through NormalizeTags first.
	Tags string
	// Cloud-init settings. Any of these being set makes the VM
	// cloud-init-enabled, which provisions a cloudinit drive on ide2 —
	// mutually exclusive with ISO, which wants the same bus slot. The
	// command layer rejects that combination before reaching here.
	CIUser     string
	CIPassword string
	SSHKeys    string
	IPConfig0  string
}

// HasCloudInit reports whether any cloud-init parameter is set, i.e.
// whether CreateVM will provision a cloudinit drive. Exported so the
// command layer can enforce the ISO mutual exclusion against exactly the
// same condition CreateVM acts on, rather than a second, drifting copy.
func (p CreateVMParams) HasCloudInit() bool {
	return p.CIUser != "" || p.CIPassword != "" || p.SSHKeys != "" || p.IPConfig0 != ""
}

// escapeSSHKeys percent-encodes an authorized_keys blob the way Proxmox
// expects the sshkeys parameter: the field is stored URL-encoded, so the
// value is escaped here *before* form encoding escapes it again. Sending
// the raw key round-trips to a mangled authorized_keys.
//
// url.QueryEscape alone is wrong. It encodes a space as "+", but Proxmox
// decodes this field with Perl's URI::Escape::uri_unescape, which only
// expands %XX and leaves "+" untouched — so every space in a key (the two
// separating the type, the base64 body, and the comment) would arrive as
// a literal plus sign. Rewriting "+" to %20 keeps spaces intact; the "+"
// characters that legitimately occur inside base64 key material are
// already encoded as %2B by QueryEscape, so they are not affected by the
// rewrite.
func escapeSSHKeys(keys string) string {
	return strings.ReplaceAll(url.QueryEscape(keys), "+", "%20")
}

// CreateVM creates a new QEMU VM on node, returning the Proxmox task
// UPID — mirrors CreateContainer, except the disk is a scsi0 volume
// instead of a rootfs and boot media is optional rather than a required
// ostemplate. When ISO is set, it's attached as ide2 (cdrom) and the boot
// order tries it before the disk, matching how Proxmox's own GUI wizard
// orders a fresh install.
func (c *Client) CreateVM(ctx context.Context, node string, p CreateVMParams) (string, error) {
	path := fmt.Sprintf("/nodes/%s/qemu", node)
	form := url.Values{
		"vmid":   {strconv.Itoa(p.VMID)},
		"name":   {p.Name},
		"cores":  {strconv.Itoa(p.Cores)},
		"memory": {strconv.Itoa(p.MemoryMB)},
		"scsi0":  {fmt.Sprintf("%s:%d", p.Storage, p.DiskSizeGB)},
		"net0":   {p.Net0},
		"scsihw": {p.SCSIHW},
		"ostype": {p.OSType},
	}
	switch {
	case p.ISO != "":
		form.Set("ide2", fmt.Sprintf("%s,media=cdrom", p.ISO))
		form.Set("boot", "order=ide2;scsi0")
	case p.HasCloudInit():
		// A cloud-init VM boots the (already provisioned) disk; the
		// cloudinit drive is config data Proxmox regenerates, not boot
		// media, so it stays out of the boot order.
		form.Set("ide2", fmt.Sprintf("%s:cloudinit", p.Storage))
		form.Set("boot", "order=scsi0")
	default:
		form.Set("boot", "order=scsi0")
	}
	if p.CIUser != "" {
		form.Set("ciuser", p.CIUser)
	}
	if p.CIPassword != "" {
		form.Set("cipassword", p.CIPassword)
	}
	if p.SSHKeys != "" {
		form.Set("sshkeys", escapeSSHKeys(p.SSHKeys))
	}
	if p.IPConfig0 != "" {
		form.Set("ipconfig0", p.IPConfig0)
	}
	if p.Tags != "" {
		form.Set("tags", p.Tags)
	}
	return c.postUPID(ctx, path, strings.NewReader(form.Encode()))
}

// CloneVMParams holds the qm-clone parameters `pvectl qm clone` exposes —
// mirrors CloneContainerParams exactly, except Name replaces Hostname
// (QEMU VMs have a Name, not a Hostname). Full is sent as "1" only when
// true — omitted otherwise, so Proxmox falls back to its own default
// (full copy for a normal VM, linked clone for a template), matching
// qm's own behavior.
type CloneVMParams struct {
	TargetVMID  int
	Name        string
	Storage     string
	Full        bool
	Target      string
	Pool        string
	Description string
	SnapName    string
}

// CloneVM clones vmid on node into a new VM p.TargetVMID, returning the
// Proxmox task UPID — mirrors CloneContainer exactly.
func (c *Client) CloneVM(ctx context.Context, node string, vmid int, p CloneVMParams) (string, error) {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/clone", node, vmid)
	form := url.Values{"newid": {strconv.Itoa(p.TargetVMID)}}
	if p.Name != "" {
		form.Set("name", p.Name)
	}
	if p.Storage != "" {
		form.Set("storage", p.Storage)
	}
	if p.Full {
		form.Set("full", "1")
	}
	if p.Target != "" {
		form.Set("target", p.Target)
	}
	if p.Pool != "" {
		form.Set("pool", p.Pool)
	}
	if p.Description != "" {
		form.Set("description", p.Description)
	}
	if p.SnapName != "" {
		form.Set("snapname", p.SnapName)
	}
	return c.postUPID(ctx, path, strings.NewReader(form.Encode()))
}

// RestoreVM restores archive (a backup volid, as returned by
// ListBackups/ListAllBackups) onto vmid on node, returning the Proxmox
// task UPID — mirrors RestoreContainer exactly, except QEMU's restore
// endpoint takes the archive as `archive` (no `ostemplate`/`restore=1`
// pair like LXC). force must be true to overwrite a vmid that already
// exists; Proxmox rejects the restore otherwise.
func (c *Client) RestoreVM(ctx context.Context, node string, vmid int, archive, storage string, force bool) (string, error) {
	path := fmt.Sprintf("/nodes/%s/qemu", node)
	form := url.Values{
		"vmid":    {strconv.Itoa(vmid)},
		"archive": {archive},
		"storage": {storage},
	}
	if force {
		form.Set("force", "1")
	}
	return c.postUPID(ctx, path, strings.NewReader(form.Encode()))
}

// ResizeVM grows disk (e.g. "scsi0", "virtio0") on vmid — like `qm resize`,
// this only grows a disk, it can't shrink one. size takes Proxmox's own
// size syntax: a "+"-prefixed delta (e.g. "+2G") to grow by that amount, or
// a bare absolute size (e.g. "10G") to set the new total size directly.
// Unlike ResizeContainer, Proxmox applies this synchronously and returns no
// task UPID (it just extends the underlying disk image), so this returns
// only an error — same shape as PutVMConfig.
func (c *Client) ResizeVM(ctx context.Context, node string, vmid int, disk, size string) error {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/resize", node, vmid)
	form := url.Values{"disk": {disk}, "size": {size}}
	return c.do(ctx, http.MethodPut, path, strings.NewReader(form.Encode()), nil)
}

func (c *Client) SnapshotVM(ctx context.Context, node string, vmid int, name string) (string, error) {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/snapshot", node, vmid)
	form := url.Values{"snapname": {name}}
	return c.postUPID(ctx, path, strings.NewReader(form.Encode()))
}

// DeleteVM permanently destroys a VM, returning the Proxmox task UPID —
// mirrors DeleteContainer, except there's no force param: Proxmox's own
// DELETE .../qemu/{vmid} has no equivalent to the LXC endpoint's `force`
// (destroy while running); a running VM must be stopped first, same as
// an LXC container without ct delete's --force. purge removes the VMID
// from backup jobs, replication jobs, HA, and ACLs; without it those
// references are left dangling (matching Proxmox's own default).
func (c *Client) DeleteVM(ctx context.Context, node string, vmid int, purge bool) (string, error) {
	path := fmt.Sprintf("/nodes/%s/qemu/%d", node, vmid)
	if purge {
		path += "?" + url.Values{"purge": {"1"}}.Encode()
	}
	var resp struct {
		Data string `json:"data"`
	}
	if err := c.do(ctx, http.MethodDelete, path, nil, &resp); err != nil {
		return "", err
	}
	return resp.Data, nil
}

// VMConfig is a QEMU VM's full configuration, as returned by
// GET /nodes/{node}/qemu/{vmid}/config. Unlike the LXC Config, there is no
// RawLXC-equivalent passthrough block — QEMU config keys are flat, so
// every non-digest key fits in Fields.
type VMConfig struct {
	Digest string
	Fields map[string]string
}

func (c *Client) GetVMConfig(ctx context.Context, node string, vmid int) (VMConfig, error) {
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/nodes/%s/qemu/%d/config", node, vmid), nil, &resp); err != nil {
		return VMConfig{}, err
	}
	digest, fields, _ := configFromData(resp.Data, false)
	return VMConfig{Digest: digest, Fields: fields}, nil
}

func (c *Client) PutVMConfig(ctx context.Context, node string, vmid int, changed map[string]string, digest string) error {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/config", node, vmid)
	form := url.Values{"digest": {digest}}
	for k, v := range changed {
		form.Set(k, v)
	}
	return classifyPutError(c.do(ctx, http.MethodPut, path, strings.NewReader(form.Encode()), nil))
}

// VMStatus is a QEMU VM's live status, as returned by
// GET /nodes/{node}/qemu/{vmid}/status/current. Unlike LXCStatus, there's
// no swap/maxswap pair — QEMU's guest swap isn't visible via cgroups the
// way LXC's is — and disk/maxdisk track configured disk size rather than
// actual filesystem usage, since Proxmox can't see inside a VM's disks
// without the guest agent.
type VMStatus struct {
	Status  string
	CPU     float64
	CPUs    int
	Mem     int64
	MaxMem  int64
	Disk    int64
	MaxDisk int64
	Uptime  int64
}

// QemuStatus fetches vmid's live status on node — QEMU's equivalent of
// LXCStatus. Numeric fields are loose-decoded (looseInt64) for the same
// reason as LXCStatus's.
func (c *Client) QemuStatus(ctx context.Context, node string, vmid int) (VMStatus, error) {
	var resp struct {
		Data struct {
			Status  string     `json:"status"`
			CPU     float64    `json:"cpu"`
			CPUs    int        `json:"cpus"`
			Mem     looseInt64 `json:"mem"`
			MaxMem  looseInt64 `json:"maxmem"`
			Disk    looseInt64 `json:"disk"`
			MaxDisk looseInt64 `json:"maxdisk"`
			Uptime  looseInt64 `json:"uptime"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/nodes/%s/qemu/%d/status/current", node, vmid), nil, &resp); err != nil {
		return VMStatus{}, err
	}
	d := resp.Data
	return VMStatus{
		Status:  d.Status,
		CPU:     d.CPU,
		CPUs:    d.CPUs,
		Mem:     int64(d.Mem),
		MaxMem:  int64(d.MaxMem),
		Disk:    int64(d.Disk),
		MaxDisk: int64(d.MaxDisk),
		Uptime:  int64(d.Uptime),
	}, nil
}

// TemplateVM converts vmid on node into a template, matching
// `qm template <vmid>` — mirrors TemplateContainer exactly (one-way
// conversion, synchronous API call with no task UPID).
func (c *Client) TemplateVM(ctx context.Context, node string, vmid int) error {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/template", node, vmid)
	return c.do(ctx, http.MethodPost, path, nil, nil)
}

// QemuInterface is one network interface reported by the QEMU guest agent
// via GET /nodes/{node}/qemu/{vmid}/agent/network-get-interfaces. Unlike
// LXCInterface's fixed Inet/Inet6 pair, the guest agent reports an
// arbitrary list of addresses per interface (0+ of either family) with no
// CIDR suffix to strip — prefix length comes back as a separate field
// pvectl doesn't currently need.
type QemuInterface struct {
	Name        string
	HWAddr      string
	IPAddresses []string
}

// QemuInterfaces fetches vmid's network interfaces on node via the QEMU
// guest agent, omitting the loopback interface ("lo") — QEMU's equivalent
// of LXCInterfaces. Only works when the guest agent is installed, running,
// and enabled in the VM's config; Proxmox errors otherwise, which callers
// should treat as "IPs unavailable" rather than a hard failure.
func (c *Client) QemuInterfaces(ctx context.Context, node string, vmid int) ([]QemuInterface, error) {
	var resp struct {
		Data struct {
			Result []struct {
				Name        string `json:"name"`
				HWAddr      string `json:"hardware-address"`
				IPAddresses []struct {
					Address string `json:"ip-address"`
				} `json:"ip-addresses"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/nodes/%s/qemu/%d/agent/network-get-interfaces", node, vmid), nil, &resp); err != nil {
		return nil, err
	}
	var out []QemuInterface
	for _, e := range resp.Data.Result {
		if e.Name == "lo" {
			continue
		}
		qi := QemuInterface{Name: e.Name, HWAddr: e.HWAddr}
		for _, ip := range e.IPAddresses {
			qi.IPAddresses = append(qi.IPAddresses, ip.Address)
		}
		out = append(out, qi)
	}
	return out, nil
}
