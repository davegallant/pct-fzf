package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
)

// Proxmox writes the agent property string several ways; the "enabled=1"
// form is what the GUI's own toggle produces, so it matters most.
func TestAgentEnabled(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"absent", "", false},
		{"bare one", "1", true},
		{"bare zero", "0", false},
		{"explicit enabled", "enabled=1", true},
		{"explicit disabled", "enabled=0", false},
		{"bare one with options", "1,fstrim_cloned_disks=1", true},
		{"bare zero with options", "0,fstrim_cloned_disks=1", false},
		{"enabled with options", "enabled=1,type=virtio", true},
		{"options before enabled", "fstrim_cloned_disks=1,enabled=1", true},
		{"unrecognized", "type=virtio", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AgentEnabled(tt.in); got != tt.want {
				t.Errorf("AgentEnabled(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// The command must go out as repeated "command=" parameters, one per argv
// element. PVE 9.2 rejects a single joined string with HTTP 596, so this
// is the behavior that keeps qm exec working at all.
func TestAgentExecSendsCommandAsArray(t *testing.T) {
	var gotPath, gotMethod, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"pid": 834015}})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	pid, err := client.AgentExec(context.Background(), "pve1", 140, []string{"sh", "-c", "echo hi"})
	if err != nil {
		t.Fatalf("AgentExec() error = %v", err)
	}
	if pid != 834015 {
		t.Errorf("AgentExec() pid = %d, want 834015", pid)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api2/json/nodes/pve1/qemu/140/agent/exec" {
		t.Errorf("path = %q", gotPath)
	}

	values, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", gotBody, err)
	}
	want := []string{"sh", "-c", "echo hi"}
	if !reflect.DeepEqual(values["command"], want) {
		t.Errorf("body[command] = %#v, want %#v (one repeated param per argv element)", values["command"], want)
	}
}

func TestAgentExecStatus(t *testing.T) {
	var gotPath, gotQuery, gotMethod string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotMethod = r.URL.Path, r.URL.RawQuery, r.Method
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"exited":        1,
				"exitcode":      3,
				"err-data":      "oops\n",
				"err-truncated": 0,
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	got, err := client.AgentExecStatus(context.Background(), "pve1", 140, 834015)
	if err != nil {
		t.Fatalf("AgentExecStatus() error = %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET — exec-status is a GET, unlike exec itself", gotMethod)
	}
	if gotPath != "/api2/json/nodes/pve1/qemu/140/agent/exec-status" {
		t.Errorf("path = %q", gotPath)
	}
	if gotQuery != "pid=834015" {
		t.Errorf("query = %q, want pid=834015", gotQuery)
	}

	want := AgentExecResult{Exited: true, ExitCode: 3, ErrData: "oops\n"}
	if got != want {
		t.Errorf("AgentExecStatus() = %+v, want %+v", got, want)
	}
}

// A still-running command reports exited=0 and carries no output yet —
// the poll loop depends on distinguishing this from a finished one.
func TestAgentExecStatusStillRunning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"exited": 0}})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	got, err := client.AgentExecStatus(context.Background(), "pve1", 140, 1)
	if err != nil {
		t.Fatalf("AgentExecStatus() error = %v", err)
	}
	if got.Exited {
		t.Errorf("AgentExecStatus() Exited = true, want false for a running command")
	}
}

func TestAgentExecStatusTruncation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"exited": 1, "exitcode": 0,
				"out-data": "partial", "out-truncated": 1,
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	got, err := client.AgentExecStatus(context.Background(), "pve1", 140, 1)
	if err != nil {
		t.Fatalf("AgentExecStatus() error = %v", err)
	}
	if !got.OutTruncated {
		t.Error("AgentExecStatus() OutTruncated = false, want true so the caller can warn that output is incomplete")
	}
}
