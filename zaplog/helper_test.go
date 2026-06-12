package zaplog

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	collogspb "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/proto"
)

// capturedRequest records one OTLP/HTTP export the test server received.
type capturedRequest struct {
	path            string
	contentType     string
	contentEncoding string
	headers         http.Header
	req             *collogspb.ExportLogsServiceRequest
}

// captureServer is an httptest server that decodes ExportLogsServiceRequest
// bodies (gzip-aware) and records them for assertions.
type captureServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []capturedRequest
}

func newCaptureServer(t *testing.T) *captureServer {
	t.Helper()
	cs := &captureServer{}
	cs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}
		if r.Header.Get("Content-Encoding") == "gzip" {
			zr, gerr := gzip.NewReader(bytes.NewReader(body))
			if gerr != nil {
				http.Error(w, gerr.Error(), http.StatusBadRequest)

				return
			}
			body, gerr = io.ReadAll(zr)
			if gerr != nil {
				http.Error(w, gerr.Error(), http.StatusBadRequest)

				return
			}
		}
		var req collogspb.ExportLogsServiceRequest
		if uerr := proto.Unmarshal(body, &req); uerr != nil {
			http.Error(w, uerr.Error(), http.StatusBadRequest)

			return
		}
		cs.mu.Lock()
		cs.requests = append(cs.requests, capturedRequest{
			path:            r.URL.Path,
			contentType:     r.Header.Get("Content-Type"),
			contentEncoding: r.Header.Get("Content-Encoding"),
			headers:         r.Header.Clone(),
			req:             &req,
		})
		cs.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(cs.Close)

	return cs
}

func (cs *captureServer) snapshot() []capturedRequest {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	out := make([]capturedRequest, len(cs.requests))
	copy(out, cs.requests)

	return out
}

// allRecords flattens every LogRecord across captured requests.
func (cs *captureServer) allRecords() []*logspb.LogRecord {
	var recs []*logspb.LogRecord
	for _, c := range cs.snapshot() {
		for _, rl := range c.req.GetResourceLogs() {
			for _, sl := range rl.GetScopeLogs() {
				recs = append(recs, sl.GetLogRecords()...)
			}
		}
	}

	return recs
}

// firstResource returns the first ResourceLogs.Resource attributes captured.
func (cs *captureServer) resourceAttrs(t *testing.T) []*commonpb.KeyValue {
	t.Helper()
	snap := cs.snapshot()
	require.NotEmpty(t, snap, "no requests captured")
	rls := snap[0].req.GetResourceLogs()
	require.NotEmpty(t, rls, "no ResourceLogs in request")

	return rls[0].GetResource().GetAttributes()
}

// schemaURLs returns every ResourceLogs.schema_url captured.
func (cs *captureServer) schemaURLs() []string {
	var urls []string
	for _, c := range cs.snapshot() {
		for _, rl := range c.req.GetResourceLogs() {
			urls = append(urls, rl.GetSchemaUrl())
		}
	}

	return urls
}

func attrValue(attrs []*commonpb.KeyValue, key string) (*commonpb.AnyValue, bool) {
	for _, a := range attrs {
		if a.GetKey() == key {
			return a.GetValue(), true
		}
	}

	return nil, false
}

func countKey(attrs []*commonpb.KeyValue, key string) int {
	n := 0
	for _, a := range attrs {
		if a.GetKey() == key {
			n++
		}
	}

	return n
}
