package client

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestRuntimeIngestConfigMethods(t *testing.T) {
	cli, _ := newAuthTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeaders(t, r)
		if r.URL.Path != runtimeIngestConfigPath {
			t.Errorf("path = %q, want %q", r.URL.Path, runtimeIngestConfigPath)
		}
		if r.Header.Get(HeaderDatabase) != "" {
			t.Errorf("runtime config request unexpectedly sent database header %q", r.Header.Get(HeaderDatabase))
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"max_buffer_size":50000,"max_buffer_age_ms":5000,"scope":"current_process","persistent":false,"source":"startup_config"}`))
		case http.MethodPatch:
			var body map[string]int
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode PATCH body: %v", err)
			}
			if len(body) != 1 || body["max_buffer_age_ms"] != 9000 {
				t.Errorf("PATCH body = %#v; want only max_buffer_age_ms=9000", body)
			}
			_, _ = w.Write([]byte(`{"max_buffer_size":50000,"max_buffer_age_ms":9000,"scope":"current_process","persistent":true,"source":"persistent_override"}`))
		case http.MethodDelete:
			_, _ = w.Write([]byte(`{"max_buffer_size":50000,"max_buffer_age_ms":5000,"scope":"current_process","persistent":false,"source":"startup_config"}`))
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	})

	ctx := context.Background()
	current, err := cli.RuntimeIngestConfig(ctx)
	if err != nil || current.Persistent || current.MaxBufferSize != 50000 {
		t.Fatalf("RuntimeIngestConfig() = %+v, %v", current, err)
	}
	age := 9000
	updated, err := cli.PatchRuntimeIngestConfig(ctx, RuntimeIngestConfigPatch{MaxBufferAgeMS: &age})
	if err != nil || !updated.Persistent || updated.MaxBufferAgeMS != age {
		t.Fatalf("PatchRuntimeIngestConfig() = %+v, %v", updated, err)
	}
	reset, err := cli.ResetRuntimeIngestConfig(ctx)
	if err != nil || reset.Persistent || reset.Source != "startup_config" {
		t.Fatalf("ResetRuntimeIngestConfig() = %+v, %v", reset, err)
	}
}

func TestPatchRuntimeIngestConfigValidatesLocally(t *testing.T) {
	cli, _ := newAuthTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
	})
	for _, patch := range []RuntimeIngestConfigPatch{
		{},
		{MaxBufferSize: intPointer(0)},
		{MaxBufferAgeMS: intPointer(-1)},
	} {
		if _, err := cli.PatchRuntimeIngestConfig(context.Background(), patch); err == nil {
			t.Errorf("PatchRuntimeIngestConfig(%+v) succeeded", patch)
		}
	}
}

func intPointer(v int) *int { return &v }

func TestRuntimeIngestConfigReturnsArcHTTPError(t *testing.T) {
	cli, _ := newAuthTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"Permission denied: admin required"}`))
	})
	_, err := cli.RuntimeIngestConfig(context.Background())
	if err == nil || !strings.Contains(err.Error(), "admin required") {
		t.Fatalf("error = %v; want admin permission error", err)
	}
}
