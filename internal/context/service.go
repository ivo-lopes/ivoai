package context

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
)

// Service coordinates connectors, catalog, embeddings, and the rebuildable index.
type Service struct {
	Embedder Embedder
	Store    VectorStore
	Catalog  Catalog
	Chunker  Chunker

	mu         sync.RWMutex
	connectors map[string]Connector
}

// ID and Probe let in-memory/test compositions satisfy the common backend
// contract. Production uses LegacyQdrantContextBackend, which supplies the
// concrete implementation provenance without changing Service semantics.
func (s *Service) ID() core.ComponentID { return core.ComponentContext }

func (s *Service) Probe(ctx context.Context) core.ComponentStatus {
	current := s.Status(ctx)
	health := core.HealthDegraded
	if current.Healthy {
		health = core.HealthHealthy
	}
	return core.ComponentStatus{
		ID: core.ComponentContext, Implementation: "context-service", Active: true,
		Installed: true, Available: current.Healthy, Health: health, Lifecycle: core.LifecycleRunning,
		Provenance: core.Provenance{Source: "runtime_verified"},
		Capabilities: core.CapabilitySet{
			core.CapabilityContextInitialize: core.SupportSupported,
			core.CapabilityContextSearch:     core.SupportSupported,
			core.CapabilityContextRead:       core.SupportSupported,
			core.CapabilityContextRecent:     core.SupportSupported,
			core.CapabilityContextStatus:     core.SupportSupported,
			core.CapabilityContextIngest:     core.SupportSupported,
		},
		Compatibility: core.Compatibility{State: core.CompatibilityCompatible},
	}
}

func NewService(embedder Embedder, store VectorStore, catalog Catalog) (*Service, error) {
	if embedder == nil || store == nil || catalog == nil {
		return nil, errors.New("context service requires embedder, vector store, and catalog")
	}
	return &Service{Embedder: embedder, Store: store, Catalog: catalog, Chunker: Chunker{}, connectors: make(map[string]Connector)}, nil
}

func (s *Service) Initialize(ctx context.Context) error {
	return s.Store.Ensure(ctx, s.Embedder.Dimensions())
}

func (s *Service) AddConnector(connector Connector) error {
	if connector == nil || connector.Name() == "" {
		return errors.New("connector name is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.connectors[connector.Name()]; exists {
		return fmt.Errorf("connector %q already exists", connector.Name())
	}
	s.connectors[connector.Name()] = connector
	return nil
}

func (s *Service) RemoveConnector(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, found := s.connectors[name]; !found {
		return false
	}
	delete(s.connectors, name)
	return true
}

func (s *Service) ConnectorNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.connectors))
	for name := range s.connectors {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Ingest reads one connector. A document is catalogued only after all of its
// chunks have valid embeddings and the index replacement succeeds.
func (s *Service) Ingest(ctx context.Context, connectorName string) (int, error) {
	s.mu.RLock()
	connector, found := s.connectors[connectorName]
	s.mu.RUnlock()
	if !found {
		return 0, fmt.Errorf("connector %q not found", connectorName)
	}
	documents, err := connector.Documents(ctx)
	if err != nil {
		return 0, err
	}
	previous, err := s.Catalog.BySource(connectorName)
	if err != nil {
		return 0, fmt.Errorf("list existing %s documents: %w", connectorName, err)
	}
	currentIDs := make(map[string]bool, len(documents))
	totalChunks := 0
	for index, document := range documents {
		// Connector implementations are untrusted input adapters; ownership is
		// assigned by the service so reconciliation cannot be evaded.
		document.Source = connectorName
		if document.Metadata == nil {
			document.Metadata = make(map[string]string)
		}
		document.Metadata["connector"] = connectorName
		documents[index] = document
		currentIDs[document.ID] = true
		parts := s.Chunker.Split(document.Content)
		totalChunks += len(parts)
		if totalChunks > 250_000 {
			return 0, errors.New("connector corpus exceeds chunk quota")
		}
		vectors, err := embedDocuments(ctx, s.Embedder, parts)
		if err != nil {
			return 0, fmt.Errorf("embed %s: %w", document.Path, err)
		}
		chunks := make([]Chunk, len(parts))
		now := time.Now().UTC()
		for i, part := range parts {
			chunks[i] = Chunk{ID: stableID(document.ID, fmt.Sprint(i), part), DocumentID: document.ID,
				Text: part, Index: i, Metadata: cloneMap(document.Metadata), Vector: vectors[i], CreatedAt: now}
		}
		if err := s.Store.ReplaceDocument(ctx, document.ID, chunks); err != nil {
			return 0, fmt.Errorf("index %s: %w", document.Path, err)
		}
	}
	stale := make([]string, 0)
	for _, document := range previous {
		if !currentIDs[document.ID] {
			stale = append(stale, document.ID)
		}
	}
	if err := s.Store.DeleteDocuments(ctx, stale); err != nil {
		return 0, fmt.Errorf("reconcile removed %s documents: %w", connectorName, err)
	}
	if err := s.Catalog.ReplaceSource(connectorName, documents); err != nil {
		return 0, fmt.Errorf("catalog connector %s: %w", connectorName, err)
	}
	return len(documents), nil
}

// PurgeSource removes authoritative documents and rebuildable vectors for a
// connector. Vector removal happens first so a partial failure cannot leave
// removed content searchable; catalog deletion is an atomic file replacement.
func (s *Service) PurgeSource(ctx context.Context, source string) error {
	documents, err := s.Catalog.BySource(source)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(documents))
	for _, document := range documents {
		ids = append(ids, document.ID)
	}
	return s.deleteDocuments(ctx, ids)
}

func (s *Service) deleteDocuments(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := s.Store.DeleteDocuments(ctx, ids); err != nil {
		return err
	}
	return s.Catalog.Delete(ids)
}

func (s *Service) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if query == "" {
		return nil, errors.New("search query is required")
	}
	vector, err := embedQuery(ctx, s.Embedder, query)
	if err != nil {
		return nil, err
	}
	return s.Store.Search(ctx, vector, limit)
}

type documentEmbedder interface {
	EmbedDocuments(context.Context, []string) ([][]float32, error)
}

type queryEmbedder interface {
	EmbedQuery(context.Context, string) ([]float32, error)
}

func embedDocuments(ctx context.Context, embedder Embedder, texts []string) ([][]float32, error) {
	if specialized, ok := embedder.(documentEmbedder); ok {
		return specialized.EmbedDocuments(ctx, texts)
	}
	return embedder.Embed(ctx, texts)
}

func embedQuery(ctx context.Context, embedder Embedder, query string) ([]float32, error) {
	if specialized, ok := embedder.(queryEmbedder); ok {
		return specialized.EmbedQuery(ctx, query)
	}
	vectors, err := embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

func (s *Service) GetDocument(id string) (Document, bool, error) { return s.Catalog.Get(id) }
func (s *Service) Recent(limit int) ([]Document, error)          { return s.Catalog.Recent(limit) }

func (s *Service) Status(ctx context.Context) Status {
	documents, docsErr := s.Catalog.Count()
	chunks, chunksErr := s.Store.Count(ctx)
	s.mu.RLock()
	connectors := len(s.connectors)
	s.mu.RUnlock()
	return Status{Healthy: docsErr == nil && chunksErr == nil, Documents: documents, Chunks: chunks, Connectors: connectors}
}
