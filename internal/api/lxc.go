package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Start, Stop, Shutdown, Reboot, and Snapshot return the Proxmox task UPID
// for the triggered action so callers can poll TaskStatus for completion,
// instead of only firing the request and returning.

func (c *Client) Start(ctx context.Context, node string, vmid int) (string, error) {
	return c.statusAction(ctx, node, vmid, "start")
}

// Stop maps to Proxmox's own `pct stop` / API "stop" action: an immediate
// hard power-off with no graceful attempt, same as pulling the plug. Use
// Shutdown for a graceful ACPI-style stop that waits on the guest.
func (c *Client) Stop(ctx context.Context, node string, vmid int) (string, error) {
	return c.statusAction(ctx, node, vmid, "stop")
}

// Shutdown maps to Proxmox's own `pct shutdown` / API "shutdown" action: a
// graceful stop that asks the container's init to exit and waits for it,
// timing out if it never does. Use Stop for an immediate hard power-off.
func (c *Client) Shutdown(ctx context.Context, node string, vmid int) (string, error) {
	return c.statusAction(ctx, node, vmid, "shutdown")
}

func (c *Client) Reboot(ctx context.Context, node string, vmid int) (string, error) {
	return c.statusAction(ctx, node, vmid, "reboot")
}

func (c *Client) statusAction(ctx context.Context, node string, vmid int, action string) (string, error) {
	return c.postUPID(ctx, fmt.Sprintf("/nodes/%s/lxc/%d/status/%s", node, vmid, action), nil)
}

// Migrate moves a container from node to target, returning the Proxmox
// task UPID. restart should be true when the container is currently
// running — Proxmox's "restart migration" stops it, moves it, and starts
// it again on target; live migration of a running container isn't
// reliably available, so this is the standard way to move one that's up.
// A stopped container needs neither restart nor any equivalent flag.
//
// targetStorage is `pct migrate --target-storage`: empty means "same
// storage ID on the target" (Proxmox's default, and the only thing that
// works when both nodes share the storage layout), while a non-empty
// value moves the volumes onto different storage there. It's passed
// through verbatim, so both forms Proxmox accepts work — a bare storage
// ID (`local-lvm`, every volume) or a `source:target,...` mapping.
func (c *Client) Migrate(ctx context.Context, node string, vmid int, target string, restart bool, targetStorage string) (string, error) {
	path := fmt.Sprintf("/nodes/%s/lxc/%d/migrate", node, vmid)
	form := url.Values{"target": {target}}
	if restart {
		form.Set("restart", "1")
	}
	if targetStorage != "" {
		form.Set("target-storage", targetStorage)
	}
	return c.postUPID(ctx, path, strings.NewReader(form.Encode()))
}

// CreateContainerParams holds the pct-create parameters `pvectl ct create`
// exposes. Unprivileged is sent as "1" only when true — Proxmox treats
// its absence as privileged, matching pct's own default.
type CreateContainerParams struct {
	VMID          int
	OSTemplate    string
	Hostname      string
	Storage       string
	DiskSizeGB    int
	Cores         int
	MemoryMB      int
	SwapMB        int
	Net0          string
	Unprivileged  bool
	Features      string
	Arch          string
	Password      string
	SSHPublicKeys string
}

// CreateContainer creates a new LXC container on node, returning the
// Proxmox task UPID — container creation (unpacking the template) is a
// background task like start/stop/migrate/snapshot, not an instant call.
func (c *Client) CreateContainer(ctx context.Context, node string, p CreateContainerParams) (string, error) {
	path := fmt.Sprintf("/nodes/%s/lxc", node)
	form := url.Values{
		"vmid":       {strconv.Itoa(p.VMID)},
		"ostemplate": {p.OSTemplate},
		"hostname":   {p.Hostname},
		"rootfs":     {fmt.Sprintf("%s:%d", p.Storage, p.DiskSizeGB)},
		"cores":      {strconv.Itoa(p.Cores)},
		"memory":     {strconv.Itoa(p.MemoryMB)},
		"swap":       {strconv.Itoa(p.SwapMB)},
		"net0":       {p.Net0},
		"arch":       {p.Arch},
	}
	if p.Unprivileged {
		form.Set("unprivileged", "1")
	}
	if p.Features != "" {
		form.Set("features", p.Features)
	}
	if p.Password != "" {
		form.Set("password", p.Password)
	}
	if p.SSHPublicKeys != "" {
		form.Set("ssh-public-keys", p.SSHPublicKeys)
	}
	return c.postUPID(ctx, path, strings.NewReader(form.Encode()))
}

// RestoreContainer restores archive (a backup volid, as returned by
// ListBackups/ListAllBackups) onto vmid on node, returning the Proxmox
// task UPID — restoring (unpacking the archive) is a background task
// like CreateContainer. force must be true to overwrite a vmid that
// already exists; Proxmox rejects the restore otherwise.
func (c *Client) RestoreContainer(ctx context.Context, node string, vmid int, archive, storage string, force bool) (string, error) {
	path := fmt.Sprintf("/nodes/%s/lxc", node)
	form := url.Values{
		"vmid":       {strconv.Itoa(vmid)},
		"ostemplate": {archive},
		"restore":    {"1"},
		"storage":    {storage},
	}
	if force {
		form.Set("force", "1")
	}
	return c.postUPID(ctx, path, strings.NewReader(form.Encode()))
}

// CloneContainerParams holds the pct-clone parameters `pvectl ct clone`
// exposes. Full is sent as "1" only when true — omitted otherwise, so
// Proxmox falls back to its own default (full copy for a normal
// container, linked clone for a template), matching pct's own behavior.
type CloneContainerParams struct {
	TargetVMID  int
	Hostname    string
	Storage     string
	Full        bool
	Target      string
	Pool        string
	Description string
	SnapName    string
}

// CloneContainer clones vmid on node into a new container p.TargetVMID,
// returning the Proxmox task UPID — like CreateContainer, cloning is a
// background task (copying disks), not an instant call.
func (c *Client) CloneContainer(ctx context.Context, node string, vmid int, p CloneContainerParams) (string, error) {
	path := fmt.Sprintf("/nodes/%s/lxc/%d/clone", node, vmid)
	form := url.Values{"newid": {strconv.Itoa(p.TargetVMID)}}
	if p.Hostname != "" {
		form.Set("hostname", p.Hostname)
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

// ResizeContainer grows disk (e.g. "rootfs", "mp0") on vmid, returning the
// Proxmox task UPID — like `pct resize`, this only grows a disk, it can't
// shrink one. size takes Proxmox's own size syntax: a "+"-prefixed delta
// (e.g. "+2G") to grow by that amount, or a bare absolute size (e.g. "10G")
// to set the new total size directly.
func (c *Client) ResizeContainer(ctx context.Context, node string, vmid int, disk, size string) (string, error) {
	path := fmt.Sprintf("/nodes/%s/lxc/%d/resize", node, vmid)
	form := url.Values{"disk": {disk}, "size": {size}}
	return c.putUPID(ctx, path, strings.NewReader(form.Encode()))
}

func (c *Client) Snapshot(ctx context.Context, node string, vmid int, name string) (string, error) {
	path := fmt.Sprintf("/nodes/%s/lxc/%d/snapshot", node, vmid)
	form := url.Values{"snapname": {name}}
	return c.postUPID(ctx, path, strings.NewReader(form.Encode()))
}

// DeleteContainer permanently destroys a container, returning the Proxmox
// task UPID — like Migrate/Snapshot, destroying a container (removing its
// disks, etc.) is a background task, not an instant call. purge also
// removes the container from backup jobs, replication jobs, HA, and
// ACLs; without it, those references are left dangling (matching
// Proxmox's own DELETE .../lxc/{vmid} default). force maps to Proxmox's
// own `force` param, which destroys the container even while running
// instead of rejecting the request — Proxmox handles the force-stop
// itself, so this doesn't need a separate stop-then-delete round trip.
func (c *Client) DeleteContainer(ctx context.Context, node string, vmid int, purge bool, force bool) (string, error) {
	path := fmt.Sprintf("/nodes/%s/lxc/%d", node, vmid)
	params := url.Values{}
	if purge {
		params.Set("purge", "1")
	}
	if force {
		params.Set("force", "1")
	}
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	var resp struct {
		Data string `json:"data"`
	}
	if err := c.do(ctx, http.MethodDelete, path, nil, &resp); err != nil {
		return "", err
	}
	return resp.Data, nil
}

// Config is an LXC container's full configuration, as returned by
// GET /nodes/{node}/lxc/{vmid}/config.
type Config struct {
	Digest string
	Fields map[string]string
	// RawLXC holds "lxc.*" passthrough config lines (raw entries with no
	// dedicated Proxmox API parameter, e.g. cgroup rules, bind mounts),
	// one "lxc.subkey: value" line per entry in Proxmox's original order.
	// These can repeat (e.g. multiple lxc.mount.entry lines), so unlike
	// everything else they can't be represented in Fields (a map with
	// unique keys). Shown for context in the preview and $EDITOR view;
	// not currently editable — edits to these lines are not saved back.
	RawLXC string
}

func (c *Client) GetConfig(ctx context.Context, node string, vmid int) (Config, error) {
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/nodes/%s/lxc/%d/config", node, vmid), nil, &resp); err != nil {
		return Config{}, err
	}
	digest, fields, rawLXC := configFromData(resp.Data, true)
	return Config{Digest: digest, Fields: fields, RawLXC: rawLXC}, nil
}

// renderRawLXC formats the "lxc" config field's array of [key, value]
// pairs as one "key: value" line per entry, matching the shape of the
// real /etc/pve/lxc/<vmid>.conf file (each raw lxc.* line on its own).
func renderRawLXC(v any) string {
	pairs, ok := v.([]any)
	if !ok {
		return fmt.Sprintf("%v\n", v)
	}
	var b strings.Builder
	for _, p := range pairs {
		pair, ok := p.([]any)
		if !ok || len(pair) != 2 {
			fmt.Fprintf(&b, "%v\n", p)
			continue
		}
		fmt.Fprintf(&b, "%v: %v\n", pair[0], pair[1])
	}
	return b.String()
}

func (c *Client) PutConfig(ctx context.Context, node string, vmid int, changed map[string]string, digest string) error {
	path := fmt.Sprintf("/nodes/%s/lxc/%d/config", node, vmid)
	form := url.Values{"digest": {digest}}
	for k, v := range changed {
		form.Set(k, v)
	}
	return classifyPutError(c.do(ctx, http.MethodPut, path, strings.NewReader(form.Encode()), nil))
}

// LXCStatus is a container's live status/resource usage, as returned by
// GET /nodes/{node}/lxc/{vmid}/status/current. Unlike Container (from
// /cluster/resources), this has the byte-level mem/swap/disk usage `ct
// summary` needs.
type LXCStatus struct {
	Status  string
	CPU     float64 // fraction 0-1
	CPUs    int
	Mem     int64
	MaxMem  int64
	Swap    int64
	MaxSwap int64
	Disk    int64
	MaxDisk int64
	Uptime  int64
}

// LXCStatus fetches vmid's live status on node. mem/swap/disk/uptime are
// loose-decoded (looseInt64) — same rationale as backup.go's storage
// content listing: unconfirmed against a real cluster from this sandbox,
// and Proxmox has sent supposedly-numeric fields as JSON strings on other
// endpoints before.
func (c *Client) LXCStatus(ctx context.Context, node string, vmid int) (LXCStatus, error) {
	var resp struct {
		Data struct {
			Status  string     `json:"status"`
			CPU     float64    `json:"cpu"`
			CPUs    int        `json:"cpus"`
			Mem     looseInt64 `json:"mem"`
			MaxMem  looseInt64 `json:"maxmem"`
			Swap    looseInt64 `json:"swap"`
			MaxSwap looseInt64 `json:"maxswap"`
			Disk    looseInt64 `json:"disk"`
			MaxDisk looseInt64 `json:"maxdisk"`
			Uptime  looseInt64 `json:"uptime"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/nodes/%s/lxc/%d/status/current", node, vmid), nil, &resp); err != nil {
		return LXCStatus{}, err
	}
	d := resp.Data
	return LXCStatus{
		Status:  d.Status,
		CPU:     d.CPU,
		CPUs:    d.CPUs,
		Mem:     int64(d.Mem),
		MaxMem:  int64(d.MaxMem),
		Swap:    int64(d.Swap),
		MaxSwap: int64(d.MaxSwap),
		Disk:    int64(d.Disk),
		MaxDisk: int64(d.MaxDisk),
		Uptime:  int64(d.Uptime),
	}, nil
}

// TemplateContainer converts vmid on node into a template, matching `pct
// template <vmid>`. This is a one-way conversion: Proxmox marks the
// container as a template (preventing it from being started again) and
// converts its rootfs into a base image other clones can share, but
// there's no supported API path back to a regular container. Unlike
// start/stop/clone/etc., pve-container's own template endpoint runs
// synchronously (no forked task worker) and returns no data, so this
// returns only an error — same shape as PutConfig.
func (c *Client) TemplateContainer(ctx context.Context, node string, vmid int) error {
	path := fmt.Sprintf("/nodes/%s/lxc/%d/template", node, vmid)
	return c.do(ctx, http.MethodPost, path, nil, nil)
}

// LXCInterface is one network interface reported by
// GET /nodes/{node}/lxc/{vmid}/interfaces. Inet/Inet6 include Proxmox's
// CIDR suffix (e.g. "192.168.1.24/24") as returned by the API.
type LXCInterface struct {
	Name   string
	HWAddr string
	Inet   string
	Inet6  string
}

// LXCInterfaces fetches vmid's network interfaces on node, omitting the
// loopback interface ("lo") — Proxmox's own Summary panel doesn't show it
// either. Only works while the container is running; Proxmox returns an
// error otherwise, which callers should treat as "IPs unavailable" rather
// than a hard failure (there's no config-only way to know a DHCP-assigned
// IP).
func (c *Client) LXCInterfaces(ctx context.Context, node string, vmid int) ([]LXCInterface, error) {
	var resp struct {
		Data []struct {
			Name   string `json:"name"`
			HWAddr string `json:"hwaddr"`
			Inet   string `json:"inet"`
			Inet6  string `json:"inet6"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/nodes/%s/lxc/%d/interfaces", node, vmid), nil, &resp); err != nil {
		return nil, err
	}
	var out []LXCInterface
	for _, e := range resp.Data {
		if e.Name == "lo" {
			continue
		}
		out = append(out, LXCInterface{Name: e.Name, HWAddr: e.HWAddr, Inet: e.Inet, Inet6: e.Inet6})
	}
	return out, nil
}
