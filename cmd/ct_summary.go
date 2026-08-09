package cmd

import (
	"context"
	"fmt"
	"math"
	"net"
	"strings"
	"text/tabwriter"

	"github.com/davegallant/pvectl/internal/api"
)

// runSummary fetches everything `ct summary` shows. Status/current and
// config are core — a container always has them, so a failure there hard-
// errors like every other `ct` command. Interfaces and HA state are
// best-effort instead, deliberately breaking from `status`'s all-or-
// nothing rule (see AGENTS.md): a stopped container can't report live
// interfaces (Proxmox errors), and most homelab clusters have no HA
// manager configured at all — neither should block the rest of the
// summary from showing.
func runSummary(client *api.Client, c api.Container) error {
	ctx := context.Background()

	status, err := client.LXCStatus(ctx, c.Node, c.VMID)
	if err != nil {
		return fmt.Errorf("fetching status for %s (%d): %w", c.Name, c.VMID, err)
	}
	config, err := client.GetConfig(ctx, c.Node, c.VMID)
	if err != nil {
		return fmt.Errorf("fetching config for %s (%d): %w", c.Name, c.VMID, err)
	}

	interfaces, _ := client.LXCInterfaces(ctx, c.Node, c.VMID)
	haState, haManaged, _ := client.HAResourceState(ctx, fmt.Sprintf("ct:%d", c.VMID))

	if jsonOutput {
		return printJSON(summaryJSON(c, status, config, interfaces, haState, haManaged))
	}
	fmt.Print(renderSummary(c, status, config, interfaces, haState, haManaged))
	return nil
}

// ctSummaryJSON is `ct summary --output json`'s shape — the same fields
// renderSummary's table shows, structured instead of formatted.
type ctSummaryJSON struct {
	VMID         int                `json:"vmid"`
	Name         string             `json:"name"`
	Node         string             `json:"node"`
	Status       string             `json:"status"`
	HAState      string             `json:"haState"`
	HAManaged    bool               `json:"haManaged"`
	Tags         []string           `json:"tags"`
	Unprivileged bool               `json:"unprivileged"`
	CPU          float64            `json:"cpu"` // fraction 0-1
	CPUs         int                `json:"cpus"`
	Mem          int64              `json:"mem"`
	MaxMem       int64              `json:"maxMem"`
	Swap         int64              `json:"swap"`
	MaxSwap      int64              `json:"maxSwap"`
	Disk         int64              `json:"disk"`
	MaxDisk      int64              `json:"maxDisk"`
	Interfaces   []summaryInterface `json:"interfaces,omitempty"`
}

// summaryInterface is one network interface's non-link-local IPs, shared
// by ct and qm summary's JSON output.
type summaryInterface struct {
	Name string   `json:"name"`
	IPs  []string `json:"ips"`
}

// summaryJSON builds a ctSummaryJSON from already-fetched data — same
// link-local filtering as renderSummary's IPs section, just structured
// instead of formatted into text.
func summaryJSON(c api.Container, status api.LXCStatus, config api.Config, interfaces []api.LXCInterface, haState string, haManaged bool) ctSummaryJSON {
	ha := "none"
	if haManaged {
		ha = haState
	}

	var ifaces []summaryInterface
	for _, iface := range interfaces {
		var ips []string
		for _, raw := range []string{iface.Inet, iface.Inet6} {
			ip := stripCIDR(raw)
			if ip == "" {
				continue
			}
			if parsed := net.ParseIP(ip); parsed != nil && parsed.IsLinkLocalUnicast() {
				continue
			}
			ips = append(ips, ip)
		}
		if len(ips) > 0 {
			ifaces = append(ifaces, summaryInterface{Name: iface.Name, IPs: ips})
		}
	}

	return ctSummaryJSON{
		VMID:         c.VMID,
		Name:         c.Name,
		Node:         c.Node,
		Status:       status.Status,
		HAState:      ha,
		HAManaged:    haManaged,
		Tags:         c.Tags,
		Unprivileged: config.Fields["unprivileged"] == "1",
		CPU:          status.CPU,
		CPUs:         status.CPUs,
		Mem:          status.Mem,
		MaxMem:       status.MaxMem,
		Swap:         status.Swap,
		MaxSwap:      status.MaxSwap,
		Disk:         status.Disk,
		MaxDisk:      status.MaxDisk,
		Interfaces:   ifaces,
	}
}

// renderSummary formats a container's status, HA state, resource usage,
// and IPs — pvectl's equivalent of Proxmox's own "Summary" tab for a
// container. It performs no I/O, so it's directly unit-testable, same
// pattern as renderStatus/renderContainerList.
func renderSummary(c api.Container, status api.LXCStatus, config api.Config, interfaces []api.LXCInterface, haState string, haManaged bool) string {
	var buf strings.Builder

	fmt.Fprintf(&buf, "Container %d (%s) on node %q\n\n", c.VMID, c.Name, c.Node)

	ha := "none"
	if haManaged {
		ha = haState
	}
	unprivileged := "no"
	if config.Fields["unprivileged"] == "1" {
		unprivileged = "yes"
	}

	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "Status\t%s\n", status.Status)
	_, _ = fmt.Fprintf(tw, "HA State\t%s\n", ha)
	_, _ = fmt.Fprintf(tw, "Node\t%s\n", c.Node)
	_, _ = fmt.Fprintf(tw, "Unprivileged\t%s\n", unprivileged)
	// Omitted entirely when untagged, rather than printed as a blank row —
	// same "don't show an empty field" reasoning as the IPs section below.
	if len(c.Tags) > 0 {
		_, _ = fmt.Fprintf(tw, "Tags\t%s\n", joinTags(c.Tags))
	}
	_, _ = fmt.Fprintln(tw)
	_, _ = fmt.Fprintf(tw, "CPU usage\t%s\n", formatCPUUsage(status.CPU, status.CPUs))
	_, _ = fmt.Fprintf(tw, "Memory usage\t%s\n", formatUsage(status.Mem, status.MaxMem))
	_, _ = fmt.Fprintf(tw, "SWAP usage\t%s\n", formatUsage(status.Swap, status.MaxSwap))
	_, _ = fmt.Fprintf(tw, "Bootdisk size\t%s\n", formatUsage(status.Disk, status.MaxDisk))
	_ = tw.Flush()

	// IPs are grouped by interface and link-local addresses (IPv6
	// fe80::/10, IPv4 169.254.0.0/16) are dropped — mirrors qm summary's
	// renderVMSummary. A container usually has only one real NIC, so this
	// rarely matters here, but it keeps the two commands' output
	// consistent and costs nothing on the common case.
	var ifaceLines []string
	for _, iface := range interfaces {
		var ips []string
		for _, raw := range []string{iface.Inet, iface.Inet6} {
			ip := stripCIDR(raw)
			if ip == "" {
				continue
			}
			if parsed := net.ParseIP(ip); parsed != nil && parsed.IsLinkLocalUnicast() {
				continue
			}
			ips = append(ips, ip)
		}
		if len(ips) > 0 {
			ifaceLines = append(ifaceLines, fmt.Sprintf("  %s: %s", iface.Name, strings.Join(ips, ", ")))
		}
	}
	if len(ifaceLines) > 0 {
		_, _ = fmt.Fprintln(&buf)
		_, _ = fmt.Fprintln(&buf, "IPs:")
		for _, line := range ifaceLines {
			_, _ = fmt.Fprintln(&buf, line)
		}
	}

	return buf.String()
}

// formatCPUUsage renders cpu (a 0-1 fraction, as returned by
// status/current) as a "N.NN% of N CPUs" line — two decimal places since
// idle containers commonly sit under 1% (an integer-percent rounding, as
// used by `pvectl status`'s per-node CPU column, would flatten that to a
// misleading "0%").
func formatCPUUsage(cpu float64, cpus int) string {
	return fmt.Sprintf("%.2f%% of %d CPUs", cpu*100, cpus)
}

// formatUsage renders a used/max byte pair as "N.NN% (used of max)", or
// "N/A" when max is unreported (0 or less) — e.g. SWAP on a container with
// no swap configured, or a Proxmox version that doesn't report bootdisk
// usage in status/current.
func formatUsage(used, max int64) string {
	if max <= 0 {
		return "N/A"
	}
	pct := math.Round(float64(used)/float64(max)*10000) / 100
	return fmt.Sprintf("%.2f%% (%s of %s)", pct, formatBytes(used), formatBytes(max))
}

// stripCIDR trims a Proxmox interfaces entry's CIDR suffix (e.g.
// "192.168.1.24/24" -> "192.168.1.24") — pvectl shows bare IPs, matching
// the Proxmox GUI's Summary panel.
func stripCIDR(s string) string {
	if i := strings.IndexByte(s, '/'); i >= 0 {
		return s[:i]
	}
	return s
}

func init() {
	ctCmd.AddCommand(newSimpleActionCmd("summary", "Show a container's status, HA state, resource usage, and IPs", mutationSafe, runSummary))
}
