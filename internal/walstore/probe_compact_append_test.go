package walstore

import (
	"bytes"
	"path/filepath"
	"testing"
)

// TestProbeCompactThenSetPersists verifies that writes made after compaction
// are properly persisted and survive a reopen. This catches issues where the
// file is not reopened in append mode after compaction.
func TestProbeCompactThenSetPersists(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.wal")

	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}

	// Write some initial data and compact.
	if err := s.Set([]byte("pre"), []byte("before")); err != nil {
		t.Fatal(err)
	}
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}

	// Write after compaction.
	if err := s.Set([]byte("post"), []byte("after")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen and verify both keys survived.
	s2, err := Open(p)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	if v, ok := s2.Get([]byte("pre")); !ok || !bytes.Equal(v, []byte("before")) {
		t.Errorf("pre = %q,%v; want before,true", v, ok)
	}
	if v, ok := s2.Get([]byte("post")); !ok || !bytes.Equal(v, []byte("after")) {
		t.Errorf("post = %q,%v; want after,true (write after compact lost)", v, ok)
	}
}
