//go:build integration && windows

package zvec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIntegrationCyrillicCollectionPath(t *testing.T) {
	parent := filepath.Join(os.TempDir(), "zvec-тест-индекс")
	if err := os.RemoveAll(parent); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(parent) })

	schema := createTestSchema()
	defer schema.Destroy()

	collectionPath := filepath.Join(parent, "collection")
	collection, err := CreateAndOpen(collectionPath, schema, nil)
	if err != nil {
		t.Fatalf("CreateAndOpen: %v", err)
	}
	defer func() { _ = collection.Close() }()

	doc := createTestDoc("doc1", "readme snippet", []float32{0.1, 0.2, 0.3, 0.4})
	defer doc.Destroy()

	result, err := collection.Insert([]*Doc{doc})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if result.ErrorCount != 0 {
		t.Fatalf("Insert errors: %d", result.ErrorCount)
	}
	if err := collection.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := collection.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(collectionPath, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer reopened.Close()

	q := NewSearchQuery()
	defer q.Destroy()
	if err := q.SetFieldName("embedding"); err != nil {
		t.Fatal(err)
	}
	if err := q.SetTopK(1); err != nil {
		t.Fatal(err)
	}
	if err := q.SetOutputFields([]string{"text", "id"}); err != nil {
		t.Fatal(err)
	}
	if err := q.SetQueryVector([]float32{0.1, 0.2, 0.3, 0.4}); err != nil {
		t.Fatal(err)
	}
	results, err := reopened.Query(q)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	defer FreeDocs(results)
	if len(results) == 0 {
		t.Fatal("expected search hits")
	}
	text, err := results[0].GetStringField("text")
	if err != nil || text == "" {
		t.Fatalf("text field empty: %q err=%v", text, err)
	}
}
