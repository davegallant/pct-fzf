package cmd

import (
	"strings"
	"testing"

	"github.com/davegallant/pvectl/internal/api"
)

func TestAnyTagged(t *testing.T) {
	tests := []struct {
		name string
		in   []api.Container
		want bool
	}{
		{"empty list", nil, false},
		{"all untagged", []api.Container{{Tags: []string{}}, {Tags: nil}}, false},
		{"one tagged", []api.Container{{Tags: []string{}}, {Tags: []string{"media"}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := anyTagged(tt.in, func(c api.Container) []string { return c.Tags })
			if got != tt.want {
				t.Errorf("anyTagged() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The TAGS column exists only when something in the result set is tagged —
// on a typical cluster most guests are untagged, and an always-on column
// would be near-empty whitespace in everyone's output.
func TestRenderContainerListTagsColumn(t *testing.T) {
	untagged := []api.Container{
		{VMID: 100, Name: "radarr", Node: "pve1", Status: "running", Tags: []string{}},
	}
	if got := renderContainerList(untagged); strings.Contains(got, "TAGS") {
		t.Errorf("renderContainerList() = %q, want no TAGS column when nothing is tagged", got)
	}

	tagged := []api.Container{
		{VMID: 100, Name: "radarr", Node: "pve1", Status: "running", Tags: []string{"arr", "media"}},
		{VMID: 101, Name: "umami", Node: "pve2", Status: "running", Tags: []string{}},
	}
	got := renderContainerList(tagged)
	if !strings.Contains(got, "TAGS") {
		t.Errorf("renderContainerList() = %q, want a TAGS column when a container is tagged", got)
	}
	if !strings.Contains(got, "arr,media") {
		t.Errorf("renderContainerList() = %q, want comma-joined tags", got)
	}
	// The untagged row still needs its cell, or tabwriter misaligns the
	// column for every row after it.
	lines := strings.Split(strings.TrimSpace(got), "\n")
	if len(lines) != 3 {
		t.Fatalf("renderContainerList() produced %d lines, want 3 (header + 2 rows)", len(lines))
	}
	if strings.Contains(lines[2], "arr") {
		t.Errorf("untagged row = %q, want an empty TAGS cell", lines[2])
	}
}

func TestRenderVMListTagsColumn(t *testing.T) {
	untagged := []api.VM{{VMID: 201, Name: "web", Node: "pve1", Status: "running", Tags: []string{}}}
	if got := renderVMList(untagged); strings.Contains(got, "TAGS") {
		t.Errorf("renderVMList() = %q, want no TAGS column when nothing is tagged", got)
	}

	tagged := []api.VM{{VMID: 201, Name: "web", Node: "pve1", Status: "running", Tags: []string{"firewall"}}}
	got := renderVMList(tagged)
	if !strings.Contains(got, "TAGS") || !strings.Contains(got, "firewall") {
		t.Errorf("renderVMList() = %q, want a TAGS column containing firewall", got)
	}
}

func TestJoinTags(t *testing.T) {
	if got, want := joinTags([]string{"arr", "media"}), "arr,media"; got != want {
		t.Errorf("joinTags() = %q, want %q", got, want)
	}
	if got := joinTags([]string{}); got != "" {
		t.Errorf("joinTags([]) = %q, want empty string", got)
	}
}
