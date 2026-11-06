package context

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestQdrantColdCreateCanExceedReadBudgetWithoutRetry(t *testing.T) {
	var creates atomic.Int32
	var created atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			creates.Add(1)
			// This exceeds the previous production HTTP default and fails on that
			// implementation. No CPU work or provider/model is involved.
			select {
			case <-time.After(16 * time.Second):
			case <-r.Context().Done():
				return
			}
			created.Store(true)
			w.Write([]byte(`{"result":true,"status":"ok"}`))
			return
		}
		if !created.Load() {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte(`{"result":{"config":{"params":{"vectors":{"size":384}}}}}`))
	}))
	defer server.Close()
	store := QdrantStore{BaseURL: server.URL, Collection: "cold-budget"}
	if err := store.Ensure(context.Background(), 384); err != nil {
		t.Fatal("cold create still used the short read budget", err)
	}
	if creates.Load() != 1 {
		t.Fatal("collection creation was replayed")
	}
	if store.client().Timeout != 15*time.Second {
		t.Fatal("ordinary operation timeout widened")
	}
}

func TestQdrantColdCreateHonorsCallerCancellationAndExplicitClient(t *testing.T) {
	for _, mode := range []string{"caller", "client"} {
		t.Run(mode, func(t *testing.T) {
			var creates atomic.Int32
			entered := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				creates.Add(1)
				io.Copy(io.Discard, r.Body)
				r.Body.Close()
				close(entered)
				<-r.Context().Done()
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			store := QdrantStore{BaseURL: server.URL, Collection: "cancel-budget"}
			if mode == "client" {
				store.Client = &http.Client{Timeout: 50 * time.Millisecond}
			} else {
				go func() { <-entered; cancel() }()
			}
			started := time.Now()
			err := store.Ensure(ctx, 384)
			if err == nil || creates.Load() != 1 || time.Since(started) > 500*time.Millisecond {
				t.Fatal("cancellation/client budget lost or create replayed", err, creates.Load())
			}
			if mode == "caller" && !errors.Is(err, context.Canceled) {
				t.Fatal("caller cancellation classification lost", err)
			}
		})
	}
}
