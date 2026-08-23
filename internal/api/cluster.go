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

// RebootNode asks Proxmox to reboot node through its status endpoint.
// Unlike guest power actions, this endpoint returns null rather than a task
// UPID: Proxmox schedules the host reboot after replying to the request.
func (c *Client) RebootNode(ctx context.Context, node string) error {
	form := url.Values{"command": {"reboot"}}
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/nodes/%s/status", node), strings.NewReader(form.Encode()), nil)
}

// ClusterStatus is derived from GET /cluster/status: cluster identity and
// quorum (absent for a standalone, non-clustered node) plus each node's
// IP and online/offline state.
type ClusterStatus struct {
	Name       string
	Quorate    bool
	Standalone bool
	Nodes      map[string]NodeStatus
}

// NodeStatus is one node's entry from /cluster/status.
type NodeStatus struct {
	IP     string
	Online bool
}

type clusterStatusEntry struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Quorate int    `json:"quorate"`
	IP      string `json:"ip"`
	Online  int    `json:"online"`
}

type clusterStatusResponse struct {
	Data []clusterStatusEntry `json:"data"`
}

// ClusterStatus fetches cluster identity/quorum and per-node IP/online
// state. A standalone (non-clustered) node has no "cluster"-type entry in
// the response, so ClusterStatus.Standalone is set and Name/Quorate are
// left zero-valued.
func (c *Client) ClusterStatus(ctx context.Context) (ClusterStatus, error) {
	var resp clusterStatusResponse
	if err := c.do(ctx, http.MethodGet, "/cluster/status", nil, &resp); err != nil {
		return ClusterStatus{}, err
	}

	status := ClusterStatus{
		Standalone: true,
		Nodes:      make(map[string]NodeStatus),
	}
	for _, e := range resp.Data {
		switch e.Type {
		case "cluster":
			status.Name = e.Name
			status.Quorate = e.Quorate == 1
			status.Standalone = false
		case "node":
			status.Nodes[e.Name] = NodeStatus{IP: e.IP, Online: e.Online == 1}
		}
	}
	return status, nil
}

// NodeResource is one node's entry from /cluster/resources: current
// status and resource usage.
type NodeResource struct {
	Name   string  `json:"name"`
	Status string  `json:"status"`
	CPU    float64 `json:"cpu"` // fraction 0-1
	MaxCPU int     `json:"maxcpu"`
	Mem    int64   `json:"mem"`
	MaxMem int64   `json:"maxmem"`
}

// ResourceCounts tallies containers or VMs by status. Total counts every
// entry regardless of status, so a status other than "running"/"stopped"
// (e.g. "paused") is never silently dropped from the total.
type ResourceCounts struct {
	Running int
	Stopped int
	Total   int
}

// StorageResource is one storage-per-node entry from /cluster/resources.
// Every storage — shared or not — appears as one entry per node, not
// deduplicated — see AGENTS.md's "History & design rationale". Callers
// that want a collapsed,
// one-row-per-name view (e.g. `pvectl status`) dedupe on their own side,
// using Shared to decide whether collapsing is actually correct: Shared
// storage (e.g. NFS/Ceph mounted identically on every node) is genuinely
// the same pool, so one row represents it accurately; non-shared storage
// (e.g. the default "local"/"local-lvm" that every node has its own,
// unrelated copy of) must not be collapsed, or a node's real capacity
// silently disappears from the report.
type StorageResource struct {
	Name    string `json:"name"`
	Node    string `json:"node"`
	Type    string `json:"type"`
	Disk    int64  `json:"disk"`
	MaxDisk int64  `json:"maxdisk"`
	// Health is Proxmox's raw storage status ("available", etc.).
	Health string `json:"health"`
	// Shared reports whether Proxmox's storage config has this storage
	// marked shared (one logical pool visible on every node it's
	// attached to), as opposed to a per-node-distinct storage that
	// merely happens to share a name (e.g. "local").
	Shared bool `json:"shared"`
}

// ClusterResources is the /cluster/resources data needed by `pvectl
// status`, bucketed by resource type. Nodes and Storage are sorted by
// name for stable output ordering.
type ClusterResources struct {
	Nodes      []NodeResource
	Containers ResourceCounts
	VMs        ResourceCounts
	Storage    []StorageResource
}

// ClusterResources fetches every resource in the cluster and buckets it
// by type into node stats, container/VM status counts, and storage usage.
func (c *Client) ClusterResources(ctx context.Context) (ClusterResources, error) {
	entries, err := c.fetchResources(ctx)
	if err != nil {
		return ClusterResources{}, err
	}

	var resources ClusterResources
	for _, e := range entries {
		switch e.Type {
		case "node":
			resources.Nodes = append(resources.Nodes, NodeResource{
				Name:   e.Node,
				Status: e.Status,
				CPU:    e.CPU,
				MaxCPU: e.MaxCPU,
				Mem:    e.Mem,
				MaxMem: e.MaxMem,
			})
		case "lxc":
			addResourceCount(&resources.Containers, e.Status)
		case "qemu":
			addResourceCount(&resources.VMs, e.Status)
		case "storage":
			resources.Storage = append(resources.Storage, StorageResource{
				Name:    e.Storage,
				Node:    e.Node,
				Type:    e.PluginType,
				Disk:    e.Disk,
				MaxDisk: e.MaxDisk,
				Health:  e.Status,
				Shared:  bool(e.Shared),
			})
		}
	}

	sort.Slice(resources.Nodes, func(i, j int) bool {
		return resources.Nodes[i].Name < resources.Nodes[j].Name
	})
	sort.Slice(resources.Storage, func(i, j int) bool {
		if resources.Storage[i].Name != resources.Storage[j].Name {
			return resources.Storage[i].Name < resources.Storage[j].Name
		}
		return resources.Storage[i].Node < resources.Storage[j].Node
	})

	return resources, nil
}

// NextID fetches the next free VMID from GET /cluster/nextid — the same
// ID Proxmox's own GUI/`pvesh get /cluster/nextid` would hand out, so a
// container created without an explicit --vmid doesn't collide with an
// existing guest.
func (c *Client) NextID(ctx context.Context) (int, error) {
	var resp struct {
		Data string `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/cluster/nextid", nil, &resp); err != nil {
		return 0, err
	}
	id, err := strconv.Atoi(resp.Data)
	if err != nil {
		return 0, fmt.Errorf("parsing next free vmid %q: %w", resp.Data, err)
	}
	return id, nil
}

// HAResourceState fetches sid's requested HA state (e.g. "started",
// "stopped", "disabled") from GET /cluster/ha/resources — sid is
// "ct:<vmid>" for a container. managed reports whether sid appears in the
// list at all: Proxmox omits guests that aren't HA-managed entirely
// rather than listing them with an empty state, so "not found" (managed
// == false) is the normal "not under HA" case — not an error — matching
// the GUI's Summary panel showing "HA State: none". Filtered client-side
// like ListContainers, rather than GET /cluster/ha/resources/{sid}, since
// a single-sid lookup's 404-vs-error semantics aren't confirmed against a
// real cluster from this sandbox.
func (c *Client) HAResourceState(ctx context.Context, sid string) (state string, managed bool, err error) {
	var resp struct {
		Data []struct {
			SID   string `json:"sid"`
			State string `json:"state"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/cluster/ha/resources", nil, &resp); err != nil {
		return "", false, err
	}
	for _, r := range resp.Data {
		if r.SID == sid {
			return r.State, true, nil
		}
	}
	return "", false, nil
}

func addResourceCount(counts *ResourceCounts, status string) {
	counts.Total++
	switch status {
	case "running":
		counts.Running++
	case "stopped":
		counts.Stopped++
	}
}
