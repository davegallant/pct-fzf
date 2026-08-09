package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// AgentExecResult is one poll of GET /agent/exec-status. Proxmox omits
// out-data/err-data entirely when a stream produced nothing, so empty
// strings are the normal "no output" case, not a decode failure.
//
// OutTruncated/ErrTruncated report that the guest agent's buffer
// overflowed and output was silently dropped — worth surfacing, since the
// data the caller prints is then incomplete.
type AgentExecResult struct {
	Exited       bool
	ExitCode     int
	OutData      string
	ErrData      string
	OutTruncated bool
	ErrTruncated bool
}

type agentExecStatusResponse struct {
	Data struct {
		Exited       looseBool `json:"exited"`
		ExitCode     int       `json:"exitcode"`
		OutData      string    `json:"out-data"`
		ErrData      string    `json:"err-data"`
		OutTruncated looseBool `json:"out-truncated"`
		ErrTruncated looseBool `json:"err-truncated"`
	} `json:"data"`
}

// AgentEnabled reports whether a VM config's "agent" field enables the
// guest agent. Proxmox writes this property string three ways — a bare
// "1", an explicit "enabled=1", or either of those followed by extra
// options ("1,fstrim_cloned_disks=1") — and the GUI's own toggle produces
// the "enabled=1" form, so a prefix check on "1" misses the common case.
// Anything unrecognized (including an absent field) means disabled.
func AgentEnabled(field string) bool {
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		key, value, hasValue := strings.Cut(part, "=")
		if !hasValue {
			// A bare leading token is the enabled flag itself.
			return key == "1"
		}
		if key == "enabled" {
			return value == "1"
		}
	}
	return false
}

// AgentExec starts command inside vmid's guest OS via the QEMU guest
// agent, returning the guest-side pid to poll with AgentExecStatus.
//
// command must be sent as repeated "command=" parameters (an array), one
// per argv element — confirmed against PVE 9.2: passing the whole command
// line as a single string is rejected with HTTP 596. This is also why
// there's no shell involved: argv goes to the guest verbatim, so shell
// syntax (pipes, redirection, globs) only works if the caller invokes a
// shell explicitly.
func (c *Client) AgentExec(ctx context.Context, node string, vmid int, command []string) (int, error) {
	form := url.Values{}
	for _, arg := range command {
		form.Add("command", arg)
	}

	var resp struct {
		Data struct {
			PID int `json:"pid"`
		} `json:"data"`
	}
	path := fmt.Sprintf("/nodes/%s/qemu/%d/agent/exec", node, vmid)
	if err := c.do(ctx, http.MethodPost, path, strings.NewReader(form.Encode()), &resp); err != nil {
		return 0, err
	}
	return resp.Data.PID, nil
}

// AgentExecStatus polls a running (or finished) AgentExec by pid. Unlike
// the agent's action endpoints, this one is a GET with the pid in the
// query string.
func (c *Client) AgentExecStatus(ctx context.Context, node string, vmid, pid int) (AgentExecResult, error) {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/agent/exec-status?pid=%s",
		node, vmid, url.QueryEscape(strconv.Itoa(pid)))

	var resp agentExecStatusResponse
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return AgentExecResult{}, err
	}
	return AgentExecResult{
		Exited:       bool(resp.Data.Exited),
		ExitCode:     resp.Data.ExitCode,
		OutData:      resp.Data.OutData,
		ErrData:      resp.Data.ErrData,
		OutTruncated: bool(resp.Data.OutTruncated),
		ErrTruncated: bool(resp.Data.ErrTruncated),
	}, nil
}
