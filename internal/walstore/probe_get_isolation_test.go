package walstore

import (
	"bytes"
	"testing"
)

// TestProbeGetReturnValueIsolation verifies that mutating the byte slice
// returned by Get does not affect the store's internal state.
func TestProbeGetReturnValueIsolation(t *testing.T) {
	s, cleanup := openTemp(t)
	defer cleanup()

	if err := s.Set([]byte("key"), []byte("hello")); err != nil {
		t.Fatal(err)
	}

	got, ok := s.Get([]byte("key"))
	if !ok {
		t.Fatal("key not found")
	}

	// Mutate the returned slice.
	for i := range got {
		got[i] = 'X'
	}

	// A subsequent Get must still return the original value "hello".
	got2, ok := s.Get([]byte("key"))
	if !ok {
		t.Fatal("key not found after mutation")
	}
	if !bytes.Equal(got2, []byte("hello")) {
		t.Fatalf("Get returned %q after external mutation; want %q", got2, "hello")
	}
}
