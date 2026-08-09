package cmd

import (
	"context"
	"fmt"
	"net"
	"strings"
	"text/tabwriter"

	"github.com/davegallant/pvectl/internal/api"
)

// runVMSummary is runSummary's mirror for QEMU VMs — see its comment.
// Status/current and config are core (hard error on failure); interfaces
// (via the guest agent) and HA state are best-effort, same rationale as
// the container side.
func runVMSummary(client *api.Client, v api.VM) error {
	ctx := context.Background()

	status, err := client.QemuStatus(ctx, v.Node, v.VMID)
	if err != nil {
		return fmt.Errorf("fetching status for %s (%d): %w", v.Name, v.VMID, err)
	}
	config, err := client.GetVMConfig(ctx, v.Node, v.VMID)
	if err != nil {
		return fmt.Errorf("fetching config for %s (%d): %w", v.Name, v.VMID, err)
	}

	interfaces, _ := client.QemuInterfaces(ctx, v.Node, v.VMID)
	haState, haManaged, _ := client.HAResourceState(ctx, fmt.Sprintf("vm:%d", v.VMID))

	if jsonOutput {
		return printJSON(vmSummaryJSON(v, status, config, interfaces, haState, haManaged))
	}
	fmt.Print(renderVMSummary(v, status, config, interfaces, haState, haManaged))
	return nil
}

// qmSummaryJSON is `qm summary --output json`'s shape — the same fields
// renderVMSummary's table shows, structured instead of formatted.
type qmSummaryJSON struct {
	VMID       int                `json:"vmid"`
	Name       string             `json:"name"`
	Node       string             `json:"node"`
	Status     string             `json:"status"`
	HAState    string             `json:"haState"`
	HAManaged  bool               `json:"haManaged"`
	Tags       []string           `json:"tags"`
	Agent      bool               `json:"agent"`
	CPU        float64            `json:"cpu"` // fraction 0-1
	CPUs       int                `json:"cpus"`
	Mem        int64              `json:"mem"`
	MaxMem     int64              `json:"maxMem"`
	Disk       int64              `json:"disk"`
	MaxDisk    int64              `json:"maxDisk"`
	Interfaces []summaryInterface `json:"interfaces,omitempty"`
}

// vmSummaryJSON builds a qmSummaryJSON from already-fetched data — same
// link-local filtering as renderVMSummary's IPs section, just structured
// instead of formatted into text.
func vmSummaryJSON(v api.VM, status api.VMStatus, config api.VMConfig, interfaces []api.QemuInterface, haState string, haManaged bool) qmSummaryJSON {
	ha := "none"
	if haManaged {
		ha = haState
	}

	var ifaces []summaryInterface
	for _, iface := range interfaces {
		var ips []string
		for _, ip := range iface.IPAddresses {
			if parsed := net.ParseIP(ip); parsed != nil && parsed.IsLinkLocalUnicast() {
				continue
			}
			ips = append(ips, ip)
		}
		if len(ips) > 0 {
			ifaces = append(ifaces, summaryInterface{Name: iface.Name, IPs: ips})
		}
	}

	return qmSummaryJSON{
		VMID:       v.VMID,
		Name:       v.Name,
		Node:       v.Node,
		Status:     status.Status,
		HAState:    ha,
		HAManaged:  haManaged,
		Tags:       v.Tags,
		Agent:      api.AgentEnabled(config.Fields["agent"]),
		CPU:        status.CPU,
		CPUs:       status.CPUs,
		Mem:        status.Mem,
		MaxMem:     status.MaxMem,
		Disk:       status.Disk,
		MaxDisk:    status.MaxDisk,
		Interfaces: ifaces,
	}
}

// renderVMSummary is renderSummary's mirror for QEMU VMs. It swaps the
// container side's Unprivileged line for Guest Agent (whether the config
// enables it) — a VM has no unprivileged concept — and skips a SWAP usage
// line, since QEMU's status/current has no swap/maxswap pair the way
// LXC's does. It performs no I/O, so it's directly unit-testable, same
// pattern as renderSummary.
func renderVMSummary(v api.VM, status api.VMStatus, config api.VMConfig, interfaces []api.QemuInterface, haState string, haManaged bool) string {
	var buf strings.Builder

	fmt.Fprintf(&buf, "VM %d (%s) on node %q\n\n", v.VMID, v.Name, v.Node)

	ha := "none"
	if haManaged {
		ha = haState
	}
	agent := "no"
	if api.AgentEnabled(config.Fields["agent"]) {
		agent = "yes"
	}

	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "Status\t%s\n", status.Status)
	_, _ = fmt.Fprintf(tw, "HA State\t%s\n", ha)
	_, _ = fmt.Fprintf(tw, "Node\t%s\n", v.Node)
	_, _ = fmt.Fprintf(tw, "Guest Agent\t%s\n", agent)
	// Omitted when untagged — see renderSummary.
	if len(v.Tags) > 0 {
		_, _ = fmt.Fprintf(tw, "Tags\t%s\n", joinTags(v.Tags))
	}
	_, _ = fmt.Fprintln(tw)
	_, _ = fmt.Fprintf(tw, "CPU usage\t%s\n", formatCPUUsage(status.CPU, status.CPUs))
	_, _ = fmt.Fprintf(tw, "Memory usage\t%s\n", formatUsage(status.Mem, status.MaxMem))
	_, _ = fmt.Fprintf(tw, "Bootdisk size\t%s\n", formatUsage(status.Disk, status.MaxDisk))
	_ = tw.Flush()

	var ifaceLines []string
	for _, iface := range interfaces {
		var ips []string
		for _, ip := range iface.IPAddresses {
			// Skip link-local addresses (IPv6 fe80::/10, IPv4
			// 169.254.0.0/16): the guest agent reports one per interface,
			// so a guest with many virtual NICs (Docker bridges,
			// per-container veths — common on e.g. Home Assistant OS)
			// buries the routable addresses under a wall of non-routable,
			// per-interface noise.
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

func init() {
	qmCmd.AddCommand(newSimpleVMActionCmd("summary", "Show a VM's status, HA state, resource usage, and IPs", mutationSafe, runVMSummary))
}
