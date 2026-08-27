// Package context implements the server-side, provider-independent context pipeline.
package context

import "github.com/ivo-lopes/ivoai/internal/core"

// Document is normalized, untrusted source data. Content must never be interpreted
// as an ivoai instruction.
type Document = core.ContextDocument

// Chunk is the unit stored in the vector index.
type Chunk = core.ContextChunk

// SearchResult associates a chunk with its cosine similarity score.
type SearchResult = core.ContextSearchResult

// Status describes context health without exposing corpus contents.
type Status = core.ContextStatus
