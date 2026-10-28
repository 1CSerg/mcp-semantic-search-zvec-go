//go:build !zvec

package zvec

import (
	"errors"
	"testing"
)

func TestNewStubFactory(t *testing.T) {
	s := New(Config{IndexDir: tempDir(t)})
	if s == nil {
		t.Fatal("nil store")
	}
	if err := s.WipeCollection(); !errors.Is(err, ErrNotLinked) {
		t.Fatalf("WipeCollection: %v", err)
	}
}

func TestStubStore(t *testing.T) {
	s := NewStub()
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Open(); !errors.Is(err, ErrNotLinked) {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.DocCount(); !errors.Is(err, ErrNotLinked) {
		t.Fatalf("DocCount: %v", err)
	}
	if err := s.UpsertChunks(nil, nil); !errors.Is(err, ErrNotLinked) {
		t.Fatalf("UpsertChunks: %v", err)
	}
	if err := s.DeleteByIDs(nil); !errors.Is(err, ErrNotLinked) {
		t.Fatalf("DeleteByIDs: %v", err)
	}
	if _, err := s.Search(nil, 1, ""); !errors.Is(err, ErrNotLinked) {
		t.Fatalf("Search: %v", err)
	}
	if err := s.Compact(); !errors.Is(err, ErrNotLinked) {
		t.Fatalf("Compact: %v", err)
	}
}

// The optional maintenance capability must be reachable through the stub too, so a
// caller can type-assert on Compactor without a build-tag split.
func TestStubStoreImplementsCompactor(t *testing.T) {
	var store Store = NewStub()
	compactor, ok := store.(Compactor)
	if !ok {
		t.Fatal("StubStore must implement Compactor")
	}
	if err := compactor.Compact(); !errors.Is(err, ErrNotLinked) {
		t.Fatalf("Compact through Compactor: %v", err)
	}
}
