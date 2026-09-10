package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/orchestration"
	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/quota"
	"github.com/ivo-lopes/ivoai/internal/routing"
	"github.com/ivo-lopes/ivoai/internal/session"
)

// acknowledgePlanQuota also covers primary-only plans. This approval enables
// conservation proposals, not a blanket authorization to change providers.
func (s *Server) acknowledgePlanQuota(ctx context.Context) error {
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	if s.Quota == nil {
		return errors.New("QUOTA_UNAVAILABLE: native admission requires official probes")
	}
	v, err := s.Store.Get(s.SessionID)
	if err != nil || v.QuotaMode == "conservation_active" || v.QuotaMode == "keep_current" {
		return err
	}
	snapshot := map[quota.Provider]quota.ProviderQuota{}
	for name := range s.Registry.Providers {
		q, _ := s.Quota.Probe(ctx, quota.Provider(name), false)
		snapshot[quota.Provider(name)] = q
	}
	proposal := orchestration.QuotaConservation(snapshot, s.LowQuotaThreshold)
	if len(proposal.Providers) == 0 {
		return nil
	}
	id, err := newPlanID()
	if err != nil {
		return err
	}
	id = strings.Replace(id, "plan_", "routing_", 1)
	summary := fmt.Sprintf("Quota at or below %d%%. Enable conservation for new workers while keeping the primary strong? Material route changes still require separate approval. Reject to keep current routing.", proposal.Threshold)
	if err := s.Store.RequestDecisionSummary(s.SessionID, id, "routing", summary); err != nil {
		return err
	}
	_, err = s.Store.Update(s.SessionID, func(v *session.Session) error {
		v.CurrentPhase, v.QuotaMode = "waiting_for_routing_approval", "conservation_pending_confirmation"
		return nil
	})
	if err != nil {
		return err
	}
	decisionErr := s.Store.WaitDecision(ctx, s.SessionID, id)
	if decisionErr != nil && decisionErr.Error() != "PLAN_OR_ROUTING_REJECTED" {
		return decisionErr
	}
	_, err = s.Store.Update(s.SessionID, func(v *session.Session) error {
		v.CurrentPhase, v.QuotaMode = "parallel_dispatch", "conservation_active"
		if decisionErr != nil {
			v.QuotaMode = "keep_current"
		}
		return nil
	})
	return err
}

// refreshNativeRoute runs before starting the official child. A plan's route
// is stable until a fresh probe proves it unavailable or the operator approves
// conservation. A material route change always has its own UI decision.
func (s *Server) refreshNativeRoute(ctx context.Context, task routing.Task) (routing.ExecutionProfile, error) {
	s.routingMu.Lock()
	defer s.routingMu.Unlock()
	if s.Quota == nil {
		return task.Profile, errors.New("QUOTA_UNAVAILABLE: worker admission requires official probes")
	}
	registry := routing.Registry{Providers: map[string]routing.ProviderCapability{}}
	snapshot := map[quota.Provider]quota.ProviderQuota{}
	for name, capability := range s.Registry.Providers {
		q, _ := s.Quota.Probe(ctx, quota.Provider(name), false)
		capability.Authenticated = q.Authenticated
		registry.Providers[name], snapshot[quota.Provider(name)] = capability, q
	}
	value, err := s.Store.Get(s.SessionID)
	if err != nil {
		return task.Profile, err
	}
	proposal := orchestration.QuotaConservation(snapshot, s.LowQuotaThreshold)
	low := len(proposal.Providers) > 0
	router := routing.Router{Strict: true, Registry: registry, Quota: snapshot, Overrides: s.Overrides}
	pinned := task.TaskInput
	pinned.Executor, pinned.Model, pinned.Effort = task.Profile.Provider, task.Profile.Model, task.Profile.Effort
	_, pinnedErr := router.Resolve(pinned, task.Tier)
	alert := low && value.QuotaMode != "conservation_active" && value.QuotaMode != "keep_current"
	if pinnedErr == nil && !alert && !(low && value.QuotaMode == "conservation_active") {
		return task.Profile, nil
	}
	input := task.TaskInput
	if input.PreferredExecutor == "" && s.ProviderPreference != "auto" {
		input.PreferredExecutor = s.ProviderPreference
	}
	proposed, err := router.Resolve(input, task.Tier)
	if err != nil {
		return task.Profile, errors.New("PROVIDER_UNAVAILABLE: no compatible worker route; explicit overrides preserved")
	}
	changed := proposed.Provider != task.Profile.Provider || proposed.Model != task.Profile.Model || proposed.Effort != task.Profile.Effort
	if !changed && !alert {
		return task.Profile, nil
	}
	id, err := newPlanID()
	if err != nil {
		return task.Profile, err
	}
	id = strings.Replace(id, "plan_", "routing_", 1)
	reason := "planned route unavailable"
	if low {
		reason = fmt.Sprintf("quota at or below %d%%; preserve strong primary", proposal.Threshold)
	}
	summary := fmt.Sprintf("Routing approval (%s). Worker %s: %s/%s/%s → %s/%s/%s. Approve, or keep the current route when eligible?", reason, task.ID, task.Profile.Provider, task.Profile.Model, task.Profile.Effort, proposed.Provider, proposed.Model, proposed.Effort)
	summary = platform.Redact(summary)
	if err := s.Store.RequestDecisionSummary(s.SessionID, id, "routing", summary); err != nil {
		return task.Profile, err
	}
	_, err = s.Store.Update(s.SessionID, func(v *session.Session) error {
		v.CurrentPhase = "waiting_for_routing_approval"
		if low {
			v.QuotaMode = "conservation_pending_confirmation"
		}
		return nil
	})
	if err != nil {
		return task.Profile, err
	}
	decisionErr := s.Store.WaitDecision(ctx, s.SessionID, id)
	if ctx.Err() != nil {
		return task.Profile, ctx.Err()
	}
	if decisionErr != nil && decisionErr.Error() != "PLAN_OR_ROUTING_REJECTED" {
		return task.Profile, decisionErr
	}
	_, err = s.Store.Update(s.SessionID, func(v *session.Session) error {
		v.CurrentPhase = "running"
		if low {
			v.QuotaMode = "conservation_active"
			if decisionErr != nil {
				v.QuotaMode = "keep_current"
			}
		}
		if decisionErr != nil && pinnedErr != nil {
			v.QuotaMode, v.CurrentPhase = "degraded", "degraded"
		}
		return nil
	})
	if err != nil {
		return task.Profile, err
	}
	if decisionErr != nil {
		if pinnedErr != nil {
			return task.Profile, errors.New("PROVIDER_UNAVAILABLE: routing change rejected and original route unavailable")
		}
		return task.Profile, nil
	}
	proposed.Reason = "operator_approved_material_route"
	return proposed, nil
}
