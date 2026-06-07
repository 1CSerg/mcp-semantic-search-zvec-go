package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
)

// Client calls OpenAI-compatible /embeddings endpoints.
type Client struct {
	profile    config.EmbeddingProfile
	httpClient *http.Client
	apiKey     string
	url        string
}

// NewClient builds an embedding HTTP client from profile settings.
func NewClient(profile config.EmbeddingProfile) (*Client, error) {
	base := strings.TrimRight(profile.BaseURL, "/")
	if base == "" {
		return nil, fmt.Errorf("base_url is required for openai_compatible profile")
	}
	key := profile.APIKey
	if key == "" && profile.APIKeyEnv != "" {
		key = os.Getenv(profile.APIKeyEnv)
	}
	timeout := time.Duration(profile.TimeoutSeconds * float64(time.Second))
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Client{
		profile: profile,
		apiKey:  key,
		url:     base + "/embeddings",
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

// Dimensions returns configured vector size.
func (c *Client) Dimensions() int {
	return c.profile.Dimensions
}

// Embed encodes texts into vectors.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	batchSize := c.profile.BatchSize
	if batchSize <= 0 {
		batchSize = 32
	}
	var all [][]float32
	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := c.embedBatch(ctx, texts[i:end])
		if err != nil {
			return nil, err
		}
		all = append(all, vecs...)
	}
	return all, nil
}

// EmbedQuery embeds a single query string.
func (c *Client) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	vecs, err := c.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("empty embedding response")
	}
	return vecs[0], nil
}

func (c *Client) embedBatch(ctx context.Context, batch []string) ([][]float32, error) {
	payload := map[string]any{
		"model": c.profile.Model,
		"input": batch,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	for k, v := range c.profile.ExtraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("embeddings HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var parsed struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("parse embeddings response: %w", err)
	}
	if len(parsed.Data) == 0 {
		return nil, fmt.Errorf("embeddings response missing data")
	}
	// Sort by index
	out := make([][]float32, len(batch))
	for _, item := range parsed.Data {
		if item.Index < 0 || item.Index >= len(out) {
			return nil, fmt.Errorf("invalid embedding index %d", item.Index)
		}
		if c.profile.Dimensions > 0 && len(item.Embedding) != c.profile.Dimensions {
			return nil, fmt.Errorf("dimension mismatch: got %d want %d", len(item.Embedding), c.profile.Dimensions)
		}
		vec := make([]float32, len(item.Embedding))
		for i, v := range item.Embedding {
			vec[i] = float32(v)
		}
		out[item.Index] = vec
	}
	for i, vec := range out {
		if vec == nil {
			return nil, fmt.Errorf("missing embedding for input index %d", i)
		}
	}
	return out, nil
}
