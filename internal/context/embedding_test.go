package context

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestHTTPEmbedderAppliesE5Prefixes(t *testing.T) {
	var requests [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer embedding-internal-token" {
			t.Errorf("missing private embedding authorization")
		}
		var body struct {
			Inputs []string `json:"inputs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests = append(requests, body.Inputs)
		_ = json.NewEncoder(w).Encode([][]float32{{1, 0}, {0, 1}}[:len(body.Inputs)])
	}))
	defer server.Close()

	embedder := HTTPEmbedder{BaseURL: server.URL, DimensionsN: 2, Client: server.Client(), APIKey: "embedding-internal-token"}
	if _, err := embedder.EmbedDocuments(context.Background(), []string{"alpha", "beta"}); err != nil {
		t.Fatal(err)
	}
	if _, err := embedder.EmbedQuery(context.Background(), "alpha"); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"passage: alpha", "passage: beta"}, {"query: alpha"}}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("embedding inputs = %#v, want %#v", requests, want)
	}
}
