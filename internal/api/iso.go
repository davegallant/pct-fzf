package api

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
)

// ISO is one ISO image (iso content) found on a storage's content listing.
// JSON tags are for `pvectl iso list -o json`, matching the lowercase key
// style every other JSON-output struct uses.
type ISO struct {
	VolID   string `json:"volid"`
	Storage string `json:"storage"`
	Size    int64  `json:"size"`
}

// ListISOs is ListTemplates' mirror for ISO images: same
// fan-out-then-merge shape across storages (one GET
// /nodes/{node}/storage/{storage}/content call per storage, run
// concurrently), filtering on Content == "iso" instead of "vztmpl".
func (c *Client) ListISOs(ctx context.Context, node string, storages []string) ([]ISO, error) {
	type result struct {
		isos []ISO
		err  error
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
				if e.Content != "iso" {
					continue
				}
				results[i].isos = append(results[i].isos, ISO{
					VolID:   e.VolID,
					Storage: storage,
					Size:    int64(e.Size),
				})
			}
		}(i, storage)
	}
	wg.Wait()

	var isos []ISO
	for _, r := range results {
		if r.err != nil {
			return nil, r.err
		}
		isos = append(isos, r.isos...)
	}
	sort.Slice(isos, func(i, j int) bool { return isos[i].VolID < isos[j].VolID })
	return isos, nil
}
