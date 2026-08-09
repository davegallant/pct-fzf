package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
)

// Template is one LXC OS template (vztmpl content) found on a storage's
// content listing.
// JSON tags are for `pvectl templates list --downloaded -o json` — see
// ISO for the key-style rationale.
type Template struct {
	VolID   string `json:"volid"`
	Storage string `json:"storage"`
	Size    int64  `json:"size"`
}

// ListTemplates returns every vztmpl template found across storages
// (storage IDs mounted on node), sorted by volid. Same fan-out-then-merge
// shape as ListBackups — the per-storage GET
// /nodes/{node}/storage/{storage}/content calls are mutually independent,
// so N storages cost ~1 round trip instead of N sequential ones — but
// filters on Content == "vztmpl" instead of "backup" and carries no vmid,
// since a template isn't owned by any guest.
func (c *Client) ListTemplates(ctx context.Context, node string, storages []string) ([]Template, error) {
	type result struct {
		templates []Template
		err       error
	}
	results := make([]result, len(storages))
	var wg sync.WaitGroup
	for i, storage := range storages {
		wg.Add(1)
		go func(i int, storage string) {
			defer wg.Done()
			path := fmt.Sprintf("/nodes/%s/storage/%s/content", node, storage)
			var resp storageContentResponse
			if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
				results[i].err = fmt.Errorf("listing content on %s/%s: %w", node, storage, err)
				return
			}
			for _, e := range resp.Data {
				if e.Content != "vztmpl" {
					continue
				}
				results[i].templates = append(results[i].templates, Template{
					VolID:   e.VolID,
					Storage: storage,
					Size:    int64(e.Size),
				})
			}
		}(i, storage)
	}
	wg.Wait()

	var templates []Template
	for _, r := range results {
		if r.err != nil {
			return nil, r.err
		}
		templates = append(templates, r.templates...)
	}
	sort.Slice(templates, func(i, j int) bool { return templates[i].VolID < templates[j].VolID })
	return templates, nil
}
