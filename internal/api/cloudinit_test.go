package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// createVMBody runs CreateVM against a stub server and returns the parsed
// form body, so each cloud-init case below is a single assertion instead of
// repeating the httptest plumbing.
func createVMBody(t *testing.T, p CreateVMParams) url.Values {
	t.Helper()
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.CreateVM(context.Background(), "pve1", p); err != nil {
		t.Fatalf("CreateVM() error = %v", err)
	}
	values, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", gotBody, err)
	}
	return values
}

func TestCreateVMParamsHasCloudInit(t *testing.T) {
	tests := []struct {
		name string
		p    CreateVMParams
		want bool
	}{
		{"none set", CreateVMParams{}, false},
		{"ciuser", CreateVMParams{CIUser: "debian"}, true},
		{"cipassword", CreateVMParams{CIPassword: "hunter2"}, true},
		{"sshkeys", CreateVMParams{SSHKeys: "ssh-ed25519 AAAA"}, true},
		{"ipconfig0", CreateVMParams{IPConfig0: "ip=dhcp"}, true},
		{"tags alone is not cloud-init", CreateVMParams{Tags: "media"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.HasCloudInit(); got != tt.want {
				t.Errorf("HasCloudInit() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Any cloud-init parameter must provision the cloudinit drive on ide2, and
// that drive must stay out of the boot order — it carries config data
// Proxmox regenerates, not boot media.
func TestCreateVMCloudInitProvisionsDrive(t *testing.T) {
	values := createVMBody(t, CreateVMParams{
		VMID:       201,
		Name:       "web01",
		Storage:    "local-lvm",
		DiskSizeGB: 32,
		CIUser:     "debian",
		IPConfig0:  "ip=dhcp",
	})

	if got, want := values.Get("ide2"), "local-lvm:cloudinit"; got != want {
		t.Errorf("body[ide2] = %q, want %q", got, want)
	}
	if got, want := values.Get("boot"), "order=scsi0"; got != want {
		t.Errorf("body[boot] = %q, want %q — the cloudinit drive must not be in the boot order", got, want)
	}
	if got, want := values.Get("ciuser"), "debian"; got != want {
		t.Errorf("body[ciuser] = %q, want %q", got, want)
	}
	if got, want := values.Get("ipconfig0"), "ip=dhcp"; got != want {
		t.Errorf("body[ipconfig0] = %q, want %q", got, want)
	}
}

// Proxmox stores sshkeys URL-encoded and expects it that way on the wire,
// so the value is escaped once by CreateVM before form encoding escapes it
// again. url.ParseQuery undoes the form layer, leaving the deliberate
// inner encoding visible here.
//
// The expected value is spelled out literally rather than re-derived from
// escapeSSHKeys: asserting against the same function under test would pass
// no matter which escaper it used, including the wrong one.
func TestCreateVMSSHKeysDoubleEncoded(t *testing.T) {
	const key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5 dave@laptop"
	values := createVMBody(t, CreateVMParams{
		VMID:    201,
		Storage: "local-lvm",
		SSHKeys: key,
	})

	want := "ssh-ed25519%20AAAAC3NzaC1lZDI1NTE5%20dave%40laptop"
	if got := values.Get("sshkeys"); got != want {
		t.Errorf("body[sshkeys] = %q, want %q", got, want)
	}
}

// Spaces must survive as %20, never as "+": Proxmox decodes this field
// with Perl's uri_unescape, which expands %XX but leaves a literal "+"
// alone — so a "+"-encoded space lands in the guest's authorized_keys as
// a plus sign and breaks every key in the blob.
func TestEscapeSSHKeys(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{
			name: "spaces become %20, not +",
			in:   "ssh-ed25519 AAAA dave@laptop",
			want: "ssh-ed25519%20AAAA%20dave%40laptop",
		},
		{
			name: "base64 plus is encoded as %2B and left alone by the rewrite",
			in:   "ssh-rsa AB+cd/ef==",
			want: "ssh-rsa%20AB%2Bcd%2Fef%3D%3D",
		},
		{
			name: "newline between multiple keys",
			in:   "ssh-rsa AAA k1\nssh-rsa BBB k2",
			want: "ssh-rsa%20AAA%20k1%0Assh-rsa%20BBB%20k2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeSSHKeys(tt.in)
			if got != tt.want {
				t.Errorf("escapeSSHKeys(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if strings.Contains(got, "+") {
				t.Errorf("escapeSSHKeys(%q) = %q, which contains a literal '+' — Proxmox will not decode it back to a space", tt.in, got)
			}
		})
	}
}

func TestCreateVMWithoutCloudInitOmitsFields(t *testing.T) {
	values := createVMBody(t, CreateVMParams{VMID: 201, Storage: "local-lvm", DiskSizeGB: 32})

	for _, key := range []string{"ide2", "ciuser", "cipassword", "sshkeys", "ipconfig0", "tags"} {
		if values.Has(key) {
			t.Errorf("body has %q = %q, want it omitted when unset", key, values.Get(key))
		}
	}
}

func TestCreateVMTags(t *testing.T) {
	values := createVMBody(t, CreateVMParams{VMID: 201, Storage: "local-lvm", Tags: "arr;media"})

	if got, want := values.Get("tags"), "arr;media"; got != want {
		t.Errorf("body[tags] = %q, want %q", got, want)
	}
}

func TestCreateContainerTags(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": "UPID:..."})
	}))
	defer server.Close()

	client := NewClient(server.URL, "user@pve!test", "secret", true)
	if _, err := client.CreateContainer(context.Background(), "pve1", CreateContainerParams{
		VMID:       101,
		OSTemplate: "local:vztmpl/debian-12.tar.zst",
		Hostname:   "ct1",
		Storage:    "local-lvm",
		Tags:       "arr;media",
	}); err != nil {
		t.Fatalf("CreateContainer() error = %v", err)
	}

	values, err := url.ParseQuery(gotBody)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", gotBody, err)
	}
	if got, want := values.Get("tags"), "arr;media"; got != want {
		t.Errorf("body[tags] = %q, want %q", got, want)
	}
}
