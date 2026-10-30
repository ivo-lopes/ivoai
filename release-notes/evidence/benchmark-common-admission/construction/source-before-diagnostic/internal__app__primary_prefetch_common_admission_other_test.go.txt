//go:build !linux

package app

import "context"

// The reviewed pinned owned transport is Linux-only; preserve compilation and
// refuse before any client/model start on other systems.
func prefetchCommonLiveAdmission(_ context.Context, _ prefetchDirectScope, _ prefetchSubscriptionHostPolicy) (*prefetchCommonAdmission, prefetchCommonObservation) {
	value := prefetchCommonNotRun()
	value.Reason = "TRANSPORT_UNAVAILABLE"
	return nil, value
}
