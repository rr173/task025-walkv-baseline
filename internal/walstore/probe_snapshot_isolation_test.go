package walstore

import (
	"bytes"
	"testing"
)

// TestProbeSnapshotEmptyStoreReturnsNonNilMap verifies that calling Snapshot on
// an empty store returns a non-nil empty map, allowing callers to safely
// iterate or check length without nil-guard.
func TestProbeSnapshotEmptyStoreReturnsNonNilMap(t *testing.T) {
	s, cleanup := openTemp(t)
	defer cleanup()

	snap := s.Snapshot()
	if snap == nil {
		t.Fatal("Snapshot() returned nil for empty store; want non-nil empty map")
	}
	if len(snap) != 0 {
		t.Fatalf("Snapshot() returned %d entries for empty store; want 0", len(snap))
	}
}

// TestProbeSnapshotValueIsolation verifies that mutating values in the map
// returned by Snapshot does not corrupt the store's internal data.
func TestProbeSnapshotValueIsolation(t *testing.T) {
	s, cleanup := openTemp(t)
	defer cleanup()

	if err := s.Set([]byte("mykey"), []byte("important-data")); err != nil {
		t.Fatal(err)
	}

	snap := s.Snapshot()
	v, ok := snap["mykey"]
	if !ok {
		t.Fatal("mykey missing from snapshot")
	}

	// Corrupt the snapshot's slice.
	for i := range v {
		v[i] = 0
	}

	// The store's own data must remain intact.
	got, ok := s.Get([]byte("mykey"))
	if !ok {
		t.Fatal("mykey disappeared from store")
	}
	if !bytes.Equal(got, []byte("important-data")) {
		t.Fatalf("store data corrupted after snapshot mutation: got %q want %q", got, "important-data")
	}
}
