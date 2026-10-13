package opencodebridge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderReadinessRejectsAuthenticationWithoutRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "private-auth-detail", http.StatusUnauthorized)
	}))
	defer server.Close()
	m := &Managed{URL: server.URL, done: make(chan struct{}), expectedModels: []string{"auto"}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := m.waitProviderReady(ctx, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "authentication") || strings.Contains(err.Error(), "private-auth-detail") || calls.Load() != 1 {
		t.Fatalf("auth failure not classified/terminal: calls=%d err=%v", calls.Load(), err)
	}
}

func TestProviderReadinessReportsBoundedObservedState(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"loading", `private-loading`, "loading", 202},
		{"unavailable", `private-outage`, "unavailable", 503},
		{"empty", `{"providers":[]}`, "empty_catalog", 200},
		{"partial", `{"providers":[{"id":"ivoai","models":{}}]}`, "partial_catalog", 200},
		{"malformed", `private-invalid`, "invalid_response", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer server.Close()
			m := &Managed{URL: server.URL, done: make(chan struct{}), expectedModels: []string{"auto"}}
			ctx, cancel := context.WithTimeout(context.Background(), 280*time.Millisecond)
			defer cancel()
			err := m.waitProviderReady(ctx, t.TempDir())
			if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "private-") {
				t.Fatalf("missing sanitized readiness state: %v", err)
			}
		})
	}
}

func TestProviderReadinessRecoversFromLoadingAndEmpty(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.WriteHeader(202)
		case 2:
			fmt.Fprint(w, `{"providers":[]}`)
		default:
			fmt.Fprint(w, `{"providers":[{"id":"ivoai","models":{"auto":{}}}]}`)
		}
	}))
	defer server.Close()
	m := &Managed{URL: server.URL, done: make(chan struct{}), expectedModels: []string{"auto"}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := m.waitProviderReady(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatal("unexpected retry count")
	}
}

func TestProviderReadinessDoesNotFollowRedirectOrAcceptCachedCatalog(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		age, want string
	}{
		{"redirect", 302, "", "redirect"}, {"cached", 200, "5", "cached_response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var leaked atomic.Int32
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
			defer destination.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", destination.URL)
				w.Header().Set("Age", tc.age)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, `{"providers":[{"id":"ivoai","models":{"auto":{}}}]}`)
			}))
			defer server.Close()
			m := &Managed{URL: server.URL, password: "private-fixture", done: make(chan struct{}), expectedModels: []string{"auto"}}
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			err := m.waitProviderReady(ctx, t.TempDir())
			if err == nil || !strings.Contains(err.Error(), tc.want) || leaked.Load() != 0 {
				t.Fatalf("invalid readiness acceptance: %v", err)
			}
		})
	}
}
