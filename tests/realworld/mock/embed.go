//go:build ignore

// mock-embed serves a minimal OpenAI-compatible /v1/embeddings endpoint for realworld E1/E2/E4.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

func main() {
	port := flag.Int("port", 9999, "listen port")
	dims := flag.Int("dims", 128, "embedding dimensions")
	fail := flag.Bool("fail", false, "return 503 on /v1/embeddings")
	failCount := flag.Int("fail-count", 0, "return 503 for the first N embedding requests, then succeed")
	delay := flag.Duration("delay", 0, "delay before responding to embeddings")
	flag.Parse()

	var failsRemaining int32
	if *failCount > 0 {
		atomic.StoreInt32(&failsRemaining, int32(*failCount))
	}

	http.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	http.HandleFunc("/v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if *fail {
			http.Error(w, "embedding service unavailable", http.StatusServiceUnavailable)
			return
		}
		if n := atomic.LoadInt32(&failsRemaining); n > 0 {
			atomic.AddInt32(&failsRemaining, -1)
			http.Error(w, "embedding service temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		if *delay > 0 {
			time.Sleep(*delay)
		}
		var req struct {
			Input json.RawMessage `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		count := embeddingInputCount(req.Input)
		if count < 1 {
			count = 1
		}
		data := make([]map[string]any, count)
		for i := 0; i < count; i++ {
			vec := make([]float64, *dims)
			vec[0] = 1
			data[i] = map[string]any{"object": "embedding", "index": i, "embedding": vec}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data":   data,
		})
	})

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	log.Printf("mock embeddings listening on %s (dims=%d fail=%v fail-count=%d)", addr, *dims, *fail, *failCount)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func embeddingInputCount(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 1
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return 1
		}
		if len(items) == 0 {
			return 1
		}
		return len(items)
	}
	return 1
}
