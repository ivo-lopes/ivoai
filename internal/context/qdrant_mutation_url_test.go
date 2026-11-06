package context

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestQdrantMutationsSendWaitAsQueryAndNeverReplay(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		if r.Header.Get("api-key") != "private-fixture-key" {
			t.Error("backend authorization missing")
		}
		if r.URL.Query().Get("wait") != "true" {
			t.Errorf("missing synchronous mutation query: %s", r.URL.RequestURI())
		}
		if r.URL.Path != "/private/collections/test/points/delete" && r.URL.Path != "/private/collections/test/points" {
			t.Errorf("incorrect resource path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	store := QdrantStore{BaseURL: server.URL + "/private", Collection: "test", APIKey: "private-fixture-key"}
	if err := store.ReplaceDocument(context.Background(), "document", []Chunk{{ID: "point", Vector: []float32{1}}}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteDocuments(context.Background(), []string{"document"}); err != nil {
		t.Fatal(err)
	}
	expected := []string{"POST /private/collections/test/points/delete?wait=true", "PUT /private/collections/test/points?wait=true", "POST /private/collections/test/points/delete?wait=true"}
	if len(requests) != len(expected) {
		t.Fatalf("unexpected mutation count: %v", requests)
	}
	for i := range expected {
		if requests[i] != expected[i] {
			t.Errorf("request %d: got %q want %q", i, requests[i], expected[i])
		}
	}
}

func TestQdrantMutationFailureIsNotRetried(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusServiceUnavailable) }))
	defer server.Close()
	store := QdrantStore{BaseURL: server.URL, Collection: "test"}
	if err := store.ReplaceDocument(context.Background(), "document", []Chunk{{ID: "point"}}); err == nil {
		t.Fatal("expected failure")
	}
	if calls != 1 {
		t.Fatalf("failed mutation replayed: %d calls", calls)
	}
}

func TestQdrantMutationPreservesEscapedProxyPathAndCollectionBoundary(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.RequestURI != "/private%2Fproxy/collections/owned/points/delete?wait=true" {
			t.Errorf("proxy escaping lost: %s", r.RequestURI)
		}
		if r.URL.Path != "/private/proxy/collections/owned/points/delete" || r.URL.RawQuery != "wait=true" {
			t.Errorf("resource/query invalid: %s", r.URL)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	store := QdrantStore{BaseURL: server.URL + "/private%2Fproxy", Collection: "owned"}
	if err := store.DeleteDocuments(context.Background(), []string{"doc"}); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"../foreign", "owned?foreign=true", "owned/foreign", "owned%2Fforeign"} {
		store.Collection = invalid
		if err := store.DeleteDocuments(context.Background(), []string{"doc"}); err == nil || !strings.Contains(err.Error(), "collection") {
			t.Errorf("foreign collection accepted: %q %v", invalid, err)
		}
	}
	if calls != 1 {
		t.Fatalf("invalid collection reached backend: %d", calls)
	}
}
