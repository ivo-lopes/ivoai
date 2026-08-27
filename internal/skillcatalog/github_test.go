package skillcatalog

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

func TestGitHubSourceDiscoversDefaultBranchAndPinsRevision(t *testing.T) {
	revision := "1234567890abcdef1234567890abcdef12345678"
	archive := []byte("bounded archive data")
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requested = append(requested, request.URL.Path)
		switch request.URL.Path {
		case "/repos/example/skills":
			fmt.Fprint(response, `{"default_branch":"trunk","ignored":"untrusted"}`)
		case "/repos/example/skills/commits/trunk":
			fmt.Fprintf(response, `{"sha":%q,"other":"ignored"}`, revision)
		case "/repos/example/skills/tarball/" + revision:
			response.Write(archive)
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()

	source := &GitHubSource{Client: server.Client(), APIBase: server.URL}
	reference := supplychain.Reference{ID: "example-skills", Kind: supplychain.KindSkill, Source: "https://github.com/example/skills"}
	resolved, err := source.Resolve(context.Background(), reference)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.DefaultBranch != "trunk" || resolved.Revision != revision || resolved.Integrity.TrustLevel != "commit_pinned_local_digest" || resolved.Integrity.SignatureStatus != "not_exposed" {
		t.Fatalf("resolved=%+v", resolved)
	}
	reader, err := source.Fetch(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, _ := io.ReadAll(reader)
	if string(got) != string(archive) {
		t.Fatalf("archive=%q", got)
	}
	wantRequest := "/repos/example/skills/commits/trunk"
	if !contains(requested, wantRequest) {
		t.Fatalf("default branch was not used: %v", requested)
	}
}

func TestGitHubSourceRejectsFloatingOutputAndOversizedFetch(t *testing.T) {
	source := &GitHubSource{}
	_, err := source.Resolve(context.Background(), supplychain.Reference{ID: "x", Kind: supplychain.KindSkill, Source: "https://github.com/example/skills", Version: "main"})
	if err == nil || !strings.Contains(err.Error(), "floating source") {
		t.Fatalf("err=%v", err)
	}
	if _, _, err := githubCoordinates("https://example.com/example/skills"); err == nil {
		t.Fatal("non-GitHub source accepted")
	}
	resolved := supplychain.ResolvedSource{ID: "missing", Kind: supplychain.KindSkill, Source: "https://github.com/example/skills", Revision: "1234567890abcdef1234567890abcdef12345678", Integrity: supplychain.Integrity{Algorithm: "sha256", Digest: strings.Repeat("a", 64), SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "commit_pinned_local_digest"}}
	if _, err := source.Fetch(context.Background(), resolved); err == nil {
		t.Fatal("fetch without an in-process immutable resolution succeeded")
	}
}
