package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestParseTags(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", []string{}},
		{"single", "firewall", []string{"firewall"}},
		{"semicolon separated", "media;arr", []string{"arr", "media"}},
		{"sorted for stable output", "zebra;alpha;mid", []string{"alpha", "mid", "zebra"}},
		{"blank segments dropped", "a;;b;", []string{"a", "b"}},
		{"whitespace trimmed", " a ; b ", []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTags(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseTags(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}

// parseTags must never return nil: `-o json` marshals the result directly,
// and a nil slice would emit "null" instead of "[]", breaking the stable
// schema consumers parse.
func TestParseTagsNeverNil(t *testing.T) {
	if got := parseTags(""); got == nil {
		t.Error("parseTags(\"\") returned nil, want an empty non-nil slice so JSON emits [] not null")
	}
}

func TestNormalizeTags(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"comma separated becomes semicolons", "media,arr", "arr;media"},
		{"semicolons accepted too", "media;arr", "arr;media"},
		{"trailing comma is not a blank tag", "media,", "media"},
		{"whitespace trimmed", " media , arr ", "arr;media"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeTags(tt.in); got != tt.want {
				t.Errorf("NormalizeTags(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Proxmox omits the tags field entirely for an untagged guest and sends it
// semicolon-separated otherwise; both shapes must survive the round trip
// through /cluster/resources into Container.Tags and VM.Tags.
func TestListTagsFromClusterResources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"vmid": 100, "name": "tagged-ct", "node": "pve1", "status": "running", "type": "lxc", "tags": "media;arr"},
				{"vmid": 101, "name": "plain-ct", "node": "pve1", "status": "running", "type": "lxc"},
				{"vmid": 201, "name": "tagged-vm", "node": "pve1", "status": "running", "type": "qemu", "tags": "firewall"},
				{"vmid": 202, "name": "plain-vm", "node": "pve1", "status": "running", "type": "qemu"},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret123", true)

	containers, err := client.ListContainers(context.Background())
	if err != nil {
		t.Fatalf("ListContainers() error = %v", err)
	}
	if want := []string{"arr", "media"}; !reflect.DeepEqual(containers[0].Tags, want) {
		t.Errorf("tagged container Tags = %#v, want %#v", containers[0].Tags, want)
	}
	if len(containers[1].Tags) != 0 || containers[1].Tags == nil {
		t.Errorf("untagged container Tags = %#v, want empty non-nil slice", containers[1].Tags)
	}

	vms, err := client.ListVMs(context.Background())
	if err != nil {
		t.Fatalf("ListVMs() error = %v", err)
	}
	if want := []string{"firewall"}; !reflect.DeepEqual(vms[0].Tags, want) {
		t.Errorf("tagged VM Tags = %#v, want %#v", vms[0].Tags, want)
	}
	if len(vms[1].Tags) != 0 || vms[1].Tags == nil {
		t.Errorf("untagged VM Tags = %#v, want empty non-nil slice", vms[1].Tags)
	}
}
