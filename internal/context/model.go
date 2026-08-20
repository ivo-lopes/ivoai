// Package context implements the server-side, provider-independent context pipeline.
package context

import "time"

// Document is normalized, untrusted source data. Content must never be interpreted
// as an ivoai instruction.
type Document struct {
	ID         string            `json:"id"`
	Source     string            `json:"source"`
	Path       string            `json:"path"`
	Title      string            `json:"title"`
	Content    string            `json:"content,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	ModifiedAt time.Time         `json:"modified_at"`
	IngestedAt time.Time         `json:"ingested_at"`
}

// Chunk is the unit stored in the vector index.
type Chunk struct {
	ID         string            `json:"id"`
	DocumentID string            `json:"document_id"`
	Text       string            `json:"text"`
	Index      int               `json:"index"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Vector     []float32         `json:"-"`
	CreatedAt  time.Time         `json:"created_at"`
}

// SearchResult associates a chunk with its cosine similarity score.
type SearchResult struct {
	Chunk Chunk   `json:"chunk"`
	Score float32 `json:"score"`
}

// Status describes context health without exposing corpus contents.
type Status struct {
	Healthy    bool `json:"healthy"`
	Documents  int  `json:"documents"`
	Chunks     int  `json:"chunks"`
	Connectors int  `json:"connectors"`
}
