package walstore

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSetGetDelete(t *testing.T) {
	s, close := openTemp(t)
	defer close()

	if err := s.Set([]byte("k"), []byte("v")); err != nil {
		t.Fatal(err)
	}
	got, ok := s.Get([]byte("k"))
	if !ok || !bytes.Equal(got, []byte("v")) {
		t.Fatalf("Get(k) = %q,%v; want v,true", got, ok)
	}
	if err := s.Delete([]byte("k")); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get([]byte("k")); ok {
		t.Fatal("k should be deleted")
	}
	// Idempotent delete of a missing key still succeeds.
	if err := s.Delete([]byte("k")); err != nil {
		t.Fatalf("idempotent delete failed: %v", err)
	}
}

func TestEmptyAndTooLongKey(t *testing.T) {
	s, close := openTemp(t)
	defer close()

	if err := s.Set(nil, []byte("v")); err == nil {
		t.Fatal("Set with empty key should fail")
	}
	long := make([]byte, maxKeyLen+1)
	if err := s.Set(long, []byte("v")); err == nil {
		t.Fatal("Set with oversized key should fail")
	}
	if err := s.Delete(nil); err == nil {
		t.Fatal("Delete with empty key should fail")
	}
}

func TestRecoveryTombstoneAfterMultipleSets(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.wal")

	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set([]byte("k"), []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if err := s.Set([]byte("k"), []byte("v2")); err != nil {
		t.Fatal(err)
	}
	if err := s.Set([]byte("k"), []byte("v3")); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete([]byte("k")); err != nil {
		t.Fatal(err)
	}
	if err := s.Set([]byte("survivor"), []byte("alive")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(p)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if _, ok := s2.Get([]byte("k")); ok {
		t.Fatal("k should be gone after tombstone recovery")
	}
	if v, ok := s2.Get([]byte("survivor")); !ok || !bytes.Equal(v, []byte("alive")) {
		t.Fatalf("survivor = %q,%v; want alive,true", v, ok)
	}
}

func TestReplayIdempotency(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.wal")

	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	s.Set([]byte("a"), []byte("1"))
	s.Set([]byte("a"), []byte("2"))
	s.Delete([]byte("a"))
	s.Set([]byte("b"), []byte("3"))
	s.Close()

	first, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	snap1 := first.Snapshot()
	first.Close()

	second, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	snap2 := second.Snapshot()
	second.Close()

	if len(snap1) != len(snap2) {
		t.Fatalf("replay not idempotent: %d vs %d keys", len(snap1), len(snap2))
	}
	for k, v := range snap1 {
		v2, ok := snap2[k]
		if !ok || !bytes.Equal(v, v2) {
			t.Fatalf("replay mismatch on %q: %q vs %q", k, v, v2)
		}
	}
}

func TestTailTruncationTolerance(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.wal")

	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	s.Set([]byte("a"), []byte("1"))
	s.Set([]byte("b"), []byte("2"))
	s.Set([]byte("c"), []byte("3"))
	s.Close()

	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	origSize := fi.Size()
	// Chop off enough bytes to break the 3rd record.
	if err := os.Truncate(p, origSize-5); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(p)
	if err != nil {
		t.Fatalf("open truncated WAL: %v", err)
	}
	defer s2.Close()
	if v, ok := s2.Get([]byte("a")); !ok || !bytes.Equal(v, []byte("1")) {
		t.Errorf("a = %q,%v; want 1,true", v, ok)
	}
	if v, ok := s2.Get([]byte("b")); !ok || !bytes.Equal(v, []byte("2")) {
		t.Errorf("b = %q,%v; want 2,true", v, ok)
	}
	if _, ok := s2.Get([]byte("c")); ok {
		t.Errorf("c should be gone after truncation")
	}
	fi2, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi2.Size() >= origSize {
		t.Errorf("truncated tail not trimmed: %d >= %d", fi2.Size(), origSize)
	}
	// Append after recovery must still be recoverable.
	if err := s2.Set([]byte("c"), []byte("3-recovered")); err != nil {
		t.Fatal(err)
	}
	s2.Close()

	s3, err := Open(p)
	if err != nil {
		t.Fatalf("reopen after truncation append: %v", err)
	}
	defer s3.Close()
	if v, ok := s3.Get([]byte("c")); !ok || !bytes.Equal(v, []byte("3-recovered")) {
		t.Errorf("post-truncation append lost: %q,%v", v, ok)
	}
}

func TestCompactionEquivalence(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "d.wal")

	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	// Bloat the WAL with repeated writes of the same key.
	for i := 0; i < 20; i++ {
		if err := s.Set([]byte("hot"), []byte("vvvvvvvvvvvvvvvvvvvvvvvvvvvvvv")); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Set([]byte("cold"), []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete([]byte("cold")); err != nil {
		t.Fatal(err)
	}
	want := s.Snapshot()
	before, err := s.WALSize()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Compact(); err != nil {
		t.Fatal(err)
	}
	after, err := s.WALSize()
	if err != nil {
		t.Fatal(err)
	}
	if after >= before {
		t.Errorf("compaction did not shrink WAL: %d -> %d", before, after)
	}
	got := s.Snapshot()
	if len(got) != len(want) {
		t.Fatalf("compaction changed key count: want %d got %d", len(want), len(got))
	}
	for k, v := range want {
		g, ok := got[k]
		if !ok || !bytes.Equal(g, v) {
			t.Fatalf("compaction changed %q: want %q got %q", k, v, g)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen after compaction: state must match.
	s2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got2 := s2.Snapshot()
	if len(got2) != len(want) {
		t.Fatalf("post-compact recovery changed count: want %d got %d", len(want), len(got2))
	}
	for k, v := range want {
		g, ok := got2[k]
		if !ok || !bytes.Equal(g, v) {
			t.Fatalf("post-compact recovery mismatch on %q: want %q got %q", k, v, g)
		}
	}
}

func openTemp(t *testing.T) (*Store, func()) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "d.wal"))
	if err != nil {
		t.Fatal(err)
	}
	return s, func() { _ = s.Close() }
}
