//go:build ignore

// mock-embed serves a minimal OpenAI-compatible /v1/embeddings endpoint for realworld E1/E2.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	port := flag.Int("port", 9999, "listen port")
	dims := flag.Int("dims", 128, "embedding dimensions")
	fail := flag.Bool("fail", false, "return 503 on /v1/embeddings")
	delay := flag.Duration("delay", 0, "delay before responding to embeddings")
	flag.Parse()

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
		if *delay > 0 {
			time.Sleep(*delay)
		}
		vec := make([]float64, *dims)
		vec[0] = 1
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"object": "embedding", "index": 0, "embedding": vec},
			},
		})
	})

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	log.Printf("mock embeddings listening on %s (dims=%d fail=%v)", addr, *dims, *fail)
	log.Fatal(http.ListenAndServe(addr, nil))
}
