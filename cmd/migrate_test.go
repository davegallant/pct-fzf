package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/davegallant/pvectl/internal/api"
)

// TestPromptTargetNodeNoOtherNodes doesn't need to mock stdin: with no
// other cluster nodes, promptTargetNode must error out before it ever
// tries to prompt, matching runDeleteBackup's "reject before reading
// input" precedent for a similarly dead-end case.
func TestPromptTargetNodeNoOtherNodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "node", "node": "pve1", "status": "online"},
			},
		})
	}))
	defer server.Close()

	client := api.NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := promptTargetNode(client, "pve1"); err == nil {
		t.Error("promptTargetNode() error = nil, want an error when there are no other nodes")
	}
}

func TestValidateTargetNodeRejectsUnknownNode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "node", "node": "pve1", "status": "online"},
				{"type": "node", "node": "pve2", "status": "online"},
			},
		})
	}))
	defer server.Close()

	client := api.NewClient(server.URL, "user@pve!test", "secret", true)
	if err := validateTargetNode(client, "pve1", "not-a-real-node"); err == nil {
		t.Error("validateTargetNode() error = nil, want a rejection for an unknown node")
	}
	if err := validateTargetNode(client, "pve1", "pve2"); err != nil {
		t.Errorf("validateTargetNode() error = %v, want nil for a valid target", err)
	}
}

func TestValidateTargetNodeNoOtherNodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "node", "node": "pve1", "status": "online"},
			},
		})
	}))
	defer server.Close()

	client := api.NewClient(server.URL, "user@pve!test", "secret", true)
	if err := validateTargetNode(client, "pve1", "pve2"); err == nil {
		t.Error("validateTargetNode() error = nil, want an error when there are no other nodes")
	}
}

func TestFindContainerByVMID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "lxc", "vmid": 101, "name": "web01", "node": "pve1", "status": "running"},
			},
		})
	}))
	defer server.Close()

	client := api.NewClient(server.URL, "user@pve!test", "secret", true)
	c, err := findContainer(client, "101")
	if err != nil {
		t.Fatalf("findContainer() error = %v", err)
	}
	if c.Name != "web01" {
		t.Errorf("findContainer().Name = %q, want %q", c.Name, "web01")
	}
}

func TestFindContainerByName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "lxc", "vmid": 101, "name": "web01", "node": "pve1", "status": "running"},
			},
		})
	}))
	defer server.Close()

	client := api.NewClient(server.URL, "user@pve!test", "secret", true)
	c, err := findContainer(client, "web01")
	if err != nil {
		t.Fatalf("findContainer() error = %v", err)
	}
	if c.VMID != 101 {
		t.Errorf("findContainer().VMID = %d, want 101", c.VMID)
	}
}

func TestFindContainerNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
	}))
	defer server.Close()

	client := api.NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := findContainer(client, "nonexistent"); err == nil {
		t.Error("findContainer() error = nil, want an error for a name that doesn't exist")
	}
}

func TestFindContainerAmbiguousName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "lxc", "vmid": 101, "name": "web", "node": "pve1", "status": "running"},
				{"type": "lxc", "vmid": 102, "name": "web", "node": "pve2", "status": "stopped"},
			},
		})
	}))
	defer server.Close()

	client := api.NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := findContainer(client, "web"); err == nil {
		t.Error("findContainer() error = nil, want an error for an ambiguous name match")
	}
}

func TestFindVMByVMID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "qemu", "vmid": 201, "name": "opnsense", "node": "pve1", "status": "running"},
			},
		})
	}))
	defer server.Close()

	client := api.NewClient(server.URL, "user@pve!test", "secret", true)
	v, err := findVM(client, "201")
	if err != nil {
		t.Fatalf("findVM() error = %v", err)
	}
	if v.Name != "opnsense" {
		t.Errorf("findVM().Name = %q, want %q", v.Name, "opnsense")
	}
}

// TestRunCtMigratePromptsForTargetWithArg exercises the "named container,
// no --target" path: it must still resolve the container and fall back to
// the interactive target-node prompt (runMigrateWithPrompt) rather than
// erroring outright — reusing TestPromptTargetNodeNoOtherNodes's
// stdin-free "no other cluster nodes" case so the assertion doesn't need
// to mock stdin to prove the prompt path was actually reached.
func TestRunCtMigratePromptsForTargetWithArg(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "node", "node": "pve1", "status": "online"},
				{"type": "lxc", "vmid": 101, "name": "web01", "node": "pve1", "status": "stopped"},
			},
		})
	}))
	defer server.Close()

	client := api.NewClient(server.URL, "user@pve!test", "secret", true)
	err := runCtMigrate(client, []string{"web01"}, "", "")
	if err == nil || !strings.Contains(err.Error(), "no other cluster nodes") {
		t.Errorf("runCtMigrate() error = %v, want the promptTargetNode 'no other nodes' error", err)
	}
}

// TestRunCtMigrateRejectsTargetWithoutArg covers the opposite misuse: a
// bare --target with no name-or-vmid argument is rejected rather than
// silently falling back to the interactive picker.
func TestRunCtMigrateRejectsTargetWithoutArg(t *testing.T) {
	client := api.NewClient("https://unused.invalid:8006", "user@pve!test", "secret", true)
	if err := runCtMigrate(client, nil, "pve2", ""); err == nil {
		t.Error("runCtMigrate() error = nil, want an error when --target is set without an argument")
	}
}

// TestRunCtMigrateDirectPath exercises the full non-interactive path end
// to end: a name-or-vmid argument plus a valid --target looks up the
// container, validates the target, and triggers the migrate API call —
// with stdout redirected so it never has to actually be a terminal.
func TestRunCtMigrateDirectPath(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api2/json/cluster/resources":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"type": "node", "node": "pve1", "status": "online"},
					{"type": "node", "node": "pve2", "status": "online"},
					{"type": "lxc", "vmid": 101, "name": "web01", "node": "pve1", "status": "stopped"},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/migrate"):
			gotPath = r.URL.Path
			_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:pve1:..."})
		case strings.Contains(r.URL.Path, "/tasks/"):
			// runMigrate polls the task to completion (even
			// non-interactively); answer the status poll with a finished task.
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"status": "stopped", "exitstatus": "OK"}})
		default:
			t.Errorf("unexpected request path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := api.NewClient(server.URL, "user@pve!test", "secret", true)
	if err := runCtMigrate(client, []string{"web01"}, "pve2", ""); err != nil {
		t.Fatalf("runCtMigrate() error = %v", err)
	}
	if gotPath != "/api2/json/nodes/pve1/lxc/101/migrate" {
		t.Errorf("migrate request path = %q, want /api2/json/nodes/pve1/lxc/101/migrate", gotPath)
	}
}

// TestRunCtMigrateTargetStorage proves --target-storage survives the
// whole cmd-layer path (flag → runCtMigrate → runMigrate → client) and
// lands in the migrate request body, which is the point of the flag:
// nodes whose storage IDs don't match can't be migrated between without
// it.
func TestRunCtMigrateTargetStorage(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api2/json/cluster/resources":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"type": "node", "node": "pve1", "status": "online"},
					{"type": "node", "node": "pve2", "status": "online"},
					{"type": "lxc", "vmid": 101, "name": "web01", "node": "pve1", "status": "stopped"},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/migrate"):
			body, _ := io.ReadAll(r.Body)
			gotBody = string(body)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:pve1:..."})
		case strings.Contains(r.URL.Path, "/tasks/"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"status": "stopped", "exitstatus": "OK"}})
		default:
			t.Errorf("unexpected request path = %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := api.NewClient(server.URL, "user@pve!test", "secret", true)
	if err := runCtMigrate(client, []string{"web01"}, "pve2", "local-lvm"); err != nil {
		t.Fatalf("runCtMigrate() error = %v", err)
	}
	if !strings.Contains(gotBody, "target-storage=local-lvm") {
		t.Errorf("migrate request body = %q, want it to carry target-storage=local-lvm", gotBody)
	}
}

// TestRunCtMigrateDirectPathRejectsUnknownTarget confirms a typo'd
// --target never reaches the migrate API.
func TestRunCtMigrateDirectPathRejectsUnknownTarget(t *testing.T) {
	migrateCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/migrate") {
			migrateCalled = true
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"type": "node", "node": "pve1", "status": "online"},
				{"type": "lxc", "vmid": 101, "name": "web01", "node": "pve1", "status": "stopped"},
			},
		})
	}))
	defer server.Close()

	client := api.NewClient(server.URL, "user@pve!test", "secret", true)
	if err := runCtMigrate(client, []string{"web01"}, "not-a-real-node", ""); err == nil {
		t.Error("runCtMigrate() error = nil, want an error for an invalid --target")
	}
	if migrateCalled {
		t.Error("runCtMigrate() called the migrate API despite an invalid --target")
	}
}

func TestMigrateCommandsRegistered(t *testing.T) {
	for _, args := range [][]string{{"ct", "migrate"}, {"qm", "migrate"}} {
		found, _, err := rootCmd.Find(args)
		if err != nil {
			t.Errorf("rootCmd.Find(%v) error = %v", args, err)
			continue
		}
		if found.Name() != "migrate" {
			t.Errorf("Find(%v).Name() = %q, want %q", args, found.Name(), "migrate")
		}
	}
}
