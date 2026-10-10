package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const cloudflareAPI = "https://api.cloudflare.com/client/v4"

// Record is a Cloudflare DNS record, limited to the fields this tool uses.
type Record struct {
	ID      string   `json:"id,omitempty"`
	Type    string   `json:"type"`
	Name    string   `json:"name"`
	Content string   `json:"content"`
	TTL     int      `json:"ttl,omitempty"`
	Proxied bool     `json:"proxied"`
	Comment string   `json:"comment,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

// CloudflareClient manages DNS records in one zone with a scoped API token.
type CloudflareClient struct {
	base   string
	token  string
	zoneID string
	http   *http.Client
}

// NewCloudflareClient returns a client for zoneID.
func NewCloudflareClient(token, zoneID string) *CloudflareClient {
	return &CloudflareClient{
		base:   cloudflareAPI,
		token:  token,
		zoneID: zoneID,
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

type cfResponse struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result     json.RawMessage `json:"result"`
	ResultInfo struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

func (c *CloudflareClient) do(ctx context.Context, method, path string, body any) (*cfResponse, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out cfResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("HTTP %d: undecodable response: %w", resp.StatusCode, err)
	}
	if !out.Success {
		var msgs []string
		for _, e := range out.Errors {
			msgs = append(msgs, fmt.Sprintf("%d %s", e.Code, e.Message))
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.Join(msgs, "; "))
	}
	return &out, nil
}

// ListRecords returns every A, AAAA and CNAME record in the zone.
func (c *CloudflareClient) ListRecords(ctx context.Context) ([]Record, error) {
	var all []Record
	for page := 1; ; page++ {
		q := url.Values{"per_page": {"100"}, "page": {fmt.Sprint(page)}}
		out, err := c.do(ctx, http.MethodGet, "/zones/"+c.zoneID+"/dns_records?"+q.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("list records: %w", err)
		}
		var records []Record
		if err := json.Unmarshal(out.Result, &records); err != nil {
			return nil, fmt.Errorf("list records: %w", err)
		}
		for _, r := range records {
			if r.Type == "A" || r.Type == "AAAA" || r.Type == "CNAME" {
				all = append(all, r)
			}
		}
		if out.ResultInfo.TotalPages <= page {
			return all, nil
		}
	}
}

// CreateRecord creates r and returns the stored record.
func (c *CloudflareClient) CreateRecord(ctx context.Context, r Record) (Record, error) {
	out, err := c.do(ctx, http.MethodPost, "/zones/"+c.zoneID+"/dns_records", r)
	if err != nil {
		return Record{}, fmt.Errorf("create %s: %w", r.Name, err)
	}
	var created Record
	if err := json.Unmarshal(out.Result, &created); err != nil {
		return Record{}, fmt.Errorf("create %s: %w", r.Name, err)
	}
	return created, nil
}

// DeleteRecord deletes the record with id.
func (c *CloudflareClient) DeleteRecord(ctx context.Context, id string) error {
	if _, err := c.do(ctx, http.MethodDelete, "/zones/"+c.zoneID+"/dns_records/"+id, nil); err != nil {
		return fmt.Errorf("delete %s: %w", id, err)
	}
	return nil
}
