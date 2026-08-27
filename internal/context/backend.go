package context

import (
	"context"

	"github.com/ivo-lopes/ivoai/internal/core"
)

// LegacyQdrantContextBackend is the production adapter for the current
// catalog + local embeddings + Qdrant implementation. The adapter name is
// runtime metadata and is never persisted in the v0.5 config/state schemas.
type LegacyQdrantContextBackend struct {
	Service *Service
	Version string
	Managed bool
}

func (b *LegacyQdrantContextBackend) ID() core.ComponentID { return core.ComponentContext }

func (b *LegacyQdrantContextBackend) Probe(ctx context.Context) core.ComponentStatus {
	status := core.ComponentStatus{
		ID: core.ComponentContext, Implementation: "legacy-qdrant", Active: true,
		Installed: b != nil && b.Service != nil, Managed: b != nil && b.Managed,
		Health: core.HealthUnavailable, Lifecycle: core.LifecycleStopped,
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
	if b == nil || b.Service == nil {
		status.Compatibility = core.Compatibility{State: core.CompatibilityUnknown, Reason: "context service is not constructed"}
		return status
	}
	status.Provenance.Version = b.Version
	current := b.Service.Status(ctx)
	status.Available = current.Healthy
	status.Lifecycle = core.LifecycleRunning
	if current.Healthy {
		status.Health = core.HealthHealthy
	} else {
		status.Health = core.HealthDegraded
	}
	return status
}

func (b *LegacyQdrantContextBackend) Initialize(ctx context.Context) error {
	return b.Service.Initialize(ctx)
}
func (b *LegacyQdrantContextBackend) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	return b.Service.Search(ctx, query, limit)
}
func (b *LegacyQdrantContextBackend) GetDocument(id string) (Document, bool, error) {
	return b.Service.GetDocument(id)
}
func (b *LegacyQdrantContextBackend) Recent(limit int) ([]Document, error) {
	return b.Service.Recent(limit)
}
func (b *LegacyQdrantContextBackend) Status(ctx context.Context) Status {
	return b.Service.Status(ctx)
}
func (b *LegacyQdrantContextBackend) AddConnector(connector Connector) error {
	return b.Service.AddConnector(connector)
}
func (b *LegacyQdrantContextBackend) RemoveConnector(name string) bool {
	return b.Service.RemoveConnector(name)
}
func (b *LegacyQdrantContextBackend) ConnectorNames() []string {
	return b.Service.ConnectorNames()
}
func (b *LegacyQdrantContextBackend) Ingest(ctx context.Context, connector string) (int, error) {
	return b.Service.Ingest(ctx, connector)
}
func (b *LegacyQdrantContextBackend) PurgeSource(ctx context.Context, source string) error {
	return b.Service.PurgeSource(ctx, source)
}

var _ core.ContextBackend = (*LegacyQdrantContextBackend)(nil)
