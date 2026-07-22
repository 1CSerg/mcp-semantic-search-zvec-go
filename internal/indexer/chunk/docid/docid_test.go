package docid

import "testing"

func TestMakeDistinctForSameLineChunks(t *testing.T) {
	base := Params{
		RelativePath:  "server.go",
		StartLine:     202,
		EndLine:       208,
		ChunkStrategy: "ast",
		ChunkType:     "code",
		Snippet:       "func handleSearch() {}",
	}
	a := Make(base)
	base.ChunkIndex = 1
	b := Make(base)
	base.Snippet = "func writeError() {}"
	base.ChunkIndex = 2
	c := Make(base)
	if a == b || a == c || b == c {
		t.Fatalf("expected distinct ids: %q %q %q", a, b, c)
	}
}

func TestAssertUniqueDetectsDuplicate(t *testing.T) {
	if err := AssertUnique([]string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if err := AssertUnique([]string{"a", "a"}); err == nil {
		t.Fatal("expected duplicate error")
	}
}
