package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/davegallant/pvectl/internal/api"
)

func TestQmConfigViewCommandRegistered(t *testing.T) {
	found, _, err := rootCmd.Find([]string{"qm", "config", "view"})
	if err != nil {
		t.Fatalf(`rootCmd.Find("qm", "config", "view") error = %v`, err)
	}
	if found.Name() != "view" {
		t.Errorf(`Find("qm", "config", "view").Name() = %q, want "view"`, found.Name())
	}
}

func TestRunVMConfigViewTable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"name": "web01", "cores": "2", "digest": "abc123"},
		})
	}))
	defer server.Close()

	jsonOutput = false
	client := api.NewClient(server.URL, "user@pve!test", "secret", true)
	v := api.VM{VMID: 201, Name: "web01", Node: "pve1"}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w
	runErr := runVMConfigView(client, v)
	os.Stdout = origStdout
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if runErr != nil {
		t.Fatalf("runVMConfigView() error = %v", runErr)
	}
	got := buf.String()
	if !strings.Contains(got, "name: web01") || !strings.Contains(got, "cores: 2") {
		t.Errorf("runVMConfigView() output = %q, want it to contain rendered fields", got)
	}
	if strings.Contains(got, "digest") {
		t.Errorf("runVMConfigView() output = %q, should not print the digest", got)
	}
}

func TestRunVMConfigViewJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"name": "web01", "digest": "abc123"},
		})
	}))
	defer server.Close()

	jsonOutput = true
	defer func() { jsonOutput = false }()
	client := api.NewClient(server.URL, "user@pve!test", "secret", true)
	v := api.VM{VMID: 201, Name: "web01", Node: "pve1"}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w
	runErr := runVMConfigView(client, v)
	os.Stdout = origStdout
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if runErr != nil {
		t.Fatalf("runVMConfigView() error = %v", runErr)
	}

	var got map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", buf.String(), err)
	}
	if got["name"] != "web01" {
		t.Errorf("runVMConfigView() JSON = %v, want name=web01", got)
	}
	if _, ok := got["digest"]; ok {
		t.Errorf("runVMConfigView() JSON = %v, should not include digest", got)
	}
}
