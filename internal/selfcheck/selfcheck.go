// Package selfcheck runs an end-to-end self-test of the WAL KV engine that
// exercises every documented boundary condition. It is invoked via
// `--smoke-test` and exits non-zero on any failure.
package selfcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"task025-walkv/internal/httpapi"
	"task025-walkv/internal/walstore"
)

// Run executes the self-check. It returns a non-nil error if any assertion
// fails. All work happens under a temporary directory and an in-process HTTP
// server; nothing touches the network or external services.
func Run() error {
	dir, err := os.MkdirTemp("", "walkv-smoke-*")
	if err != nil {
		return fmt.Errorf("mkdtemp: %w", err)
	}
	defer os.RemoveAll(dir)

	walPath := filepath.Join(dir, "data.wal")

	// --- 1. Basic set/get/delete on a fresh store ---
	s, err := walstore.Open(walPath)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	if err := s.Set([]byte("k1"), []byte("v1")); err != nil {
		return fmt.Errorf("set k1: %w", err)
	}
	if err := s.Set([]byte("k2"), []byte("v2")); err != nil {
		return fmt.Errorf("set k2: %w", err)
	}
	if v, ok := s.Get([]byte("k1")); !ok || !bytes.Equal(v, []byte("v1")) {
		return fmt.Errorf("get k1 = %q,%v; want v1,true", v, ok)
	}
	if err := s.Delete([]byte("k1")); err != nil {
		return fmt.Errorf("delete k1: %w", err)
	}
	if _, ok := s.Get([]byte("k1")); ok {
		return fmt.Errorf("k1 should be deleted")
	}
	if s.Len() != 1 {
		return fmt.Errorf("len after delete = %d; want 1", s.Len())
	}
	if err := s.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}

	// --- 2. Recovery after reopen (tombstone semantics) ---
	s, err = walstore.Open(walPath)
	if err != nil {
		return fmt.Errorf("reopen: %w", err)
	}
	if _, ok := s.Get([]byte("k1")); ok {
		return fmt.Errorf("k1 should not exist after recovery")
	}
	if v, ok := s.Get([]byte("k2")); !ok || !bytes.Equal(v, []byte("v2")) {
		return fmt.Errorf("get k2 after recovery = %q,%v; want v2,true", v, ok)
	}
	// Multiple sets then a tombstone: the key must stay deleted after recovery.
	if err := s.Set([]byte("k1"), []byte("v1a")); err != nil {
		return err
	}
	if err := s.Set([]byte("k1"), []byte("v1b")); err != nil {
		return err
	}
	if err := s.Delete([]byte("k1")); err != nil {
		return err
	}
	if err := s.Close(); err != nil {
		return err
	}

	s, err = walstore.Open(walPath)
	if err != nil {
		return fmt.Errorf("reopen2: %w", err)
	}
	if _, ok := s.Get([]byte("k1")); ok {
		return fmt.Errorf("k1 should not exist after tombstone recovery")
	}
	if v, ok := s.Get([]byte("k2")); !ok || !bytes.Equal(v, []byte("v2")) {
		return fmt.Errorf("get k2 after tombstone recovery = %q,%v; want v2,true", v, ok)
	}

	// --- 3. Compaction equivalence ---
	for i := 0; i < 30; i++ {
		if err := s.Set([]byte(fmt.Sprintf("hot%d", i)), []byte(fmt.Sprintf("val%d", i))); err != nil {
			return err
		}
	}
	// Rewrite the hot keys several times to bloat the WAL.
	for j := 0; j < 4; j++ {
		for i := 0; i < 30; i++ {
			if err := s.Set([]byte(fmt.Sprintf("hot%d", i)), []byte(fmt.Sprintf("val%d-r%d", i, j))); err != nil {
				return err
			}
		}
	}
	sizeBefore, err := s.WALSize()
	if err != nil {
		return err
	}
	want := s.Snapshot()
	if err := s.Compact(); err != nil {
		return fmt.Errorf("compact: %w", err)
	}
	sizeAfter, err := s.WALSize()
	if err != nil {
		return err
	}
	if sizeAfter >= sizeBefore {
		return fmt.Errorf("compaction did not shrink WAL: before=%d after=%d", sizeBefore, sizeAfter)
	}
	if err := assertSame(s.Snapshot(), want); err != nil {
		return fmt.Errorf("post-compact state mismatch: %w", err)
	}
	if err := s.Close(); err != nil {
		return err
	}

	// Reopen after compaction: state must match (compaction equivalence).
	s, err = walstore.Open(walPath)
	if err != nil {
		return fmt.Errorf("reopen3: %w", err)
	}
	if err := assertSame(s.Snapshot(), want); err != nil {
		return fmt.Errorf("post-compact recovery mismatch: %w", err)
	}
	if err := s.Close(); err != nil {
		return err
	}

	// --- 4. Tail truncation tolerance ---
	walPath2 := filepath.Join(dir, "trunc.wal")
	s, err = walstore.Open(walPath2)
	if err != nil {
		return err
	}
	if err := s.Set([]byte("a"), []byte("1")); err != nil {
		return err
	}
	if err := s.Set([]byte("b"), []byte("2")); err != nil {
		return err
	}
	if err := s.Set([]byte("c"), []byte("3")); err != nil {
		return err
	}
	if err := s.Close(); err != nil {
		return err
	}
	fi, err := os.Stat(walPath2)
	if err != nil {
		return err
	}
	origSize := fi.Size()
	if err := os.Truncate(walPath2, origSize-6); err != nil {
		return err
	}
	s, err = walstore.Open(walPath2)
	if err != nil {
		return fmt.Errorf("open truncated WAL: %w", err)
	}
	if v, ok := s.Get([]byte("a")); !ok || !bytes.Equal(v, []byte("1")) {
		return fmt.Errorf("after truncation, get a = %q,%v; want 1,true", v, ok)
	}
	if v, ok := s.Get([]byte("b")); !ok || !bytes.Equal(v, []byte("2")) {
		return fmt.Errorf("after truncation, get b = %q,%v; want 2,true", v, ok)
	}
	if _, ok := s.Get([]byte("c")); ok {
		return fmt.Errorf("after truncation, c should be gone")
	}
	fi2, err := os.Stat(walPath2)
	if err != nil {
		return err
	}
	if fi2.Size() >= origSize {
		return fmt.Errorf("truncated WAL not trimmed: size=%d orig=%d", fi2.Size(), origSize)
	}
	// New writes after truncation recovery must persist and survive reopen.
	if err := s.Set([]byte("c"), []byte("3-recovered")); err != nil {
		return err
	}
	if err := s.Close(); err != nil {
		return err
	}
	s, err = walstore.Open(walPath2)
	if err != nil {
		return err
	}
	if v, ok := s.Get([]byte("c")); !ok || !bytes.Equal(v, []byte("3-recovered")) {
		return fmt.Errorf("post-truncation append recovery failed: %q,%v", v, ok)
	}
	if err := s.Close(); err != nil {
		return err
	}

	// --- 5. HTTP wiring (httptest, no real network) ---
	if err := httpSmoke(filepath.Join(dir, "http.wal")); err != nil {
		return fmt.Errorf("http smoke: %w", err)
	}

	fmt.Println("selfcheck: all assertions passed")
	return nil
}

// httpSmoke exercises the HTTP API against an in-process server.
func httpSmoke(walPath string) error {
	s, err := walstore.Open(walPath)
	if err != nil {
		return err
	}
	defer s.Close()
	ts := httptest.NewServer(httpapi.NewServer(s).Handler())
	defer ts.Close()

	// set
	resp, err := http.Post(ts.URL+"/set", "application/json", strings.NewReader(`{"key":"hk","value":"hv"}`))
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST /set status = %d", resp.StatusCode)
	}

	// get
	resp, err = http.Get(ts.URL + "/get?key=hk")
	if err != nil {
		return err
	}
	var gr struct {
		Found bool   `json:"found"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&gr); err != nil {
		return err
	}
	resp.Body.Close()
	if !gr.Found || gr.Value != "hv" {
		return fmt.Errorf("GET /get = %+v; want found hv", gr)
	}

	// get missing
	resp, err = http.Get(ts.URL + "/get?key=nope")
	if err != nil {
		return err
	}
	var gr2 struct {
		Found bool `json:"found"`
	}
	json.NewDecoder(resp.Body).Decode(&gr2)
	resp.Body.Close()
	if gr2.Found {
		return fmt.Errorf("GET missing key should report found=false")
	}

	// delete
	resp, err = http.Post(ts.URL+"/delete", "application/json", strings.NewReader(`{"key":"hk"}`))
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST /delete status = %d", resp.StatusCode)
	}
	resp, _ = http.Get(ts.URL + "/get?key=hk")
	var gr3 struct {
		Found bool `json:"found"`
	}
	json.NewDecoder(resp.Body).Decode(&gr3)
	resp.Body.Close()
	if gr3.Found {
		return fmt.Errorf("deleted key should be absent")
	}

	// stats
	resp, err = http.Get(ts.URL + "/stats")
	if err != nil {
		return err
	}
	var sr struct {
		Keys     int   `json:"keys"`
		WALBytes int64 `json:"wal_bytes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return err
	}
	resp.Body.Close()
	if sr.Keys < 0 || sr.WALBytes < 0 {
		return fmt.Errorf("stats = %+v; want non-negative", sr)
	}

	// compact
	resp, err = http.Post(ts.URL+"/compact", "application/json", nil)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST /compact status = %d", resp.StatusCode)
	}

	return nil
}

// assertSame reports whether got matches want (both full snapshots).
func assertSame(got, want map[string][]byte) error {
	if len(got) != len(want) {
		return fmt.Errorf("key count %d != %d", len(got), len(want))
	}
	for k, v := range want {
		g, ok := got[k]
		if !ok {
			return fmt.Errorf("missing key %q", k)
		}
		if !bytes.Equal(g, v) {
			return fmt.Errorf("key %q = %q; want %q", k, g, v)
		}
	}
	return nil
}
