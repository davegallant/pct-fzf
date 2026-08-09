package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestListAppliances(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		// Returned out of order, to prove ListAppliances sorts rather than
		// trusting the catalog's ordering.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"template": "ubuntu-24.04-standard_24.04-2_amd64.tar.zst", "os": "ubuntu-24.04", "version": "24.04-2", "section": "system", "headline": "Ubuntu 24.04 Noble", "type": "lxc"},
				{"template": "alpine-3.22-default_20250617_amd64.tar.xz", "os": "alpine", "version": "20250617", "section": "system", "headline": "LXC default image for alpine", "type": "lxc"},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	got, err := client.ListAppliances(context.Background(), "pve1")
	if err != nil {
		t.Fatalf("ListAppliances() error = %v", err)
	}
	if gotPath != "/api2/json/nodes/pve1/aplinfo" {
		t.Errorf("path = %q, want /api2/json/nodes/pve1/aplinfo", gotPath)
	}
	if len(got) != 2 {
		t.Fatalf("ListAppliances() returned %d entries, want 2", len(got))
	}
	if got[0].Template != "alpine-3.22-default_20250617_amd64.tar.xz" {
		t.Errorf("ListAppliances()[0].Template = %q, want the alphabetically first template", got[0].Template)
	}
	if got[1].OS != "ubuntu-24.04" || got[1].Headline != "Ubuntu 24.04 Noble" {
		t.Errorf("ListAppliances()[1] = %+v, want os/headline carried through", got[1])
	}
}

// An empty catalog must come back as an empty slice, not nil, so `-o json`
// emits [] rather than null.
func TestListAppliancesEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	got, err := client.ListAppliances(context.Background(), "pve1")
	if err != nil {
		t.Fatalf("ListAppliances() error = %v", err)
	}
	if got == nil {
		t.Error("ListAppliances() = nil, want an empty non-nil slice")
	}
}

func TestDownloadAppliance(t *testing.T) {
	var gotPath, gotMethod, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:pve1:0000:aplinfo"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	upid, err := client.DownloadAppliance(context.Background(), "pve1", "local", "debian-12-standard_12.7-1_amd64.tar.zst")
	if err != nil {
		t.Fatalf("DownloadAppliance() error = %v", err)
	}
	if upid != "UPID:pve1:0000:aplinfo" {
		t.Errorf("DownloadAppliance() upid = %q", upid)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api2/json/nodes/pve1/aplinfo" {
		t.Errorf("path = %q, want /api2/json/nodes/pve1/aplinfo", gotPath)
	}

	values, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", gotBody, err)
	}
	if values.Get("storage") != "local" {
		t.Errorf("body[storage] = %q, want local", values.Get("storage"))
	}
	if values.Get("template") != "debian-12-standard_12.7-1_amd64.tar.zst" {
		t.Errorf("body[template] = %q", values.Get("template"))
	}
}

func TestDownloadURL(t *testing.T) {
	var gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:pve1:0000:download"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.DownloadURL(context.Background(), "pve1", DownloadURLParams{
		Storage:  "proxmox-iso",
		Content:  "iso",
		Filename: "debian-13.iso",
		URL:      "https://example.invalid/debian-13.iso",
	}); err != nil {
		t.Fatalf("DownloadURL() error = %v", err)
	}

	if gotPath != "/api2/json/nodes/pve1/storage/proxmox-iso/download-url" {
		t.Errorf("path = %q", gotPath)
	}
	values, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", gotBody, err)
	}
	want := map[string]string{
		"storage":  "proxmox-iso",
		"content":  "iso",
		"filename": "debian-13.iso",
		"url":      "https://example.invalid/debian-13.iso",
	}
	for k, v := range want {
		if values.Get(k) != v {
			t.Errorf("body[%q] = %q, want %q", k, values.Get(k), v)
		}
	}
	// Proxmox rejects a checksum without its algorithm, so neither may be
	// sent when the caller supplied no checksum at all.
	for _, k := range []string{"checksum", "checksum-algorithm"} {
		if values.Has(k) {
			t.Errorf("body has %q, want it omitted when no checksum was given", k)
		}
	}
}

func TestDownloadURLWithChecksum(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.DownloadURL(context.Background(), "pve1", DownloadURLParams{
		Storage:           "proxmox-iso",
		Content:           "iso",
		Filename:          "debian-13.iso",
		URL:               "https://example.invalid/debian-13.iso",
		Checksum:          "abc123",
		ChecksumAlgorithm: "sha256",
	}); err != nil {
		t.Fatalf("DownloadURL() error = %v", err)
	}

	values, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", gotBody, err)
	}
	if values.Get("checksum") != "abc123" || values.Get("checksum-algorithm") != "sha256" {
		t.Errorf("body = %q, want checksum=abc123 and checksum-algorithm=sha256", gotBody)
	}
}
