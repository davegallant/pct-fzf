package cmd

import (
	"strings"
	"testing"

	"github.com/davegallant/pvectl/internal/api"
)

func TestFilenameFromURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "plain", in: "https://example.com/debian-13.iso", want: "debian-13.iso"},
		{name: "nested path", in: "https://example.com/a/b/c/alpine.iso", want: "alpine.iso"},
		{name: "query string discarded", in: "https://example.com/ubuntu.iso?token=abc", want: "ubuntu.iso"},
		{name: "fragment discarded", in: "https://example.com/ubuntu.iso#sha256", want: "ubuntu.iso"},
		{name: "trailing slash trimmed", in: "https://example.com/dir/file.iso/", want: "file.iso"},
		{name: "no path", in: "https://example.com", wantErr: true},
		{name: "root path only", in: "https://example.com/", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := filenameFromURL(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("filenameFromURL(%q) = %q, want an error directing the user to --filename", tt.in, got)
				}
				if !strings.Contains(err.Error(), "--filename") {
					t.Errorf("filenameFromURL(%q) error = %v, want it to mention --filename", tt.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("filenameFromURL(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("filenameFromURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// Proxmox rejects one half of the checksum pair without the other, so the
// conflict is caught locally by flag name instead of surfacing as an
// opaque API error.
func TestValidateChecksum(t *testing.T) {
	tests := []struct {
		name, checksum, algorithm string
		wantErr                   bool
	}{
		{name: "neither set"},
		{name: "both set", checksum: "abc", algorithm: "sha256"},
		{name: "checksum without algorithm", checksum: "abc", wantErr: true},
		{name: "algorithm without checksum", algorithm: "sha256", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateChecksum(tt.checksum, tt.algorithm)
			if tt.wantErr != (err != nil) {
				t.Errorf("validateChecksum(%q, %q) error = %v, wantErr %v", tt.checksum, tt.algorithm, err, tt.wantErr)
			}
		})
	}
}

// An omitted --node must resolve to an *online* node: picking the
// alphabetically first name regardless of status would break every image
// command whenever that node happened to be down.
func TestOnlineNodeNames(t *testing.T) {
	nodes := []api.NodeResource{
		{Name: "pve-c", Status: "online"},
		{Name: "pve-a", Status: "offline"},
		{Name: "pve-b", Status: "online"},
	}
	got := onlineNodeNames(nodes)
	want := []string{"pve-b", "pve-c"}
	if len(got) != len(want) {
		t.Fatalf("onlineNodeNames() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("onlineNodeNames() = %#v, want %#v (offline pve-a excluded, rest sorted)", got, want)
		}
	}

	if got := onlineNodeNames([]api.NodeResource{{Name: "pve-a", Status: "offline"}}); len(got) != 0 {
		t.Errorf("onlineNodeNames() = %#v, want empty when every node is offline", got)
	}
}

func TestRenderAppliances(t *testing.T) {
	got := renderAppliances([]api.Appliance{
		{Template: "debian-13-standard_13.0-1_amd64.tar.zst", OS: "debian-13", Version: "13.0-1", Headline: "Debian 13 Trixie"},
	})
	for _, want := range []string{"TEMPLATE", "OS", "VERSION", "DESCRIPTION", "debian-13-standard_13.0-1_amd64.tar.zst", "Debian 13 Trixie"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderAppliances() = %q, want it to contain %q", got, want)
		}
	}
}

func TestRenderDownloadedTemplates(t *testing.T) {
	got := renderDownloadedTemplates([]api.Template{
		{VolID: "local:vztmpl/debian-13.tar.zst", Storage: "local", Size: 141557760},
	})
	for _, want := range []string{"VOLID", "STORAGE", "SIZE", "local:vztmpl/debian-13.tar.zst", "135M"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderDownloadedTemplates() = %q, want it to contain %q", got, want)
		}
	}
}

func TestRenderISOs(t *testing.T) {
	got := renderISOs([]api.ISO{
		{VolID: "proxmox-iso:iso/debian-13.iso", Storage: "proxmox-iso", Size: 1073741824},
	})
	for _, want := range []string{"VOLID", "STORAGE", "SIZE", "proxmox-iso:iso/debian-13.iso", "1G"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderISOs() = %q, want it to contain %q", got, want)
		}
	}
}
