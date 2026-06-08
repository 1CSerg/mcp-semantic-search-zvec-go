//go:build ignore

// mock-embed serves a minimal OpenAI-compatible /v1/embeddings endpoint for Phase 1 smoke.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
)

func main() {
	port := flag.Int("port", 9999, "listen port")
	dims := flag.Int("dims", 128, "embedding dimensions")
	flag.Parse()

	http.HandleFunc("/v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
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
	log.Printf("mock embeddings listening on %s (dims=%d)", addr, *dims)
	log.Fatal(http.ListenAndServe(addr, nil))
}
