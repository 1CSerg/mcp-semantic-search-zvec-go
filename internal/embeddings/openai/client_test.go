package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

func TestNewClientMissingBaseURL(t *testing.T) {
	_, err := NewClient(config.EmbeddingProfile{Model: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDimensions(t *testing.T) {
	c := &Client{profile: config.EmbeddingProfile{Dimensions: 384}}
	if c.Dimensions() != 384 {
		t.Fatalf("dimensions=%d", c.Dimensions())
	}
}

func TestEmbedEmpty(t *testing.T) {
	c, err := NewClient(config.EmbeddingProfile{
		Model:   "test",
		BaseURL: "http://127.0.0.1:9/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	vecs, err := c.Embed(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if vecs != nil {
		t.Fatalf("vecs=%v", vecs)
	}
}

func TestEmbedBatch(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]any{
				"index":     i,
				"embedding": []float64{float64(i) + 0.1, float64(i) + 0.2},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()

	c, err := NewClient(config.EmbeddingProfile{
		Model:      "test",
		BaseURL:    srv.URL + "/v1",
		Dimensions: 2,
		BatchSize:  2,
	})
	if err != nil {
		t.Fatal(err)
	}
	vecs, err := c.Embed(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 3 || calls != 2 {
		t.Fatalf("len=%d calls=%d", len(vecs), calls)
	}
}

func TestEmbedHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	c, err := NewClient(config.EmbeddingProfile{
		Model:   "test",
		BaseURL: srv.URL + "/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.EmbedQuery(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEmbedQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 0, "embedding": []float64{0.1, 0.2, 0.3}},
			},
		})
	}))
	defer srv.Close()

	c, err := NewClient(config.EmbeddingProfile{
		Model:      "test",
		BaseURL:    srv.URL + "/v1",
		Dimensions: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	vec, err := c.EmbedQuery(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 3 {
		t.Fatalf("len=%d", len(vec))
	}
}
