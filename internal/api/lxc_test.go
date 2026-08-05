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

func TestClientStart(t *testing.T) {
	var gotPath, gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:pve1:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	upid, err := client.Start(context.Background(), "pve1", 101)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if upid != "UPID:pve1:..." {
		t.Errorf("upid = %q, want %q", upid, "UPID:pve1:...")
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api2/json/nodes/pve1/lxc/101/status/start" {
		t.Errorf("path = %q, want /api2/json/nodes/pve1/lxc/101/status/start", gotPath)
	}
}

func TestClientStop(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.Stop(context.Background(), "pve1", 101); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if gotPath != "/api2/json/nodes/pve1/lxc/101/status/stop" {
		t.Errorf("path = %q, want .../status/stop", gotPath)
	}
}

func TestClientShutdown(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.Shutdown(context.Background(), "pve1", 101); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if gotPath != "/api2/json/nodes/pve1/lxc/101/status/shutdown" {
		t.Errorf("path = %q, want .../status/shutdown", gotPath)
	}
}

func TestClientReboot(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.Reboot(context.Background(), "pve1", 101); err != nil {
		t.Fatalf("Reboot() error = %v", err)
	}
	if gotPath != "/api2/json/nodes/pve1/lxc/101/status/reboot" {
		t.Errorf("path = %q, want .../status/reboot", gotPath)
	}
}

func TestClientSnapshot(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.Snapshot(context.Background(), "pve1", 101, "before-upgrade"); err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if gotBody != "snapname=before-upgrade" {
		t.Errorf("request body = %q, want snapname=before-upgrade", gotBody)
	}
}

func TestClientTemplateContainer(t *testing.T) {
	var gotPath, gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewEncoder(w).Encode(map[string]any{"data": nil})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if err := client.TemplateContainer(context.Background(), "pve1", 101); err != nil {
		t.Fatalf("TemplateContainer() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api2/json/nodes/pve1/lxc/101/template" {
		t.Errorf("path = %q, want .../lxc/101/template", gotPath)
	}
}

func TestClientResizeContainer(t *testing.T) {
	var gotPath, gotMethod, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	upid, err := client.ResizeContainer(context.Background(), "pve1", 101, "rootfs", "+2G")
	if err != nil {
		t.Fatalf("ResizeContainer() error = %v", err)
	}
	if upid != "UPID:..." {
		t.Errorf("upid = %q, want %q", upid, "UPID:...")
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotPath != "/api2/json/nodes/pve1/lxc/101/resize" {
		t.Errorf("path = %q, want .../lxc/101/resize", gotPath)
	}
	form, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", gotBody, err)
	}
	if form.Get("disk") != "rootfs" || form.Get("size") != "+2G" {
		t.Errorf("form = %q, want disk=rootfs&size=%%2B2G", gotBody)
	}
}

func TestCreateContainer(t *testing.T) {
	var gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	upid, err := client.CreateContainer(context.Background(), "pve1", CreateContainerParams{
		VMID:         105,
		OSTemplate:   "local:vztmpl/ubuntu-24.04-standard_24.04-2_amd64.tar.zst",
		Hostname:     "web01",
		Storage:      "local-lvm",
		DiskSizeGB:   8,
		Cores:        1,
		MemoryMB:     512,
		SwapMB:       512,
		Net0:         "name=eth0,bridge=vmbr0,ip=dhcp",
		Unprivileged: true,
		Arch:         "amd64",
	})
	if err != nil {
		t.Fatalf("CreateContainer() error = %v", err)
	}
	if upid != "UPID:..." {
		t.Errorf("CreateContainer() upid = %q", upid)
	}
	if gotPath != "/api2/json/nodes/pve1/lxc" {
		t.Errorf("path = %q, want /api2/json/nodes/pve1/lxc", gotPath)
	}

	values, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", gotBody, err)
	}
	want := map[string]string{
		"vmid":         "105",
		"ostemplate":   "local:vztmpl/ubuntu-24.04-standard_24.04-2_amd64.tar.zst",
		"hostname":     "web01",
		"rootfs":       "local-lvm:8",
		"cores":        "1",
		"memory":       "512",
		"swap":         "512",
		"net0":         "name=eth0,bridge=vmbr0,ip=dhcp",
		"arch":         "amd64",
		"unprivileged": "1",
	}
	for k, v := range want {
		if values.Get(k) != v {
			t.Errorf("body[%q] = %q, want %q", k, values.Get(k), v)
		}
	}
	if values.Has("features") || values.Has("password") || values.Has("ssh-public-keys") {
		t.Errorf("body = %q, want features/password/ssh-public-keys omitted when empty", gotBody)
	}
}

func TestCreateContainerOptionalFields(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.CreateContainer(context.Background(), "pve1", CreateContainerParams{
		VMID:          105,
		Unprivileged:  false,
		Features:      "nesting=1",
		Password:      "hunter2",
		SSHPublicKeys: "ssh-ed25519 AAAA...",
	}); err != nil {
		t.Fatalf("CreateContainer() error = %v", err)
	}

	values, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", gotBody, err)
	}
	if values.Has("unprivileged") {
		t.Errorf("body = %q, want unprivileged omitted when false", gotBody)
	}
	if values.Get("features") != "nesting=1" {
		t.Errorf(`body["features"] = %q, want "nesting=1"`, values.Get("features"))
	}
	if values.Get("password") != "hunter2" {
		t.Errorf(`body["password"] = %q, want "hunter2"`, values.Get("password"))
	}
	if values.Get("ssh-public-keys") != "ssh-ed25519 AAAA..." {
		t.Errorf(`body["ssh-public-keys"] = %q, want the given key`, values.Get("ssh-public-keys"))
	}
}

func TestRestoreContainer(t *testing.T) {
	var gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:pve1:restore"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	upid, err := client.RestoreContainer(context.Background(), "pve1", 101, "local:backup/vzdump-lxc-101-2024_01_01.tar.zst", "local-lvm", false)
	if err != nil {
		t.Fatalf("RestoreContainer() error = %v", err)
	}
	if upid != "UPID:pve1:restore" {
		t.Errorf("RestoreContainer() upid = %q", upid)
	}
	if gotPath != "/api2/json/nodes/pve1/lxc" {
		t.Errorf("path = %q, want /api2/json/nodes/pve1/lxc", gotPath)
	}

	values, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", gotBody, err)
	}
	want := map[string]string{
		"vmid":       "101",
		"ostemplate": "local:backup/vzdump-lxc-101-2024_01_01.tar.zst",
		"restore":    "1",
		"storage":    "local-lvm",
	}
	for k, v := range want {
		if values.Get(k) != v {
			t.Errorf("body[%q] = %q, want %q", k, values.Get(k), v)
		}
	}
	if values.Has("force") {
		t.Errorf("body = %q, want force omitted when false", gotBody)
	}
}

func TestRestoreContainerForce(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.RestoreContainer(context.Background(), "pve1", 101, "local:backup/vzdump-lxc-101.tar.zst", "local-lvm", true); err != nil {
		t.Fatalf("RestoreContainer() error = %v", err)
	}
	values, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", gotBody, err)
	}
	if values.Get("force") != "1" {
		t.Errorf(`body["force"] = %q, want "1"`, values.Get("force"))
	}
}

func TestClientCloneContainer(t *testing.T) {
	var gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	upid, err := client.CloneContainer(context.Background(), "pve1", 101, CloneContainerParams{
		TargetVMID:  105,
		Hostname:    "web01-clone",
		Storage:     "local-lvm",
		Full:        true,
		Target:      "pve2",
		Pool:        "eng",
		Description: "clone for testing",
		SnapName:    "pre-upgrade",
	})
	if err != nil {
		t.Fatalf("CloneContainer() error = %v", err)
	}
	if upid != "UPID:..." {
		t.Errorf("CloneContainer() upid = %q", upid)
	}
	if gotPath != "/api2/json/nodes/pve1/lxc/101/clone" {
		t.Errorf("path = %q, want /api2/json/nodes/pve1/lxc/101/clone", gotPath)
	}

	values, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", gotBody, err)
	}
	want := map[string]string{
		"newid":       "105",
		"hostname":    "web01-clone",
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

func TestClientCloneContainerOmitsOptionalFields(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.CloneContainer(context.Background(), "pve1", 101, CloneContainerParams{TargetVMID: 105}); err != nil {
		t.Fatalf("CloneContainer() error = %v", err)
	}
	if gotBody != "newid=105" {
		t.Errorf("body = %q, want newid=105 with every optional field omitted", gotBody)
	}
}

func TestDeleteContainer(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:pve1:destroy"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	upid, err := client.DeleteContainer(context.Background(), "pve1", 101, false, false)
	if err != nil {
		t.Fatalf("DeleteContainer() error = %v", err)
	}
	if upid != "UPID:pve1:destroy" {
		t.Errorf("DeleteContainer() upid = %q", upid)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
	if gotPath != "/api2/json/nodes/pve1/lxc/101?" {
		t.Errorf("path = %q, want no purge param when purge is false", gotPath)
	}
}

func TestDeleteContainerPurge(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:pve1:destroy"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.DeleteContainer(context.Background(), "pve1", 101, true, false); err != nil {
		t.Fatalf("DeleteContainer() error = %v", err)
	}
	if gotPath != "/api2/json/nodes/pve1/lxc/101?purge=1" {
		t.Errorf("path = %q, want purge=1 query param", gotPath)
	}
}

func TestDeleteContainerForce(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path + "?" + r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:pve1:destroy"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.DeleteContainer(context.Background(), "pve1", 101, false, true); err != nil {
		t.Fatalf("DeleteContainer() error = %v", err)
	}
	if gotPath != "/api2/json/nodes/pve1/lxc/101?force=1" {
		t.Errorf("path = %q, want force=1 query param", gotPath)
	}
}

func TestClientMigrate(t *testing.T) {
	var gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.Migrate(context.Background(), "pve1", 101, "pve2", true, ""); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if gotPath != "/api2/json/nodes/pve1/lxc/101/migrate" {
		t.Errorf("path = %q, want /api2/json/nodes/pve1/lxc/101/migrate", gotPath)
	}
	if gotBody != "restart=1&target=pve2" {
		t.Errorf("request body = %q, want restart=1&target=pve2", gotBody)
	}
}

func TestClientMigrateStoppedOmitsRestart(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.Migrate(context.Background(), "pve1", 101, "pve2", false, ""); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if gotBody != "target=pve2" {
		t.Errorf("request body = %q, want target=pve2 (no restart param)", gotBody)
	}
}

// TestClientMigrateTargetStorage covers migrating between nodes whose
// storage IDs differ (`pct migrate --target-storage`): the storage rides
// along with restart, and a source:target mapping is passed through
// verbatim rather than being parsed or split up on the way out.
func TestClientMigrateTargetStorage(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.Migrate(context.Background(), "pve1", 101, "pve2", true, "local-lvm"); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if gotBody != "restart=1&target=pve2&target-storage=local-lvm" {
		t.Errorf("request body = %q, want restart=1&target=pve2&target-storage=local-lvm", gotBody)
	}

	if _, err := client.Migrate(context.Background(), "pve1", 101, "pve2", false, "local:local-lvm"); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if gotBody != "target=pve2&target-storage=local%3Alocal-lvm" {
		t.Errorf("request body = %q, want the source:target mapping passed through", gotBody)
	}
}

func TestClientGetConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"hostname": "web01",
				"cores":    2,
				"digest":   "abc123",
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	cfg, err := client.GetConfig(context.Background(), "pve1", 101)
	if err != nil {
		t.Fatalf("GetConfig() error = %v", err)
	}
	if cfg.Digest != "abc123" {
		t.Errorf("Digest = %q, want abc123", cfg.Digest)
	}
	if cfg.Fields["hostname"] != "web01" {
		t.Errorf(`Fields["hostname"] = %q, want "web01"`, cfg.Fields["hostname"])
	}
	if cfg.Fields["cores"] != "2" {
		t.Errorf(`Fields["cores"] = %q, want "2"`, cfg.Fields["cores"])
	}
	if _, ok := cfg.Fields["digest"]; ok {
		t.Error("Fields should not contain the digest key")
	}
}

func TestClientGetConfigFormatsRawLXCField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"hostname": "forgejo-runner",
				"digest":   "abc123",
				"lxc": [][]string{
					{"lxc.cgroup2.devices.allow", "c 10:200 rwm"},
					{"lxc.mount.entry", "/dev/net dev/net none bind,create=dir"},
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	cfg, err := client.GetConfig(context.Background(), "pve1", 101)
	if err != nil {
		t.Fatalf("GetConfig() error = %v", err)
	}

	if _, ok := cfg.Fields["lxc"]; ok {
		t.Error(`Fields should not contain a "lxc" key — it must not be diffable/PUT-able`)
	}

	want := "lxc.cgroup2.devices.allow: c 10:200 rwm\nlxc.mount.entry: /dev/net dev/net none bind,create=dir\n"
	if cfg.RawLXC != want {
		t.Errorf("RawLXC = %q, want %q", cfg.RawLXC, want)
	}
}

func TestClientGetConfigLargeNumericValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Write the raw JSON directly so the numeric literal is exactly
		// 4194304 (>= 1,000,000), the case that triggers Go's default
		// float64 scientific-notation formatting via fmt.Sprintf("%v").
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"hostname":"web01","memory":4194304,"digest":"abc123"}}`)
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	cfg, err := client.GetConfig(context.Background(), "pve1", 101)
	if err != nil {
		t.Fatalf("GetConfig() error = %v", err)
	}
	if strings.Contains(cfg.Fields["memory"], "e+") {
		t.Errorf(`Fields["memory"] = %q, contains scientific notation`, cfg.Fields["memory"])
	}
	if cfg.Fields["memory"] != "4194304" {
		t.Errorf(`Fields["memory"] = %q, want "4194304"`, cfg.Fields["memory"])
	}
}

func TestClientPutConfigDigestMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "digest mismatch"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	err := client.PutConfig(context.Background(), "pve1", 101, map[string]string{"hostname": "web02"}, "stale-digest")
	if !errors.Is(err, ErrDigestMismatch) {
		t.Errorf("PutConfig() error = %v, want ErrDigestMismatch", err)
	}
}

func TestClientLXCStatus(t *testing.T) {
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
				"swap":    0,
				"maxswap": 0,
				"disk":    13980000000,
				"maxdisk": 20960000000,
				"uptime":  2069716,
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	status, err := client.LXCStatus(context.Background(), "pve-g3-1", 104)
	if err != nil {
		t.Fatalf("LXCStatus() error = %v", err)
	}
	if gotPath != "/api2/json/nodes/pve-g3-1/lxc/104/status/current" {
		t.Errorf("path = %q, want .../lxc/104/status/current", gotPath)
	}
	if status.Status != "running" || status.CPUs != 4 || status.MaxMem != 6500000000 || status.MaxDisk != 20960000000 {
		t.Errorf("LXCStatus() = %+v, unexpected values", status)
	}
}

func TestClientLXCStatusLooseNumericStrings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"status":  "stopped",
				"mem":     "0",
				"maxmem":  "6500000000",
				"swap":    "0",
				"maxswap": "0",
				"disk":    "0",
				"maxdisk": "20960000000",
				"uptime":  "0",
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	status, err := client.LXCStatus(context.Background(), "pve1", 101)
	if err != nil {
		t.Fatalf("LXCStatus() error = %v", err)
	}
	if status.MaxMem != 6500000000 || status.MaxDisk != 20960000000 {
		t.Errorf("LXCStatus() = %+v, want string-encoded numerics parsed", status)
	}
}

func TestClientLXCInterfacesOmitsLoopback(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"name": "lo", "hwaddr": "00:00:00:00:00:00", "inet": "127.0.0.1/8", "inet6": "::1/128"},
				{"name": "eth0", "hwaddr": "bc:24:11:87:d8:27", "inet": "192.168.1.24/24", "inet6": "fe80::be24:11ff:feac:5f59/64"},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	interfaces, err := client.LXCInterfaces(context.Background(), "pve-g3-1", 104)
	if err != nil {
		t.Fatalf("LXCInterfaces() error = %v", err)
	}
	if gotPath != "/api2/json/nodes/pve-g3-1/lxc/104/interfaces" {
		t.Errorf("path = %q, want .../lxc/104/interfaces", gotPath)
	}
	if len(interfaces) != 1 || interfaces[0].Name != "eth0" {
		t.Fatalf("LXCInterfaces() = %+v, want a single eth0 entry (lo filtered out)", interfaces)
	}
	if interfaces[0].Inet != "192.168.1.24/24" || interfaces[0].Inet6 != "fe80::be24:11ff:feac:5f59/64" {
		t.Errorf("LXCInterfaces()[0] = %+v, unexpected inet/inet6", interfaces[0])
	}
}

func TestClientLXCInterfacesError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.LXCInterfaces(context.Background(), "pve1", 101); err == nil {
		t.Error("LXCInterfaces() error = nil, want non-nil for a stopped/unreachable container")
	}
}

func TestClientPutConfigDigestMismatchStructuredError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errors": map[string]string{"digest": "detected modified configuration - file changed by other process"},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	err := client.PutConfig(context.Background(), "pve1", 101, map[string]string{"hostname": "web02"}, "stale-digest")
	if !errors.Is(err, ErrDigestMismatch) {
		t.Errorf("PutConfig() error = %v, want ErrDigestMismatch", err)
	}
}
