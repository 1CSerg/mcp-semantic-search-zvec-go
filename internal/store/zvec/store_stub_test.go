//go:build !zvec

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
