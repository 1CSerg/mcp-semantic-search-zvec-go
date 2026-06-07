package zvec

import (
	"errors"
	"testing"
)

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
}

func TestCollectionNameStable(t *testing.T) {
	a := CollectionName("/workspace", "profile", 384)
	b := CollectionName("/workspace", "profile", 384)
	if a != b {
		t.Fatalf("unstable: %q vs %q", a, b)
	}
	if len(a) != 3+16 { // ws_ + 16 hex
		t.Fatalf("name=%q", a)
	}
	c := CollectionName("/other", "profile", 384)
	if a == c {
		t.Fatal("expected different workspace to differ")
	}
}
