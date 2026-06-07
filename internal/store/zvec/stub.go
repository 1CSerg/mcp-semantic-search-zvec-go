//go:build !zvec

package zvec

// StubStore is a placeholder when built without -tags zvec.
type StubStore struct{}

// New creates a stub store (CGO/zvec not linked).
func New(_ Config) Store {
	return NewStub()
}

// NewStub creates a stub store.
func NewStub() *StubStore { return &StubStore{} }

func (s *StubStore) Open() error                             { return ErrNotLinked }
func (s *StubStore) Close() error                            { return nil }
func (s *StubStore) DocCount() (int, error)                  { return 0, ErrNotLinked }
func (s *StubStore) UpsertChunks([]Chunk, [][]float32) error { return ErrNotLinked }
func (s *StubStore) DeleteByIDs([]string) error              { return ErrNotLinked }
func (s *StubStore) Search([]float32, int, string) ([]SearchHit, error) {
	return nil, ErrNotLinked
}
