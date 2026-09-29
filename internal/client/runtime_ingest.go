package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const runtimeIngestConfigPath = "/api/v1/config/runtime/ingest"

// RuntimeIngestConfig is the process-wide ingest buffer configuration Arc
// currently applies.
type RuntimeIngestConfig struct {
	MaxBufferSize  int             `json:"max_buffer_size"`
	MaxBufferAgeMS int             `json:"max_buffer_age_ms"`
	Scope          string          `json:"scope"`
	Persistent     bool            `json:"persistent"`
	Source         string          `json:"source"`
	Raw            json.RawMessage `json:"-"`
}

// RuntimeIngestConfigPatch contains only the thresholds the caller wants to
// change. At least one field must be provided.
type RuntimeIngestConfigPatch struct {
	MaxBufferSize  *int `json:"max_buffer_size,omitempty"`
	MaxBufferAgeMS *int `json:"max_buffer_age_ms,omitempty"`
}

// RuntimeIngestConfig calls GET /api/v1/config/runtime/ingest (admin).
func (c *Client) RuntimeIngestConfig(ctx context.Context) (*RuntimeIngestConfig, error) {
	body, err := c.runtimeIngestJSON(ctx, http.MethodGet, nil)
	if err != nil {
		return nil, err
	}
	var out RuntimeIngestConfig
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode runtime ingest config: %w", err)
	}
	out.Raw = body
	return &out, nil
}

// PatchRuntimeIngestConfig calls PATCH /api/v1/config/runtime/ingest (admin).
func (c *Client) PatchRuntimeIngestConfig(ctx context.Context, patch RuntimeIngestConfigPatch) (*RuntimeIngestConfig, error) {
	if patch.MaxBufferSize == nil && patch.MaxBufferAgeMS == nil {
		return nil, fmt.Errorf("provide at least one runtime ingest setting")
	}
	if patch.MaxBufferSize != nil && *patch.MaxBufferSize <= 0 {
		return nil, fmt.Errorf("max_buffer_size must be greater than zero")
	}
	if patch.MaxBufferAgeMS != nil && *patch.MaxBufferAgeMS <= 0 {
		return nil, fmt.Errorf("max_buffer_age_ms must be greater than zero")
	}
	body, err := c.runtimeIngestJSON(ctx, http.MethodPatch, patch)
	if err != nil {
		return nil, err
	}
	var out RuntimeIngestConfig
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode runtime ingest config: %w", err)
	}
	out.Raw = body
	return &out, nil
}

// ResetRuntimeIngestConfig calls DELETE /api/v1/config/runtime/ingest (admin)
// to remove the saved override and return to startup configuration.
func (c *Client) ResetRuntimeIngestConfig(ctx context.Context) (*RuntimeIngestConfig, error) {
	body, err := c.runtimeIngestJSON(ctx, http.MethodDelete, nil)
	if err != nil {
		return nil, err
	}
	var out RuntimeIngestConfig
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode runtime ingest config: %w", err)
	}
	out.Raw = body
	return &out, nil
}

func (c *Client) runtimeIngestJSON(ctx context.Context, method string, value any) ([]byte, error) {
	var body io.Reader
	if value != nil {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode runtime ingest config: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.Endpoint+runtimeIngestConfigPath, body)
	if err != nil {
		return nil, fmt.Errorf("build runtime ingest config request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if value != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.setCrossDBHeaders(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, runtimeIngestConfigPath, err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read runtime ingest config response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, decodeWriteError(resp.StatusCode, responseBody)
	}
	if trimmed := bytes.TrimSpace(responseBody); len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, fmt.Errorf("%s %s: empty response from server", method, runtimeIngestConfigPath)
	}
	return responseBody, nil
}
