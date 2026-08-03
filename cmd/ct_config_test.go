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

func TestCtConfigViewCommandRegistered(t *testing.T) {
	found, _, err := rootCmd.Find([]string{"ct", "config", "view"})
	if err != nil {
		t.Fatalf(`rootCmd.Find("ct", "config", "view") error = %v`, err)
	}
	if found.Name() != "view" {
		t.Errorf(`Find("ct", "config", "view").Name() = %q, want "view"`, found.Name())
	}
}

func TestRunConfigViewTable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"hostname": "web",
				"digest":   "abc123",
				"lxc":      []any{[]any{"lxc.cgroup2.devices.allow", "c 10:200 rwm"}},
			},
		})
	}))
	defer server.Close()

	jsonOutput = false
	client := api.NewClient(server.URL, "user@pve!test", "secret", true)
	c := api.Container{VMID: 101, Name: "web", Node: "pve1"}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w
	runErr := runConfigView(client, c)
	os.Stdout = origStdout
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if runErr != nil {
		t.Fatalf("runConfigView() error = %v", runErr)
	}
	got := buf.String()
	if !strings.Contains(got, "hostname: web") {
		t.Errorf("runConfigView() output = %q, want it to contain rendered fields", got)
	}
	if !strings.Contains(got, "lxc.cgroup2.devices.allow: c 10:200 rwm") {
		t.Errorf("runConfigView() output = %q, want it to contain the raw lxc.* lines", got)
	}
	if strings.Contains(got, "digest") {
		t.Errorf("runConfigView() output = %q, should not print the digest", got)
	}
}

func TestRunConfigViewJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"hostname": "web",
				"digest":   "abc123",
				"lxc":      []any{[]any{"lxc.cgroup2.devices.allow", "c 10:200 rwm"}},
			},
		})
	}))
	defer server.Close()

	jsonOutput = true
	defer func() { jsonOutput = false }()
	client := api.NewClient(server.URL, "user@pve!test", "secret", true)
	c := api.Container{VMID: 101, Name: "web", Node: "pve1"}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w
	runErr := runConfigView(client, c)
	os.Stdout = origStdout
	_ = w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if runErr != nil {
		t.Fatalf("runConfigView() error = %v", runErr)
	}

	var got ctConfigViewJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal(%q) error = %v", buf.String(), err)
	}
	if got.Fields["hostname"] != "web" {
		t.Errorf("runConfigView() JSON fields = %v, want hostname=web", got.Fields)
	}
	if !strings.Contains(got.RawLXC, "lxc.cgroup2.devices.allow: c 10:200 rwm") {
		t.Errorf("runConfigView() JSON rawLxc = %q, want it to contain the raw lxc.* line", got.RawLXC)
	}
}

func TestConfigAppendCommandRegistered(t *testing.T) {
	if _, _, err := rootCmd.Find([]string{"ct", "config", "append"}); err != nil {
		t.Errorf("rootCmd.Find([ct config append]) error = %v", err)
	}
}

func TestRunAppendConfigNoLines(t *testing.T) {
	ctConfigAppendLines = nil
	c := api.Container{VMID: 101, Name: "web", Node: "pve1"}

	if err := runAppendConfig(nil, c); err == nil {
		t.Fatal("runAppendConfig() error = nil, want error for missing --line")
	}
}

func TestRunAppendConfigInvalidPrefix(t *testing.T) {
	ctConfigAppendLines = []string{"not.a.raw.line: foo"}
	defer func() { ctConfigAppendLines = nil }()
	c := api.Container{VMID: 101, Name: "web", Node: "pve1"}

	if err := runAppendConfig(nil, c); err == nil {
		t.Fatal("runAppendConfig() error = nil, want error for line not starting with \"lxc.\"")
	}
}
