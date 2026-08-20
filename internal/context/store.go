package context

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// VectorStore contains a rebuildable chunk index.
type VectorStore interface {
	Ensure(context.Context, int) error
	ReplaceDocument(context.Context, string, []Chunk) error
	DeleteDocuments(context.Context, []string) error
	Search(context.Context, []float32, int) ([]SearchResult, error)
	Count(context.Context) (int, error)
}

// MemoryStore is a complete in-process vector store for tests and small setups.
type MemoryStore struct {
	mu     sync.RWMutex
	dim    int
	chunks map[string]Chunk
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{chunks: make(map[string]Chunk)} }

func (s *MemoryStore) Ensure(_ context.Context, dimensions int) error {
	if dimensions <= 0 {
		return errors.New("vector dimension must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dim != 0 && s.dim != dimensions {
		return errors.New("vector store dimension mismatch")
	}
	s.dim = dimensions
	return nil
}

func (s *MemoryStore) ReplaceDocument(_ context.Context, documentID string, chunks []Chunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, chunk := range s.chunks {
		if chunk.DocumentID == documentID {
			delete(s.chunks, id)
		}
	}
	for _, chunk := range chunks {
		if len(chunk.Vector) != s.dim {
			return errors.New("chunk vector dimension mismatch")
		}
		s.chunks[chunk.ID] = cloneChunk(chunk)
	}
	return nil
}

func (s *MemoryStore) DeleteDocuments(_ context.Context, documentIDs []string) error {
	wanted := make(map[string]bool, len(documentIDs))
	for _, id := range documentIDs {
		wanted[id] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, chunk := range s.chunks {
		if wanted[chunk.DocumentID] {
			delete(s.chunks, id)
		}
	}
	return nil
}

func (s *MemoryStore) Search(_ context.Context, query []float32, limit int) ([]SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(query) != s.dim {
		return nil, errors.New("query vector dimension mismatch")
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	results := make([]SearchResult, 0, len(s.chunks))
	for _, chunk := range s.chunks {
		results = append(results, SearchResult{Chunk: cloneChunk(chunk), Score: cosine(query, chunk.Vector)})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *MemoryStore) Count(_ context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.chunks), nil
}

func cloneChunk(c Chunk) Chunk {
	c.Vector = append([]float32(nil), c.Vector...)
	c.Metadata = cloneMap(c.Metadata)
	return c
}

func cloneMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}

func cosine(a, b []float32) float32 {
	var dot, aa, bb float64
	for i := range a {
		dot += float64(a[i] * b[i])
		aa += float64(a[i] * a[i])
		bb += float64(b[i] * b[i])
	}
	if aa == 0 || bb == 0 {
		return 0
	}
	return float32(dot / math.Sqrt(aa*bb))
}

// QdrantStore talks only to the private Qdrant HTTP endpoint.
type QdrantStore struct {
	BaseURL    string
	Collection string
	Client     *http.Client
	APIKey     string
}

func (q QdrantStore) client() *http.Client {
	if q.Client != nil {
		return q.Client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (q QdrantStore) endpoint(path string) (string, error) {
	base, err := url.Parse(q.BaseURL)
	if err != nil || base.Scheme != "http" || base.Host == "" {
		return "", errors.New("invalid private Qdrant URL")
	}
	collection := q.Collection
	if collection == "" {
		collection = "ivoai_context_v1"
	}
	for _, r := range collection {
		if !(r == '_' || r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return "", errors.New("invalid Qdrant collection name")
		}
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/collections/" + collection + path
	return base.String(), nil
}

func (q QdrantStore) do(ctx context.Context, method, path string, body any, output any, accepted ...int) error {
	endpoint, err := q.endpoint(path)
	if err != nil {
		return err
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if q.APIKey != "" {
		req.Header.Set("api-key", q.APIKey)
	}
	resp, err := q.client().Do(req)
	if err != nil {
		return fmt.Errorf("Qdrant request: %w", err)
	}
	defer resp.Body.Close()
	ok := false
	for _, status := range accepted {
		ok = ok || resp.StatusCode == status
	}
	if !ok {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("Qdrant returned %s", resp.Status)
	}
	if output != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(output)
	}
	return nil
}

func (q QdrantStore) Ensure(ctx context.Context, dimensions int) error {
	if dimensions <= 0 {
		return errors.New("vector dimension must be positive")
	}
	existing, found, err := q.collectionDimensions(ctx)
	if err != nil {
		return err
	}
	if found {
		if existing != dimensions {
			return fmt.Errorf("Qdrant collection dimension mismatch: got %d, want %d", existing, dimensions)
		}
		return nil
	}
	err = q.do(ctx, http.MethodPut, "", map[string]any{
		"vectors": map[string]any{"size": dimensions, "distance": "Cosine"},
	}, nil, http.StatusOK, http.StatusConflict)
	if err != nil {
		return err
	}
	// A concurrent initializer may have won the create race. Read the final
	// collection configuration and refuse a conflicting dimension.
	existing, found, err = q.collectionDimensions(ctx)
	if err != nil {
		return err
	}
	if found && existing != dimensions {
		return fmt.Errorf("Qdrant collection dimension mismatch: got %d, want %d", existing, dimensions)
	}
	return nil
}

func (q QdrantStore) collectionDimensions(ctx context.Context) (int, bool, error) {
	endpoint, err := q.endpoint("")
	if err != nil {
		return 0, false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, false, err
	}
	if q.APIKey != "" {
		req.Header.Set("api-key", q.APIKey)
	}
	resp, err := q.client().Do(req)
	if err != nil {
		return 0, false, fmt.Errorf("Qdrant request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return 0, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return 0, false, fmt.Errorf("Qdrant returned %s", resp.Status)
	}
	var result struct {
		Result struct {
			Config struct {
				Params struct {
					Vectors struct {
						Size int `json:"size"`
					} `json:"vectors"`
				} `json:"params"`
			} `json:"config"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return 0, false, fmt.Errorf("decode Qdrant collection: %w", err)
	}
	if result.Result.Config.Params.Vectors.Size <= 0 {
		return 0, false, errors.New("Qdrant collection has unsupported vector configuration")
	}
	return result.Result.Config.Params.Vectors.Size, true, nil
}

func (q QdrantStore) ReplaceDocument(ctx context.Context, documentID string, chunks []Chunk) error {
	filter := map[string]any{"filter": map[string]any{"must": []any{map[string]any{"key": "document_id", "match": map[string]string{"value": documentID}}}}}
	if err := q.do(ctx, http.MethodPost, "/points/delete?wait=true", filter, nil, http.StatusOK); err != nil {
		return err
	}
	if len(chunks) == 0 {
		return nil
	}
	points := make([]map[string]any, 0, len(chunks))
	for _, chunk := range chunks {
		points = append(points, map[string]any{"id": chunk.ID, "vector": chunk.Vector, "payload": chunk})
	}
	return q.do(ctx, http.MethodPut, "/points?wait=true", map[string]any{"points": points}, nil, http.StatusOK)
}

func (q QdrantStore) DeleteDocuments(ctx context.Context, documentIDs []string) error {
	if len(documentIDs) == 0 {
		return nil
	}
	filter := map[string]any{"filter": map[string]any{"must": []any{map[string]any{
		"key": "document_id", "match": map[string]any{"any": documentIDs},
	}}}}
	return q.do(ctx, http.MethodPost, "/points/delete?wait=true", filter, nil, http.StatusOK)
}

func (q QdrantStore) Search(ctx context.Context, vector []float32, limit int) ([]SearchResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	var response struct {
		Result []struct {
			Score   float32         `json:"score"`
			Payload json.RawMessage `json:"payload"`
		} `json:"result"`
	}
	err := q.do(ctx, http.MethodPost, "/points/search", map[string]any{
		"vector": vector, "limit": limit, "with_payload": true, "with_vector": false,
	}, &response, http.StatusOK)
	if err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(response.Result))
	for _, point := range response.Result {
		var chunk Chunk
		if err := json.Unmarshal(point.Payload, &chunk); err != nil {
			return nil, fmt.Errorf("decode Qdrant payload: %w", err)
		}
		results = append(results, SearchResult{Chunk: chunk, Score: point.Score})
	}
	return results, nil
}

func (q QdrantStore) Count(ctx context.Context) (int, error) {
	var response struct {
		Result struct {
			Count int `json:"count"`
		} `json:"result"`
	}
	err := q.do(ctx, http.MethodPost, "/points/count", map[string]bool{"exact": true}, &response, http.StatusOK)
	return response.Result.Count, err
}
