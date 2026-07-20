//go:build onnx

package onnx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/1CSerg/mcp-semantic-search-zvec-go/internal/config"
	"github.com/sugarme/tokenizer"
	"github.com/sugarme/tokenizer/pretrained"
	ort "github.com/yalue/onnxruntime_go"
)

var (
	ortOnce sync.Once
	ortErr  error
)

// Client runs local ONNX embedding inference.
type Client struct {
	profile    config.EmbeddingProfile
	paths      BundlePaths
	tokenizer  *tokenizer.Tokenizer
	session    *ort.DynamicAdvancedSession
	inputNames []string
	hiddenSize int
	closed     bool
	mu         sync.Mutex
}

// NewClient loads tokenizer and ONNX session for the configured profile.
func NewClient(profile config.EmbeddingProfile, workspaceRoot string) (*Client, error) {
	paths, err := ResolveBundle(profile, workspaceRoot)
	if err != nil {
		return nil, err
	}
	if profile.Dimensions <= 0 {
		return nil, fmt.Errorf("dimensions must be > 0 for onnx profile")
	}

	if err := initRuntime(); err != nil {
		return nil, err
	}

	tok, err := pretrained.FromFile(paths.Tokenizer)
	if err != nil {
		return nil, fmt.Errorf("load tokenizer: %w", err)
	}
	tok.WithTruncation(&tokenizer.TruncationParams{
		MaxLength: 256,
		Strategy:  tokenizer.LongestFirst,
	})

	inputs, outputs, err := ort.GetInputOutputInfo(paths.ModelFile)
	if err != nil {
		return nil, fmt.Errorf("inspect onnx model: %w", err)
	}
	inputNames := make([]string, len(inputs))
	for i, in := range inputs {
		inputNames[i] = in.Name
	}
	outputNames := make([]string, len(outputs))
	for i, out := range outputs {
		outputNames[i] = out.Name
	}

	session, err := ort.NewDynamicAdvancedSession(paths.ModelFile, inputNames, outputNames, nil)
	if err != nil {
		return nil, fmt.Errorf("create onnx session: %w", err)
	}

	hiddenSize := profile.Dimensions
	if len(outputs) > 0 && len(outputs[0].Dimensions) >= 3 {
		if d := outputs[0].Dimensions[2]; d > 0 {
			hiddenSize = int(d)
		}
	}

	return &Client{
		profile:    profile,
		paths:      paths,
		tokenizer:  tok,
		session:    session,
		inputNames: inputNames,
		hiddenSize: hiddenSize,
	}, nil
}

func initRuntime() error {
	ortOnce.Do(func() {
		if lib := resolveRuntimeLibrary(); lib != "" {
			ort.SetSharedLibraryPath(lib)
		}
		ortErr = ort.InitializeEnvironment()
	})
	return ortErr
}

func resolveRuntimeLibrary() string {
	if v := os.Getenv("ONNXRUNTIME_SHARED_LIBRARY_PATH"); v != "" {
		return v
	}
	if v := os.Getenv("ORT_LIB_DIR"); v != "" {
		return runtimeLibraryInDir(v)
	}
	candidates := []string{
		filepath.Join(".deps", "onnxruntime", runtimeLibraryName()),
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), runtimeLibraryName()))
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			abs, err := filepath.Abs(c)
			if err == nil {
				return abs
			}
			return c
		}
	}
	return ""
}

func runtimeLibraryInDir(dir string) string {
	path := filepath.Join(dir, runtimeLibraryName())
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		return path
	}
	return dir
}

func runtimeLibraryName() string {
	switch runtime.GOOS {
	case "windows":
		return "onnxruntime.dll"
	case "darwin":
		return "libonnxruntime.dylib"
	default:
		return "libonnxruntime.so"
	}
}

// Dimensions returns configured vector size.
func (c *Client) Dimensions() int {
	return c.profile.Dimensions
}

// Embed encodes texts into normalized vectors.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.session == nil {
		return nil, fmt.Errorf("onnx client is closed")
	}
	batchSize := c.profile.BatchSize
	if batchSize <= 0 {
		batchSize = 32
	}
	out := make([][]float32, 0, len(texts))
	for i := 0; i < len(texts); i += batchSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := c.embedBatch(texts[i:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
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

// HealthCheck verifies bundle files and session readiness.
func (c *Client) HealthCheck(_ context.Context) error {
	if err := ValidateBundle(c.paths); err != nil {
		return err
	}
	if c.session == nil {
		return fmt.Errorf("onnx session not initialized")
	}
	_, err := c.Embed(context.Background(), []string{"health check"})
	return err
}

// Close releases native ONNX resources. It acquires the inference mutex so it
// cannot race with a concurrent Embed (which would otherwise destroy the
// session mid-Run — a use-after-free in native onnxruntime). After Close
// returns, Embed reports an error instead of panicking on a nil session.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.session != nil {
		c.session.Destroy()
		c.session = nil
	}
	return nil
}

func (c *Client) embedBatch(texts []string) ([][]float32, error) {
	maxLen := 0
	encoded := make([]*tokenizer.Encoding, len(texts))
	for i, text := range texts {
		enc, err := c.tokenizer.EncodeSingle(text, true)
		if err != nil {
			return nil, fmt.Errorf("tokenize: %w", err)
		}
		encoded[i] = enc
		if l := len(enc.Ids); l > maxLen {
			maxLen = l
		}
	}
	if maxLen == 0 {
		maxLen = 1
	}

	batch := len(texts)
	inputIDs := make([]int64, batch*maxLen)
	attentionMask := make([]int64, batch*maxLen)
	tokenTypeIDs := make([]int64, batch*maxLen)
	for i, enc := range encoded {
		row := i * maxLen
		for j, id := range enc.Ids {
			inputIDs[row+j] = int64(id)
			attentionMask[row+j] = 1
		}
	}

	inputTensors := make([]ort.Value, 0, len(c.inputNames))
	defer func() {
		for _, t := range inputTensors {
			t.Destroy()
		}
	}()

	for _, name := range c.inputNames {
		lower := strings.ToLower(name)
		switch {
		case strings.Contains(lower, "input_ids"):
			t, err := ort.NewTensor(ort.NewShape(int64(batch), int64(maxLen)), inputIDs)
			if err != nil {
				return nil, err
			}
			inputTensors = append(inputTensors, t)
		case strings.Contains(lower, "attention"):
			t, err := ort.NewTensor(ort.NewShape(int64(batch), int64(maxLen)), attentionMask)
			if err != nil {
				return nil, err
			}
			inputTensors = append(inputTensors, t)
		case strings.Contains(lower, "token_type"):
			t, err := ort.NewTensor(ort.NewShape(int64(batch), int64(maxLen)), tokenTypeIDs)
			if err != nil {
				return nil, err
			}
			inputTensors = append(inputTensors, t)
		default:
			return nil, fmt.Errorf("unsupported onnx input %q", name)
		}
	}

	outputShape := ort.NewShape(int64(batch), int64(maxLen), int64(c.hiddenSize))
	outputTensor, err := ort.NewEmptyTensor[float32](outputShape)
	if err != nil {
		return nil, err
	}
	defer outputTensor.Destroy()

	if err := c.session.Run(inputTensors, []ort.Value{outputTensor}); err != nil {
		return nil, fmt.Errorf("onnx inference: %w", err)
	}

	raw := outputTensor.GetData()
	vectors := make([][]float32, batch)
	for i := 0; i < batch; i++ {
		vec := meanPool(raw, i, maxLen, c.hiddenSize, attentionMask[i*maxLen:(i+1)*maxLen])
		vec = l2Normalize(vec)
		if c.profile.Dimensions > 0 && len(vec) != c.profile.Dimensions {
			return nil, fmt.Errorf("dimension mismatch: got %d want %d", len(vec), c.profile.Dimensions)
		}
		vectors[i] = vec
	}
	return vectors, nil
}
