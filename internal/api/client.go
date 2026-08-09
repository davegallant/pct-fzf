package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// requestTimeout bounds every request so a network failure (dead route,
// firewall silently dropping packets, unreachable host) fails with a clear
// error instead of hanging the CLI forever.
const requestTimeout = 30 * time.Second

// Client talks to a Proxmox VE cluster's REST API using API token auth.
type Client struct {
	baseURL     string
	tokenID     string
	tokenSecret string
	httpClient  *http.Client
	debug       bool
	debugOut    io.Writer
}

// NewClient builds a Client. host must include scheme and port, e.g.
// "https://pve.example.com:8006".
func NewClient(host, tokenID, tokenSecret string, insecureSkipVerify bool) *Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecureSkipVerify},
	}
	return &Client{
		baseURL:     strings.TrimRight(host, "/") + "/api2/json",
		tokenID:     tokenID,
		tokenSecret: tokenSecret,
		httpClient:  &http.Client{Transport: transport, Timeout: requestTimeout},
		debugOut:    os.Stderr,
	}
}

// SetDebug enables or disables logging of request method/path and
// response status/duration (or error) to the client's debug output
// (stderr by default). Never logs the Authorization header or request
// body, since the token secret must never be written to a log.
func (c *Client) SetDebug(debug bool) {
	c.debug = debug
}

func (c *Client) logf(format string, args ...any) {
	if !c.debug {
		return
	}
	_, _ = fmt.Fprintf(c.debugOut, "[debug] "+format+"\n", args...)
}

type apiError struct {
	Errors  map[string]string `json:"errors"`
	Message string            `json:"message"`
}

// Error renders both Message and any per-field Errors when Proxmox sends
// both — a generic "Parameter verification failed." Message is otherwise
// useless on its own; the Errors map is what actually says which
// parameter was wrong and why (e.g. "rootfs: format error - ..."). Fields
// are sorted for deterministic output, since map iteration order isn't
// stable. When Errors is empty, Message alone is returned unchanged (the
// common case: most Proxmox error replies, e.g. a plain permission
// check failure, only ever set Message).
func (e *apiError) Error() string {
	if len(e.Errors) > 0 {
		fields := make([]string, 0, len(e.Errors))
		for field := range e.Errors {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		parts := make([]string, len(fields))
		for i, field := range fields {
			parts[i] = fmt.Sprintf("%s: %s", field, e.Errors[field])
		}
		detail := strings.Join(parts, "; ")
		if e.Message != "" {
			return fmt.Sprintf("%s (%s)", strings.TrimSpace(e.Message), detail)
		}
		return detail
	}
	if e.Message != "" {
		return e.Message
	}
	return "unknown proxmox api error"
}

func isCertError(err error) bool {
	var unknownAuth x509.UnknownAuthorityError
	var certInvalid x509.CertificateInvalidError
	var hostnameErr x509.HostnameError
	return errors.As(err, &unknownAuth) || errors.As(err, &certInvalid) || errors.As(err, &hostnameErr)
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader, out any) error {
	url := c.baseURL + path
	start := time.Now()
	c.logf("--> %s %s", method, url)

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s=%s", c.tokenID, c.tokenSecret))
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.logf("<-- error after %s: %v", time.Since(start), err)
		if isCertError(err) {
			return fmt.Errorf("TLS verify failed — if this cluster uses a self-signed cert, re-run 'pvectl setup --insecure-skip-verify': %w", err)
		}
		return fmt.Errorf("proxmox api request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading proxmox api response: %w", err)
	}

	c.logf("<-- %d (%s)", resp.StatusCode, time.Since(start))

	if resp.StatusCode >= 400 {
		var apiErr apiError
		if jsonErr := json.Unmarshal(respBody, &apiErr); jsonErr == nil {
			return &apiErr
		}
		return fmt.Errorf("proxmox api returned status %d", resp.StatusCode)
	}

	if out == nil {
		return nil
	}
	// A handful of Proxmox endpoints (e.g. some DELETEs) reply with a
	// 2xx status and a genuinely empty body — decoding that into any
	// out type, including json.RawMessage, would otherwise fail with
	// io.EOF. Leave *out at its zero value instead of erroring; this
	// matters for RawRequest (cmd `api` passthrough), which has no
	// fixed response shape to fall back on.
	if len(respBody) == 0 {
		return nil
	}
	// Use a Decoder with UseNumber so that numeric values decoded into
	// interface{} (e.g. map[string]any) retain their original literal
	// text as json.Number instead of being converted to float64, which
	// would render large values (>= 1e6) in scientific notation.
	dec := json.NewDecoder(bytes.NewReader(respBody))
	dec.UseNumber()
	return dec.Decode(out)
}

type versionResponse struct {
	Data struct {
		Version string `json:"version"`
	} `json:"data"`
}

func (c *Client) Version(ctx context.Context) (string, error) {
	var resp versionResponse
	if err := c.do(ctx, http.MethodGet, "/version", nil, &resp); err != nil {
		return "", err
	}
	return resp.Data.Version, nil
}

// Container is an LXC container as returned by /cluster/resources.
type Container struct {
	VMID   int    `json:"vmid"`
	Name   string `json:"name"`
	Node   string `json:"node"`
	Status string `json:"status"`
	// Tags is never nil, so `-o json` emits [] rather than null for an
	// untagged guest — a stable schema for consumers parsing the output.
	Tags []string `json:"tags"`
}

type resourcesResponse struct {
	Data []resourceEntry `json:"data"`
}

type resourceEntry struct {
	VMID       int       `json:"vmid"`
	Name       string    `json:"name"`
	Node       string    `json:"node"`
	Status     string    `json:"status"`
	Type       string    `json:"type"`
	CPU        float64   `json:"cpu"`
	MaxCPU     int       `json:"maxcpu"`
	Mem        int64     `json:"mem"`
	MaxMem     int64     `json:"maxmem"`
	Disk       int64     `json:"disk"`
	MaxDisk    int64     `json:"maxdisk"`
	Storage    string    `json:"storage"`
	PluginType string    `json:"plugintype"`
	Shared     looseBool `json:"shared"`
	Tags       string    `json:"tags"`
}

// looseBool decodes a JSON bool, number (0/1), or numeric/bool string
// into a bool, defaulting to false for a missing/null field. Added for
// storage's "shared" flag: its wire type hasn't been confirmed against a
// real cluster from this sandbox, and — per the looseInt64 precedent in
// backup.go, where Proxmox sent supposedly-numeric fields as JSON
// strings on a different endpoint — a strict int/bool type here would
// risk failing all of ClusterResources (not just storage collapsing) if
// the assumption is wrong.
type looseBool bool

func (b *looseBool) UnmarshalJSON(data []byte) error {
	switch s := strings.Trim(string(data), `"`); s {
	case "", "null", "0", "false":
		*b = false
	default:
		*b = true
	}
	return nil
}

// fetchResources fetches every entry from /cluster/resources, unfiltered.
// Shared by ListContainers (filters to type "lxc") and ClusterResources
// (buckets every type).
func (c *Client) fetchResources(ctx context.Context) ([]resourceEntry, error) {
	var resp resourcesResponse
	if err := c.do(ctx, http.MethodGet, "/cluster/resources", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// ListContainers returns every LXC container across the cluster, sorted by
// VMID ascending. /cluster/resources does not guarantee any particular
// order, so without sorting the picker's display order is effectively
// random from run to run — sorting also matches the order tools like
// `pct list` and `lxc-ls` present.
func (c *Client) ListContainers(ctx context.Context) ([]Container, error) {
	entries, err := c.fetchResources(ctx)
	if err != nil {
		return nil, err
	}

	var containers []Container
	for _, r := range entries {
		if r.Type != "lxc" {
			continue
		}
		containers = append(containers, Container{
			VMID:   r.VMID,
			Name:   r.Name,
			Node:   r.Node,
			Status: r.Status,
			Tags:   parseTags(r.Tags),
		})
	}
	sort.Slice(containers, func(i, j int) bool {
		return containers[i].VMID < containers[j].VMID
	})
	return containers, nil
}
