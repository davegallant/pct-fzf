package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// ErrDigestMismatch is returned by PutConfig/PutVMConfig when the supplied
// digest no longer matches the guest's current config (it changed
// concurrently). Used by both the LXC and QEMU PUT paths, defined here
// rather than in lxc.go (its original home) since classifyPutError —
// shared by both — wraps it.
var ErrDigestMismatch = errors.New("config digest mismatch")

// postUPID posts body (which may be nil) to path and returns the task
// UPID Proxmox replies with. Shared by every action method that triggers
// an async Proxmox task (start/stop/reboot/migrate/snapshot/backup) —
// they're all the same "POST, read the data string back" shape, differing
// only in path and form body. Kept here rather than repeated per file so a
// fix to the UPID-decode path (e.g. a future "data is empty => clearer
// error") lands once for LXC, QEMU, and vzdump alike — without merging the
// ct/qm command trees, which stay deliberately parallel (see AGENTS.md's
// "History & design rationale").
func (c *Client) postUPID(ctx context.Context, path string, body io.Reader) (string, error) {
	var resp struct {
		Data string `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, path, body, &resp); err != nil {
		return "", err
	}
	if resp.Data == "" {
		// Every endpoint routed through here is meant to return a task
		// UPID. An empty one means the reply wasn't the shape we assumed,
		// and handing "" onward produces a confusing failure inside task
		// polling instead of here, where the path is still known.
		return "", fmt.Errorf("no task id in reply from %s", path)
	}
	return resp.Data, nil
}

// putUPID is postUPID's PUT counterpart — used by the LXC/QEMU resize
// endpoints, which (unlike every other config PUT) return a task UPID
// rather than an empty reply.
func (c *Client) putUPID(ctx context.Context, path string, body io.Reader) (string, error) {
	var resp struct {
		Data string `json:"data"`
	}
	if err := c.do(ctx, http.MethodPut, path, body, &resp); err != nil {
		return "", err
	}
	return resp.Data, nil
}

// classifyPutError inspects a PutConfig/PutVMConfig error and, if it's a
// Proxmox digest mismatch, wraps it in ErrDigestMismatch so callers can
// errors.Is it. A mismatch surfaces two ways in Proxmox's replies: as a
// structured error keyed on "digest", or as free-form message text
// containing "digest". Shared by both PUT methods since their mismatch
// detection is byte-identical — err is the raw return from c.do, which is
// already nil on success, so a nil err passes through unchanged.
func classifyPutError(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		if apiErr.Errors["digest"] != "" {
			return fmt.Errorf("%w: %s", ErrDigestMismatch, err)
		}
		if strings.Contains(strings.ToLower(apiErr.Message), "digest") {
			return fmt.Errorf("%w: %s", ErrDigestMismatch, err)
		}
	}
	return err
}

// parseTags splits /cluster/resources' tags field into a slice. Proxmox
// sends tags semicolon-separated, omits the field entirely for an untagged
// guest, and (unlike most fields) has no canonical ordering guarantee — so
// the result is sorted for stable output. The returned slice is always
// non-nil so JSON consumers see [] rather than null.
func parseTags(s string) []string {
	tags := []string{}
	for _, t := range strings.Split(s, ";") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	sort.Strings(tags)
	return tags
}

// NormalizeTags turns a user-supplied comma- or semicolon-separated tag
// list into the semicolon-separated form Proxmox's create/config APIs
// expect, dropping empties so a trailing comma isn't sent as a blank tag.
// Exported for the create commands, which take --tags as a CLI string.
func NormalizeTags(s string) string {
	return strings.Join(parseTags(strings.ReplaceAll(s, ",", ";")), ";")
}

// configFromData parses the map[string]any from a GET .../config reply
// into the shared digest + Fields representation, returning the digest,
// the editable field map, and (for LXC only) the rendered RawLXC block.
// The "lxc" key is consumed here and never placed in Fields, so raw
// passthrough lines can't be sent back on a PUT even when rawLXC rendering
// isn't requested (GetVMConfig passes includeRawLXC=false; QEMU never has
// an "lxc" key anyway, so this is observably a no-op for VMs). Shared by
// GetConfig and GetVMConfig; the numeric-rendering-via-fmt("%v") and
// scientific-notation-avoidance behavior (useNumber keeps large ints as
// json.Number, so %v renders them literally) are exercised by both sets of
// unit tests via the respective Get* methods.
func configFromData(data map[string]any, includeRawLXC bool) (digest string, fields map[string]string, rawLXC string) {
	fields = make(map[string]string, len(data))
	for k, v := range data {
		if k == "digest" {
			digest = fmt.Sprintf("%v", v)
			continue
		}
		if k == "lxc" {
			if includeRawLXC {
				rawLXC = renderRawLXC(v)
			}
			continue
		}
		fields[k] = fmt.Sprintf("%v", v)
	}
	return digest, fields, rawLXC
}
