package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Appliance is one downloadable LXC appliance template from Proxmox's
// appliance catalog (GET /nodes/{node}/aplinfo) — distinct from Template
// (template.go), which is a template already downloaded onto a storage.
// The catalog is identical on every node, so which node serves it doesn't
// affect the result.
// JSON tags are for `pvectl templates list -o json` — see ISO for the
// key-style rationale.
type Appliance struct {
	// Template is the filename used to request a download, e.g.
	// "debian-12-standard_12.7-1_amd64.tar.zst".
	Template string `json:"template"`
	OS       string `json:"os"`
	Version  string `json:"version"`
	Section  string `json:"section"`
	Headline string `json:"headline"`
	// Type is Proxmox's own classification, "lxc" for every entry the
	// catalog currently serves.
	Type string `json:"type"`
}

type aplinfoEntry struct {
	Template string `json:"template"`
	OS       string `json:"os"`
	Version  string `json:"version"`
	Section  string `json:"section"`
	Headline string `json:"headline"`
	Type     string `json:"type"`
}

type aplinfoResponse struct {
	Data []aplinfoEntry `json:"data"`
}

// ListAppliances fetches the appliance-template catalog from node, sorted
// by template filename for stable output. Unlike ListTemplates, this is a
// single node-scoped call rather than a fan-out across storages: the
// catalog isn't storage-backed, it's Proxmox's index of what *can* be
// downloaded.
func (c *Client) ListAppliances(ctx context.Context, node string) ([]Appliance, error) {
	var resp aplinfoResponse
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/nodes/%s/aplinfo", node), nil, &resp); err != nil {
		return nil, err
	}

	appliances := make([]Appliance, 0, len(resp.Data))
	for _, e := range resp.Data {
		appliances = append(appliances, Appliance{
			Template: e.Template,
			OS:       e.OS,
			Version:  e.Version,
			Section:  e.Section,
			Headline: e.Headline,
			Type:     e.Type,
		})
	}
	sort.Slice(appliances, func(i, j int) bool { return appliances[i].Template < appliances[j].Template })
	return appliances, nil
}

// DownloadAppliance downloads template (a filename from ListAppliances)
// onto storage, returning the Proxmox task UPID — the download runs as a
// background task like create/migrate, so callers poll TaskStatus.
func (c *Client) DownloadAppliance(ctx context.Context, node, storage, template string) (string, error) {
	form := url.Values{
		"storage":  {storage},
		"template": {template},
	}
	return c.postUPID(ctx, fmt.Sprintf("/nodes/%s/aplinfo", node), strings.NewReader(form.Encode()))
}
