package api

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// DownloadURLParams holds the parameters for fetching a file straight onto
// a Proxmox storage. Content is the storage content type the file will be
// filed under — "iso" for install media, "vztmpl" for an LXC template —
// and must be one the target storage actually accepts.
//
// Checksum/ChecksumAlgorithm are optional but travel together: Proxmox
// rejects one without the other, so callers validating user input should
// check them as a pair.
type DownloadURLParams struct {
	Storage           string
	Content           string
	Filename          string
	URL               string
	Checksum          string
	ChecksumAlgorithm string
}

// DownloadURL asks node to fetch p.URL onto p.Storage, returning the
// Proxmox task UPID. The node does the fetching, not pvectl — so the URL
// must be reachable from the cluster, and a slow download shows up as a
// long-running task rather than a stalled local command.
func (c *Client) DownloadURL(ctx context.Context, node string, p DownloadURLParams) (string, error) {
	form := url.Values{
		"storage":  {p.Storage},
		"content":  {p.Content},
		"filename": {p.Filename},
		"url":      {p.URL},
	}
	if p.Checksum != "" {
		form.Set("checksum", p.Checksum)
		form.Set("checksum-algorithm", p.ChecksumAlgorithm)
	}
	path := fmt.Sprintf("/nodes/%s/storage/%s/download-url", node, p.Storage)
	return c.postUPID(ctx, path, strings.NewReader(form.Encode()))
}
