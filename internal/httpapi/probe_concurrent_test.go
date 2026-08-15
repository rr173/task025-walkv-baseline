package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"task025-walkv/internal/walstore"
)

// TestProbeConcurrentHTTPRequestsSafe verifies that the HTTP server can handle
// many concurrent requests without panicking from unsynchronized shared state.
func TestProbeConcurrentHTTPRequestsSafe(t *testing.T) {
	dir := t.TempDir()
	s, err := walstore.Open(filepath.Join(dir, "test.wal"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	defer os.RemoveAll(dir)

	srv := httptest.NewServer(NewServer(s).Handler())
	defer srv.Close()

	const workers = 80
	const iterations = 30

	var wg sync.WaitGroup
	wg.Add(workers)

	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				resp, err := http.Post(srv.URL+"/set", "application/json",
					strings.NewReader(`{"key":"k","value":"v"}`))
				if err != nil {
					t.Errorf("request error: %v", err)
					return
				}
				resp.Body.Close()
			}
		}()
	}

	wg.Wait()
}
