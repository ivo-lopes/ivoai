package codeintel

import (
	"context"
	"encoding/json"
	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/toolrouting"
	"path/filepath"
	"time"
)

func (p *Provider) costPath() string {
	if p.CacheRoot == "" {
		return ""
	}
	key, err := Identity(p.Root, p.Scope, p.Permissions)
	if err != nil {
		return ""
	}
	return filepath.Join(p.CacheRoot, key, "costs.json")
}
func (p *Provider) costHistory() toolrouting.History {
	var h toolrouting.History
	path := p.costPath()
	if path == "" {
		return h
	}
	body, err := readBounded(path, 16384)
	if err == nil {
		_ = json.Unmarshal(body, &h)
	}
	if len(h.Entries) > 18 {
		return toolrouting.History{}
	}
	return h
}
func (p *Provider) recordCost(q core.CodeQuery, r core.CodeResult, err error) {
	// Cache hits and failures before an actual provider attempt do not predict
	// execution cost. Attempted failures retain only aggregate measurements.
	if r.CacheHit || err != nil && r.ProviderVersion == "" {
		return
	}
	path := p.costPath()
	if path == "" {
		return
	}
	if platform.EnsurePrivateDir(filepath.Dir(path)) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	unlock, e := lock(ctx, path+".lock")
	if e != nil {
		return
	}
	defer unlock()
	h := p.costHistory()
	tool := "graft"
	if r.ProviderVersion == "direct" {
		tool = "direct"
	}
	h.Observe(q.Operation, tool, toolrouting.Sample{LatencyMS: r.DurationMS, ResultBytes: len(r.Text), CPU: r.CPUMS, Memory: r.PeakMemoryBytes, Success: err == nil})
	body, e := json.Marshal(h)
	if e == nil && len(body) <= 16384 {
		_ = platform.AtomicWritePrivate(body, path)
	}
}
