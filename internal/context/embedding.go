package context

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Embedder produces equal-sized vectors without using a paid provider.
type Embedder interface {
	Dimensions() int
	Embed(context.Context, []string) ([][]float32, error)
}

// HTTPEmbedder connects to Hugging Face Text Embeddings Inference.
type HTTPEmbedder struct {
	BaseURL     string
	DimensionsN int
	Client      *http.Client
	APIKey      string
}

func (e HTTPEmbedder) Dimensions() int { return e.DimensionsN }

func (e HTTPEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return e.embed(ctx, texts)
}

// EmbedDocuments and EmbedQuery apply the prefixes required by E5-family
// models. Service uses these optional methods when the embedder provides them.
func (e HTTPEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	return e.embed(ctx, prefixed("passage: ", texts))
}

func (e HTTPEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	vectors, err := e.embed(ctx, []string{"query: " + query})
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

func prefixed(prefix string, texts []string) []string {
	result := make([]string, len(texts))
	for i, text := range texts {
		result[i] = prefix + text
	}
	return result
}

func (e HTTPEmbedder) embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	base, err := url.Parse(e.BaseURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, errors.New("invalid embedding service URL")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/embed"
	body, err := json.Marshal(map[string]any{"inputs": texts, "truncate": true})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.APIKey)
	}
	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("embedding service returned %s", resp.Status)
	}
	var vectors [][]float32
	if err := json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&vectors); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(vectors) != len(texts) {
		return nil, errors.New("embedding service returned an unexpected vector count")
	}
	for _, vector := range vectors {
		if e.DimensionsN > 0 && len(vector) != e.DimensionsN {
			return nil, errors.New("embedding service returned an unexpected dimension")
		}
	}
	return vectors, nil
}

// DeterministicEmbedder is a local hashing embedder useful for deterministic
// tests and degraded read/write operation. Production setup uses HTTPEmbedder.
type DeterministicEmbedder struct{ DimensionsN int }

func (e DeterministicEmbedder) Dimensions() int {
	if e.DimensionsN <= 0 {
		return 64
	}
	return e.DimensionsN
}

func (e DeterministicEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	dim := e.Dimensions()
	result := make([][]float32, len(texts))
	for i, value := range texts {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		vector := make([]float32, dim)
		for n := 0; n < dim; n++ {
			h := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s", n, value)))
			u := binary.LittleEndian.Uint32(h[:4])
			vector[n] = float32(float64(u)/float64(math.MaxUint32)*2 - 1)
		}
		normalize(vector)
		result[i] = vector
	}
	return result, nil
}

func normalize(vector []float32) {
	var sum float64
	for _, v := range vector {
		sum += float64(v * v)
	}
	if sum == 0 {
		return
	}
	scale := float32(1 / math.Sqrt(sum))
	for i := range vector {
		vector[i] *= scale
	}
}
