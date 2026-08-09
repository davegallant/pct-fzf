package cmd

import (
	"reflect"
	"testing"
)

func TestSetCloudInitFlags(t *testing.T) {
	tests := []struct {
		name                              string
		user, password, sshKeys, ipconfig string
		want                              []string
	}{
		{name: "none set"},
		{name: "ciuser only", user: "debian", want: []string{"--ciuser"}},
		{name: "sshkeys only", sshKeys: "/home/dave/.ssh/id_ed25519.pub", want: []string{"--sshkeys"}},
		{
			name: "several, in declaration order",
			user: "debian", ipconfig: "ip=dhcp",
			want: []string{"--ciuser", "--ipconfig0"},
		},
		{
			name: "all four",
			user: "debian", password: "hunter2", sshKeys: "key.pub", ipconfig: "ip=dhcp",
			want: []string{"--ciuser", "--cipassword", "--sshkeys", "--ipconfig0"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := setCloudInitFlags(tt.user, tt.password, tt.sshKeys, tt.ipconfig)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("setCloudInitFlags() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// A literal --cipassword value is used as-is; only "-" triggers the
// read-from-terminal path, which needs a tty and so isn't exercised here.
func TestResolveCIPasswordLiteral(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"empty stays empty", "", ""},
		{"literal value", "hunter2", "hunter2"},
		{"a dash inside a value is not the sentinel", "a-b", "a-b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveCIPassword(tt.in)
			if err != nil {
				t.Fatalf("resolveCIPassword(%q) error = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("resolveCIPassword(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
