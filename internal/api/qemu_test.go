package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestClientListVMs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api2/json/cluster/resources" {
			t.Errorf("request path = %q, want /api2/json/cluster/resources", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"vmid": 201, "name": "web", "node": "pve1", "status": "running", "type": "qemu"},
				{"vmid": 202, "name": "db", "node": "pve2", "status": "stopped", "type": "qemu"},
				{"vmid": 101, "name": "ct1", "node": "pve1", "status": "running", "type": "lxc"},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret123", true)

	got, err := client.ListVMs(context.Background())
	if err != nil {
		t.Fatalf("ListVMs() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListVMs() returned %d VMs, want 2 (lxc type filtered out)", len(got))
	}
	if got[0].Name != "web" || got[1].Name != "db" {
		t.Errorf("ListVMs() = %+v, want web then db", got)
	}
}

func TestClientListVMsSortedByVMID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /cluster/resources is not guaranteed to return entries in VMID
		// order, so return them scrambled to prove ListVMs sorts its own
		// output rather than trusting the API's ordering.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"vmid": 203, "name": "vm-03", "node": "pve2", "status": "running", "type": "qemu"},
				{"vmid": 201, "name": "vm-01", "node": "pve1", "status": "running", "type": "qemu"},
				{"vmid": 202, "name": "vm-02", "node": "pve1", "status": "stopped", "type": "qemu"},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret123", true)

	got, err := client.ListVMs(context.Background())
	if err != nil {
		t.Fatalf("ListVMs() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ListVMs() returned %d VMs, want 3", len(got))
	}
	wantOrder := []int{201, 202, 203}
	for i, vmid := range wantOrder {
		if got[i].VMID != vmid {
			t.Errorf("ListVMs()[%d].VMID = %d, want %d (sorted ascending)", i, got[i].VMID, vmid)
		}
	}
}

func TestClientStartVM(t *testing.T) {
	var gotPath, gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:pve1:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	upid, err := client.StartVM(context.Background(), "pve1", 201)
	if err != nil {
		t.Fatalf("StartVM() error = %v", err)
	}
	if upid != "UPID:pve1:..." {
		t.Errorf("upid = %q, want %q", upid, "UPID:pve1:...")
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api2/json/nodes/pve1/qemu/201/status/start" {
		t.Errorf("path = %q, want /api2/json/nodes/pve1/qemu/201/status/start", gotPath)
	}
}

func TestClientStopVM(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.StopVM(context.Background(), "pve1", 201); err != nil {
		t.Fatalf("StopVM() error = %v", err)
	}
	if gotPath != "/api2/json/nodes/pve1/qemu/201/status/stop" {
		t.Errorf("path = %q, want .../status/stop", gotPath)
	}
}

func TestClientShutdownVM(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.ShutdownVM(context.Background(), "pve1", 201); err != nil {
		t.Fatalf("ShutdownVM() error = %v", err)
	}
	if gotPath != "/api2/json/nodes/pve1/qemu/201/status/shutdown" {
		t.Errorf("path = %q, want .../status/shutdown", gotPath)
	}
}

func TestClientRebootVM(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.RebootVM(context.Background(), "pve1", 201); err != nil {
		t.Fatalf("RebootVM() error = %v", err)
	}
	if gotPath != "/api2/json/nodes/pve1/qemu/201/status/reboot" {
		t.Errorf("path = %q, want .../status/reboot", gotPath)
	}
}

func TestClientSnapshotVM(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.SnapshotVM(context.Background(), "pve1", 201, "before-upgrade"); err != nil {
		t.Fatalf("SnapshotVM() error = %v", err)
	}
	if gotBody != "snapname=before-upgrade" {
		t.Errorf("request body = %q, want snapname=before-upgrade", gotBody)
	}
}

func TestClientMigrateVM(t *testing.T) {
	var gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.MigrateVM(context.Background(), "pve1", 201, "pve2", true, ""); err != nil {
		t.Fatalf("MigrateVM() error = %v", err)
	}
	if gotPath != "/api2/json/nodes/pve1/qemu/201/migrate" {
		t.Errorf("path = %q, want /api2/json/nodes/pve1/qemu/201/migrate", gotPath)
	}
	if gotBody != "online=1&target=pve2" {
		t.Errorf("request body = %q, want online=1&target=pve2", gotBody)
	}
}

func TestClientMigrateVMStoppedOmitsOnline(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.MigrateVM(context.Background(), "pve1", 201, "pve2", false, ""); err != nil {
		t.Fatalf("MigrateVM() error = %v", err)
	}
	if gotBody != "target=pve2" {
		t.Errorf("request body = %q, want target=pve2 (no online param)", gotBody)
	}
}

// TestClientMigrateVMTargetStorage pins the two things QEMU does
// differently from LXC when the storage differs: the parameter has no
// dash (`targetstorage`), and a running VM additionally needs
// with-local-disks — which must *not* appear for a stopped one, where
// Proxmox rejects it.
func TestClientMigrateVMTargetStorage(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.MigrateVM(context.Background(), "pve1", 201, "pve2", true, "local-lvm"); err != nil {
		t.Fatalf("MigrateVM() error = %v", err)
	}
	if gotBody != "online=1&target=pve2&targetstorage=local-lvm&with-local-disks=1" {
		t.Errorf("request body = %q, want online=1&target=pve2&targetstorage=local-lvm&with-local-disks=1", gotBody)
	}

	if _, err := client.MigrateVM(context.Background(), "pve1", 201, "pve2", false, "local-lvm"); err != nil {
		t.Fatalf("MigrateVM() error = %v", err)
	}
	if gotBody != "target=pve2&targetstorage=local-lvm" {
		t.Errorf("request body = %q, want target=pve2&targetstorage=local-lvm (no with-local-disks)", gotBody)
	}
}

func TestClientTemplateVM(t *testing.T) {
	var gotPath, gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewEncoder(w).Encode(map[string]any{"data": nil})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if err := client.TemplateVM(context.Background(), "pve1", 201); err != nil {
		t.Fatalf("TemplateVM() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api2/json/nodes/pve1/qemu/201/template" {
		t.Errorf("path = %q, want .../qemu/201/template", gotPath)
	}
}

func TestClientResizeVM(t *testing.T) {
	var gotPath, gotMethod, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": nil})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if err := client.ResizeVM(context.Background(), "pve1", 201, "scsi0", "+2G"); err != nil {
		t.Fatalf("ResizeVM() error = %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotPath != "/api2/json/nodes/pve1/qemu/201/resize" {
		t.Errorf("path = %q, want .../qemu/201/resize", gotPath)
	}
	form, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", gotBody, err)
	}
	if form.Get("disk") != "scsi0" || form.Get("size") != "+2G" {
		t.Errorf("form = %q, want disk=scsi0&size=%%2B2G", gotBody)
	}
}

func TestCreateVM(t *testing.T) {
	var gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	upid, err := client.CreateVM(context.Background(), "pve1", CreateVMParams{
		VMID:       201,
		Name:       "web01",
		Cores:      2,
		MemoryMB:   2048,
		Storage:    "local-lvm",
		DiskSizeGB: 32,
		Net0:       "virtio,bridge=vmbr0",
		SCSIHW:     "virtio-scsi-pci",
		OSType:     "l26",
	})
	if err != nil {
		t.Fatalf("CreateVM() error = %v", err)
	}
	if upid != "UPID:..." {
		t.Errorf("CreateVM() upid = %q", upid)
	}
	if gotPath != "/api2/json/nodes/pve1/qemu" {
		t.Errorf("path = %q, want /api2/json/nodes/pve1/qemu", gotPath)
	}

	values, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", gotBody, err)
	}
	want := map[string]string{
		"vmid":   "201",
		"name":   "web01",
		"cores":  "2",
		"memory": "2048",
		"scsi0":  "local-lvm:32",
		"net0":   "virtio,bridge=vmbr0",
		"scsihw": "virtio-scsi-pci",
		"ostype": "l26",
		"boot":   "order=scsi0",
	}
	for k, v := range want {
		if values.Get(k) != v {
			t.Errorf("body[%q] = %q, want %q", k, values.Get(k), v)
		}
	}
	if values.Has("ide2") {
		t.Errorf("body = %q, want ide2 omitted when no ISO given", gotBody)
	}
}

func TestCreateVMWithISO(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.CreateVM(context.Background(), "pve1", CreateVMParams{
		VMID:       201,
		Name:       "web01",
		Storage:    "local-lvm",
		DiskSizeGB: 32,
		ISO:        "local:iso/ubuntu-24.04.iso",
	}); err != nil {
		t.Fatalf("CreateVM() error = %v", err)
	}

	values, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", gotBody, err)
	}
	if values.Get("ide2") != "local:iso/ubuntu-24.04.iso,media=cdrom" {
		t.Errorf("body[ide2] = %q, want local:iso/ubuntu-24.04.iso,media=cdrom", values.Get("ide2"))
	}
	if values.Get("boot") != "order=ide2;scsi0" {
		t.Errorf("body[boot] = %q, want order=ide2;scsi0 when ISO is set", values.Get("boot"))
	}
}

func TestRestoreVM(t *testing.T) {
	var gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:pve1:restore"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	upid, err := client.RestoreVM(context.Background(), "pve1", 201, "local:backup/vzdump-qemu-201-2024_01_01.vma.zst", "local-lvm", false)
	if err != nil {
		t.Fatalf("RestoreVM() error = %v", err)
	}
	if upid != "UPID:pve1:restore" {
		t.Errorf("RestoreVM() upid = %q", upid)
	}
	if gotPath != "/api2/json/nodes/pve1/qemu" {
		t.Errorf("path = %q, want /api2/json/nodes/pve1/qemu", gotPath)
	}

	values, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", gotBody, err)
	}
	want := map[string]string{
		"vmid":    "201",
		"archive": "local:backup/vzdump-qemu-201-2024_01_01.vma.zst",
		"storage": "local-lvm",
	}
	for k, v := range want {
		if values.Get(k) != v {
			t.Errorf("body[%q] = %q, want %q", k, values.Get(k), v)
		}
	}
	if values.Has("force") {
		t.Errorf("body = %q, want force omitted when false", gotBody)
	}
	if values.Has("restore") || values.Has("ostemplate") {
		t.Errorf("body = %q, want no restore/ostemplate params (LXC-only)", gotBody)
	}
}

func TestRestoreVMForce(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.RestoreVM(context.Background(), "pve1", 201, "local:backup/vzdump-qemu-201.vma.zst", "local-lvm", true); err != nil {
		t.Fatalf("RestoreVM() error = %v", err)
	}
	values, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", gotBody, err)
	}
	if values.Get("force") != "1" {
		t.Errorf(`body["force"] = %q, want "1"`, values.Get("force"))
	}
}

func TestClientCloneVM(t *testing.T) {
	var gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	upid, err := client.CloneVM(context.Background(), "pve1", 201, CloneVMParams{
		TargetVMID:  205,
		Name:        "web01-clone",
		Storage:     "local-lvm",
		Full:        true,
		Target:      "pve2",
		Pool:        "eng",
		Description: "clone for testing",
		SnapName:    "pre-upgrade",
	})
	if err != nil {
		t.Fatalf("CloneVM() error = %v", err)
	}
	if upid != "UPID:..." {
		t.Errorf("CloneVM() upid = %q", upid)
	}
	if gotPath != "/api2/json/nodes/pve1/qemu/201/clone" {
		t.Errorf("path = %q, want /api2/json/nodes/pve1/qemu/201/clone", gotPath)
	}

	values, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", gotBody, err)
	}
	want := map[string]string{
		"newid":       "205",
		"name":        "web01-clone",
		"storage":     "local-lvm",
		"full":        "1",
		"target":      "pve2",
		"pool":        "eng",
		"description": "clone for testing",
		"snapname":    "pre-upgrade",
	}
	for k, v := range want {
		if values.Get(k) != v {
			t.Errorf("body[%q] = %q, want %q", k, values.Get(k), v)
		}
	}
}

func TestClientCloneVMOmitsOptionalFields(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.CloneVM(context.Background(), "pve1", 201, CloneVMParams{TargetVMID: 205}); err != nil {
		t.Fatalf("CloneVM() error = %v", err)
	}
	if gotBody != "newid=205" {
		t.Errorf("body = %q, want newid=205 with every optional field omitted", gotBody)
	}
}

func TestClientGetVMConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"name":   "web01",
				"cores":  2,
				"digest": "abc123",
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	cfg, err := client.GetVMConfig(context.Background(), "pve1", 201)
	if err != nil {
		t.Fatalf("GetVMConfig() error = %v", err)
	}
	if cfg.Digest != "abc123" {
		t.Errorf("Digest = %q, want abc123", cfg.Digest)
	}
	if cfg.Fields["name"] != "web01" {
		t.Errorf(`Fields["name"] = %q, want "web01"`, cfg.Fields["name"])
	}
	if cfg.Fields["cores"] != "2" {
		t.Errorf(`Fields["cores"] = %q, want "2"`, cfg.Fields["cores"])
	}
	if _, ok := cfg.Fields["digest"]; ok {
		t.Error("Fields should not contain the digest key")
	}
}

func TestClientGetVMConfigLargeNumericValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Write the raw JSON directly so the numeric literal is exactly
		// 4194304 (>= 1,000,000), the case that triggers Go's default
		// float64 scientific-notation formatting via fmt.Sprintf("%v").
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"name":"web01","memory":4194304,"digest":"abc123"}}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	cfg, err := client.GetVMConfig(context.Background(), "pve1", 201)
	if err != nil {
		t.Fatalf("GetVMConfig() error = %v", err)
	}
	if strings.Contains(cfg.Fields["memory"], "e+") {
		t.Errorf(`Fields["memory"] = %q, contains scientific notation`, cfg.Fields["memory"])
	}
	if cfg.Fields["memory"] != "4194304" {
		t.Errorf(`Fields["memory"] = %q, want "4194304"`, cfg.Fields["memory"])
	}
}

func TestClientPutVMConfigDigestMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "digest mismatch"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	err := client.PutVMConfig(context.Background(), "pve1", 201, map[string]string{"name": "web02"}, "stale-digest")
	if !errors.Is(err, ErrDigestMismatch) {
		t.Errorf("PutVMConfig() error = %v, want ErrDigestMismatch", err)
	}
}

func TestClientPutVMConfigDigestMismatchStructuredError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errors": map[string]string{"digest": "detected modified configuration - file changed by other process"},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	err := client.PutVMConfig(context.Background(), "pve1", 201, map[string]string{"name": "web02"}, "stale-digest")
	if !errors.Is(err, ErrDigestMismatch) {
		t.Errorf("PutVMConfig() error = %v, want ErrDigestMismatch", err)
	}
}

func TestDeleteVM(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:pve1:destroy"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	upid, err := client.DeleteVM(context.Background(), "pve1", 201, false)
	if err != nil {
		t.Fatalf("DeleteVM() error = %v", err)
	}
	if upid != "UPID:pve1:destroy" {
		t.Errorf("DeleteVM() upid = %q", upid)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/api2/json/nodes/pve1/qemu/201?" {
		t.Errorf("path = %q, want no purge param when purge is false", gotPath)
	}
}

func TestDeleteVMPurge(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:pve1:destroy"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.DeleteVM(context.Background(), "pve1", 201, true); err != nil {
		t.Fatalf("DeleteVM() error = %v", err)
	}
	if gotPath != "/api2/json/nodes/pve1/qemu/201?purge=1" {
		t.Errorf("path = %q, want purge=1 query param", gotPath)
	}
}

func TestClientQemuStatus(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"status":  "running",
				"cpu":     0.0037,
				"cpus":    4,
				"mem":     1470000000,
				"maxmem":  6500000000,
				"disk":    0,
				"maxdisk": 20960000000,
				"uptime":  2069716,
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	status, err := client.QemuStatus(context.Background(), "pve-g3-1", 200)
	if err != nil {
		t.Fatalf("QemuStatus() error = %v", err)
	}
	if gotPath != "/api2/json/nodes/pve-g3-1/qemu/200/status/current" {
		t.Errorf("path = %q, want .../qemu/200/status/current", gotPath)
	}
	if status.Status != "running" || status.CPUs != 4 || status.MaxMem != 6500000000 || status.MaxDisk != 20960000000 {
		t.Errorf("QemuStatus() = %+v, unexpected values", status)
	}
}

func TestClientQemuStatusLooseNumericStrings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"status":  "stopped",
				"mem":     "0",
				"maxmem":  "6500000000",
				"disk":    "0",
				"maxdisk": "20960000000",
				"uptime":  "0",
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	status, err := client.QemuStatus(context.Background(), "pve1", 201)
	if err != nil {
		t.Fatalf("QemuStatus() error = %v", err)
	}
	if status.MaxMem != 6500000000 || status.MaxDisk != 20960000000 {
		t.Errorf("QemuStatus() = %+v, want string-encoded numerics parsed", status)
	}
}

func TestClientQemuInterfacesOmitsLoopback(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"result": []map[string]any{
					{
						"name":             "lo",
						"hardware-address": "00:00:00:00:00:00",
						"ip-addresses": []map[string]any{
							{"ip-address": "127.0.0.1", "ip-address-type": "ipv4", "prefix": 8},
						},
					},
					{
						"name":             "eth0",
						"hardware-address": "52:54:00:12:34:56",
						"ip-addresses": []map[string]any{
							{"ip-address": "192.168.1.50", "ip-address-type": "ipv4", "prefix": 24},
							{"ip-address": "fe80::5054:ff:fe12:3456", "ip-address-type": "ipv6", "prefix": 64},
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	interfaces, err := client.QemuInterfaces(context.Background(), "pve-g3-1", 200)
	if err != nil {
		t.Fatalf("QemuInterfaces() error = %v", err)
	}
	if gotPath != "/api2/json/nodes/pve-g3-1/qemu/200/agent/network-get-interfaces" {
		t.Errorf("path = %q, want .../qemu/200/agent/network-get-interfaces", gotPath)
	}
	if len(interfaces) != 1 || interfaces[0].Name != "eth0" {
		t.Fatalf("QemuInterfaces() = %+v, want a single eth0 entry (lo filtered out)", interfaces)
	}
	want := []string{"192.168.1.50", "fe80::5054:ff:fe12:3456"}
	if len(interfaces[0].IPAddresses) != len(want) || interfaces[0].IPAddresses[0] != want[0] || interfaces[0].IPAddresses[1] != want[1] {
		t.Errorf("QemuInterfaces()[0].IPAddresses = %+v, want %+v", interfaces[0].IPAddresses, want)
	}
}

func TestClientQemuInterfacesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.QemuInterfaces(context.Background(), "pve1", 201); err == nil {
		t.Error("QemuInterfaces() error = nil, want non-nil for a stopped VM / no guest agent")
	}
}
