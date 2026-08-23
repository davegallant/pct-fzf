package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestClientRebootNode catches either half of the node reboot API contract
// drifting: Proxmox expects the node-scoped status endpoint and the reboot
// action in a form field, rather than a guest-style /status/reboot path.
func TestClientRebootNode(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": nil})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if err := client.RebootNode(context.Background(), "pve1"); err != nil {
		t.Fatalf("RebootNode() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api2/json/nodes/pve1/status" {
		t.Errorf("path = %q, want /api2/json/nodes/pve1/status", gotPath)
	}
	if gotBody != "command=reboot" {
		t.Errorf("body = %q, want command=reboot", gotBody)
	}
}

// TestLooseBoolUnmarshalJSON covers every wire shape Proxmox's "shared"
// flag might plausibly arrive as, since it's unconfirmed against a real
// cluster from this sandbox — a strict int/bool type risks breaking all
// of ClusterResources (not just storage collapsing) if the assumption
// about the wire type turns out wrong, per the looseInt64 precedent in
// backup.go.
func TestLooseBoolUnmarshalJSON(t *testing.T) {
	tests := []struct {
		json string
		want bool
	}{
		{"true", true},
		{"false", false},
		{"1", true},
		{"0", false},
		{`"1"`, true},
		{`"0"`, false},
		{`"true"`, true},
		{`"false"`, false},
		{"null", false},
	}
	for _, tt := range tests {
		t.Run(tt.json, func(t *testing.T) {
			var b looseBool
			if err := json.Unmarshal([]byte(tt.json), &b); err != nil {
				t.Fatalf("Unmarshal(%s) error = %v", tt.json, err)
			}
			if bool(b) != tt.want {
				t.Errorf("Unmarshal(%s) = %v, want %v", tt.json, bool(b), tt.want)
			}
		})
	}
}

func TestClientClusterStatusClustered(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/cluster/status" {
			t.Errorf("request path = %q, want /api2/json/cluster/status", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "cluster", "id": "cluster", "name": "homelab", "nodes": 2, "quorate": 1, "version": 20},
				{"type": "node", "id": "node/pve1", "name": "pve1", "ip": "10.0.0.10", "online": 1, "nodeid": 1, "local": 1},
				{"type": "node", "id": "node/pve2", "name": "pve2", "ip": "10.0.0.11", "online": 1, "nodeid": 2, "local": 0},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret123", true)

	got, err := client.ClusterStatus(context.Background())
	if err != nil {
		t.Fatalf("ClusterStatus() error = %v", err)
	}
	if got.Standalone {
		t.Error("ClusterStatus().Standalone = true, want false (cluster entry present)")
	}
	if got.Name != "homelab" {
		t.Errorf("ClusterStatus().Name = %q, want %q", got.Name, "homelab")
	}
	if !got.Quorate {
		t.Error("ClusterStatus().Quorate = false, want true")
	}
	if len(got.Nodes) != 2 {
		t.Fatalf("ClusterStatus().Nodes has %d entries, want 2", len(got.Nodes))
	}
	if got.Nodes["pve1"] != (NodeStatus{IP: "10.0.0.10", Online: true}) {
		t.Errorf("ClusterStatus().Nodes[%q] = %+v, want {IP: 10.0.0.10, Online: true}", "pve1", got.Nodes["pve1"])
	}
	if got.Nodes["pve2"] != (NodeStatus{IP: "10.0.0.11", Online: true}) {
		t.Errorf("ClusterStatus().Nodes[%q] = %+v, want {IP: 10.0.0.11, Online: true}", "pve2", got.Nodes["pve2"])
	}
}

func TestClientClusterStatusStandalone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No "cluster"-type entry: this node has no cluster configured.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "node", "id": "node/pve1", "name": "pve1", "ip": "10.0.0.10", "online": 1, "nodeid": 1, "local": 1},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret123", true)

	got, err := client.ClusterStatus(context.Background())
	if err != nil {
		t.Fatalf("ClusterStatus() error = %v", err)
	}
	if !got.Standalone {
		t.Error("ClusterStatus().Standalone = false, want true (no cluster entry)")
	}
	if got.Name != "" {
		t.Errorf("ClusterStatus().Name = %q, want empty for standalone", got.Name)
	}
	if got.Quorate {
		t.Error("ClusterStatus().Quorate = true, want false for standalone")
	}
	if len(got.Nodes) != 1 {
		t.Fatalf("ClusterStatus().Nodes has %d entries, want 1", len(got.Nodes))
	}
}

func TestClientClusterStatusOfflineNode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "node", "id": "node/pve1", "name": "pve1", "ip": "10.0.0.10", "online": 0, "nodeid": 1, "local": 1},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret123", true)

	got, err := client.ClusterStatus(context.Background())
	if err != nil {
		t.Fatalf("ClusterStatus() error = %v", err)
	}
	if got.Nodes["pve1"].Online {
		t.Error("ClusterStatus().Nodes[pve1].Online = true, want false")
	}
}

func TestClientClusterResourcesBucketsByType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" {
			t.Errorf("request query = %q, want no query string", r.URL.RawQuery)
		}
		if r.URL.Path != "/api2/json/cluster/resources" {
			t.Errorf("request path = %q, want /api2/json/cluster/resources", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "node", "node": "pve2", "status": "online", "cpu": 0.08, "maxcpu": 4, "mem": 4080218931, "maxmem": 10737418240},
				{"type": "node", "node": "pve1", "status": "online", "cpu": 0.12, "maxcpu": 4, "mem": 4831838208, "maxmem": 10737418240},
				{"type": "lxc", "vmid": 101, "name": "web", "node": "pve1", "status": "running"},
				{"type": "lxc", "vmid": 102, "name": "db", "node": "pve1", "status": "running"},
				{"type": "lxc", "vmid": 103, "name": "cache", "node": "pve2", "status": "stopped"},
				{"type": "qemu", "vmid": 200, "name": "vm1", "node": "pve1", "status": "running"},
				{"type": "qemu", "vmid": 201, "name": "vm2", "node": "pve2", "status": "stopped"},
				{"type": "storage", "storage": "nfs-bulk", "node": "pve1", "plugintype": "nfs", "status": "available", "disk": 2308974647930, "maxdisk": 8796093022208, "shared": 1},
				{"type": "storage", "storage": "local-lvm", "node": "pve1", "plugintype": "lvm", "status": "available", "disk": 128849018880, "maxdisk": 536870912000, "shared": 0},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret123", true)

	got, err := client.ClusterResources(context.Background())
	if err != nil {
		t.Fatalf("ClusterResources() error = %v", err)
	}

	if len(got.Nodes) != 2 {
		t.Fatalf("ClusterResources().Nodes has %d entries, want 2", len(got.Nodes))
	}
	if got.Nodes[0].Name != "pve1" || got.Nodes[1].Name != "pve2" {
		t.Errorf("ClusterResources().Nodes = %+v, want pve1 then pve2 (sorted by name)", got.Nodes)
	}
	if got.Nodes[0].CPU != 0.12 || got.Nodes[0].MaxCPU != 4 || got.Nodes[0].Mem != 4831838208 || got.Nodes[0].MaxMem != 10737418240 {
		t.Errorf("ClusterResources().Nodes[0] = %+v, want pve1's resource fields", got.Nodes[0])
	}

	wantContainers := ResourceCounts{Running: 2, Stopped: 1, Total: 3}
	if got.Containers != wantContainers {
		t.Errorf("ClusterResources().Containers = %+v, want %+v", got.Containers, wantContainers)
	}
	wantVMs := ResourceCounts{Running: 1, Stopped: 1, Total: 2}
	if got.VMs != wantVMs {
		t.Errorf("ClusterResources().VMs = %+v, want %+v", got.VMs, wantVMs)
	}

	if len(got.Storage) != 2 {
		t.Fatalf("ClusterResources().Storage has %d entries, want 2", len(got.Storage))
	}
	if got.Storage[0].Name != "local-lvm" || got.Storage[1].Name != "nfs-bulk" {
		t.Errorf("ClusterResources().Storage = %+v, want local-lvm then nfs-bulk (sorted by name)", got.Storage)
	}
	if got.Storage[0].Type != "lvm" || got.Storage[0].Disk != 128849018880 || got.Storage[0].MaxDisk != 536870912000 || got.Storage[0].Health != "available" {
		t.Errorf("ClusterResources().Storage[0] = %+v, want local-lvm's fields", got.Storage[0])
	}
	if got.Storage[0].Shared {
		t.Errorf("ClusterResources().Storage[0].Shared = true, want false for local-lvm")
	}
	if !got.Storage[1].Shared {
		t.Errorf("ClusterResources().Storage[1].Shared = false, want true for nfs-bulk")
	}
}

func TestClientClusterResourcesCountsOtherStatusTowardTotalOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "lxc", "vmid": 101, "name": "web", "node": "pve1", "status": "running"},
				{"type": "lxc", "vmid": 102, "name": "frozen", "node": "pve1", "status": "paused"},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret123", true)

	got, err := client.ClusterResources(context.Background())
	if err != nil {
		t.Fatalf("ClusterResources() error = %v", err)
	}

	want := ResourceCounts{Running: 1, Stopped: 0, Total: 2}
	if got.Containers != want {
		t.Errorf("ClusterResources().Containers = %+v, want %+v (paused counts toward Total only)", got.Containers, want)
	}
}

func TestClientClusterResourcesStorageHealthPassthrough(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "storage", "storage": "flaky-nfs", "node": "pve1", "plugintype": "nfs", "status": "unavailable", "disk": 0, "maxdisk": 0},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret123", true)

	got, err := client.ClusterResources(context.Background())
	if err != nil {
		t.Fatalf("ClusterResources() error = %v", err)
	}
	if len(got.Storage) != 1 || got.Storage[0].Health != "unavailable" {
		t.Errorf("ClusterResources().Storage = %+v, want a single entry with Health = %q", got.Storage, "unavailable")
	}
}

func TestNextID(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "105"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	id, err := client.NextID(context.Background())
	if err != nil {
		t.Fatalf("NextID() error = %v", err)
	}
	if id != 105 {
		t.Errorf("NextID() = %d, want 105", id)
	}
	if gotPath != "/api2/json/cluster/nextid" {
		t.Errorf("NextID() path = %q", gotPath)
	}
}

func TestNextIDRejectsUnparseableID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "not-a-number"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.NextID(context.Background()); err == nil {
		t.Error("NextID() error = nil, want an error for an unparseable id")
	}
}

func TestHAResourceStateManaged(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"sid": "vm:100", "type": "vm", "state": "started"},
				{"sid": "ct:104", "type": "ct", "state": "started"},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	state, managed, err := client.HAResourceState(context.Background(), "ct:104")
	if err != nil {
		t.Fatalf("HAResourceState() error = %v", err)
	}
	if !managed || state != "started" {
		t.Errorf("HAResourceState() = (%q, %v), want (\"started\", true)", state, managed)
	}
	if gotPath != "/api2/json/cluster/ha/resources" {
		t.Errorf("path = %q, want /api2/json/cluster/ha/resources", gotPath)
	}
}

func TestHAResourceStateNotManaged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	state, managed, err := client.HAResourceState(context.Background(), "ct:104")
	if err != nil {
		t.Fatalf("HAResourceState() error = %v", err)
	}
	if managed || state != "" {
		t.Errorf("HAResourceState() = (%q, %v), want (\"\", false) for an unmanaged guest", state, managed)
	}
}
