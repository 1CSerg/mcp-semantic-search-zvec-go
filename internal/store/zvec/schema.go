//go:build zvec

package zvec

import (
	"fmt"

	zvec "github.com/zvec-ai/zvec-go"
)

const (
	fieldPath      = "path"
	fieldStartLine = "start_line"
	fieldEndLine   = "end_line"
	fieldChunkType = "chunk_type"
	fieldName      = "name"
	fieldSnippet   = "snippet"
	fieldEmbedding = "embedding"
)

func buildSchema(collectionName string, dimensions int) (*zvec.CollectionSchema, error) {
	if dimensions <= 0 {
		return nil, fmt.Errorf("embedding dimensions must be positive")
	}

	schema := zvec.NewCollectionSchema(collectionName)

	hnsw, err := zvec.NewHNSWIndexParams(zvec.MetricTypeCosine, 16, 200)
	if err != nil {
		schema.Destroy()
		return nil, err
	}
	defer hnsw.Destroy()

	embField := zvec.NewFieldSchema(fieldEmbedding, zvec.DataTypeVectorFP32, false, uint32(dimensions))
	if embField == nil {
		schema.Destroy()
		return nil, fmt.Errorf("create embedding field schema")
	}
	if err := embField.SetIndexParams(hnsw); err != nil {
		embField.Destroy()
		schema.Destroy()
		return nil, err
	}
	if err := schema.AddField(embField); err != nil {
		embField.Destroy()
		schema.Destroy()
		return nil, err
	}
	embField.Destroy()

	scalarFields := []struct {
		name string
		dt   zvec.DataType
	}{
		{fieldPath, zvec.DataTypeString},
		{fieldStartLine, zvec.DataTypeInt64},
		{fieldEndLine, zvec.DataTypeInt64},
		{fieldChunkType, zvec.DataTypeString},
		{fieldName, zvec.DataTypeString},
		{fieldSnippet, zvec.DataTypeString},
	}
	for _, f := range scalarFields {
		field := zvec.NewFieldSchema(f.name, f.dt, false, 0)
		if field == nil {
			schema.Destroy()
			return nil, fmt.Errorf("create field schema %q", f.name)
		}
		if err := schema.AddField(field); err != nil {
			field.Destroy()
			schema.Destroy()
			return nil, err
		}
		field.Destroy()
	}

	return schema, nil
}

func chunkToDoc(chunk Chunk, vector []float32) (*zvec.Doc, error) {
	doc := zvec.NewDoc()
	if doc == nil {
		return nil, fmt.Errorf("create document")
	}
	doc.SetPK(chunk.DocID)
	if err := doc.AddVectorFP32Field(fieldEmbedding, vector); err != nil {
		doc.Destroy()
		return nil, err
	}
	if err := doc.AddStringField(fieldPath, chunk.RelativePath); err != nil {
		doc.Destroy()
		return nil, err
	}
	if err := doc.AddInt64Field(fieldStartLine, chunk.StartLine); err != nil {
		doc.Destroy()
		return nil, err
	}
	if err := doc.AddInt64Field(fieldEndLine, chunk.EndLine); err != nil {
		doc.Destroy()
		return nil, err
	}
	if err := doc.AddStringField(fieldChunkType, chunk.ChunkType); err != nil {
		doc.Destroy()
		return nil, err
	}
	if err := doc.AddStringField(fieldName, chunk.Name); err != nil {
		doc.Destroy()
		return nil, err
	}
	if err := doc.AddStringField(fieldSnippet, chunk.Snippet); err != nil {
		doc.Destroy()
		return nil, err
	}
	return doc, nil
}

func docToSearchHit(doc *zvec.Doc) SearchHit {
	hit := SearchHit{
		DocID: doc.GetPK(),
		Score: float64(doc.GetScore()),
	}
	if v, err := doc.GetStringField(fieldPath); err == nil {
		hit.Path = v
	}
	if v, err := doc.GetInt64Field(fieldStartLine); err == nil {
		hit.StartLine = v
	}
	if v, err := doc.GetInt64Field(fieldEndLine); err == nil {
		hit.EndLine = v
	}
	if v, err := doc.GetStringField(fieldChunkType); err == nil {
		hit.ChunkType = v
	}
	if v, err := doc.GetStringField(fieldName); err == nil {
		hit.Name = v
	}
	if v, err := doc.GetStringField(fieldSnippet); err == nil {
		hit.Snippet = v
	}
	return hit
}
