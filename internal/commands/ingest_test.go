package commands

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIngestBufferCommandsShowSetReset(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/config/runtime/ingest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer admin-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("x-arc-database") != "" {
			t.Errorf("unexpected database header %q", r.Header.Get("x-arc-database"))
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
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	writeTestConfig(t, srv.URL, "admin-token")

	out, _, err := execCmd(t, newIngestCmd(), "buffer", "show")
	if err != nil || !strings.Contains(out, "max_buffer_size:  50000") || !strings.Contains(out, "persistent:        false") {
		t.Fatalf("show output=%q err=%v", out, err)
	}
	out, _, err = execCmd(t, newIngestCmd(), "buffer", "set", "--max-buffer-age-ms", "9000")
	if err != nil || !strings.Contains(out, "Updated runtime ingest buffer settings") || !strings.Contains(out, "max_buffer_age_ms: 9000") {
		t.Fatalf("set output=%q err=%v", out, err)
	}
	out, _, err = execCmd(t, newIngestCmd(), "buffer", "reset", "-o", "json")
	if err != nil || !strings.Contains(out, `"persistent": false`) || !strings.Contains(out, `"source": "startup_config"`) {
		t.Fatalf("reset output=%q err=%v", out, err)
	}
}

func TestIngestBufferSetRequiresValidChangedValues(t *testing.T) {
	for _, args := range [][]string{
		{"buffer", "set"},
		{"buffer", "set", "--max-buffer-size", "0"},
		{"buffer", "set", "--max-buffer-age-ms", "-1"},
	} {
		if _, _, err := execCmd(t, newIngestCmd(), args...); err == nil {
			t.Errorf("arcli ingest %v succeeded; want validation error", args)
		}
	}
}

func TestIngestBufferSetJSONOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"max_buffer_size":120000}` {
			t.Errorf("request body = %s", body)
		}
		_, _ = w.Write([]byte(`{"max_buffer_size":120000,"max_buffer_age_ms":5000,"scope":"current_process","persistent":true,"source":"persistent_override"}`))
	}))
	t.Cleanup(srv.Close)
	writeTestConfig(t, srv.URL, "admin-token")
	out, _, err := execCmd(t, newIngestCmd(), "buffer", "set", "--max-buffer-size", "120000", "-o", "json")
	if err != nil || !strings.Contains(out, `"max_buffer_size": 120000`) || !strings.Contains(out, `"persistent": true`) {
		t.Fatalf("JSON output=%q err=%v", out, err)
	}
}
