package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"task025-walkv/internal/walstore"
)

// TestProbeGetMissingKeyReturns404 verifies that requesting a non-existent key
// via the HTTP GET endpoint returns HTTP 404 (Not Found), not 200.
func TestProbeGetMissingKeyReturns404(t *testing.T) {
	dir := t.TempDir()
	s, err := walstore.Open(filepath.Join(dir, "test.wal"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	defer os.RemoveAll(dir)

	srv := httptest.NewServer(NewServer(s).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/get?key=nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET missing key: status=%d; want 404", resp.StatusCode)
	}
}
