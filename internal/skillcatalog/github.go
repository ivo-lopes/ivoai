package skillcatalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"

	"github.com/ivo-lopes/ivoai/internal/supplychain"
)

const maxGitHubArchiveBytes = 64 << 20

// GitHubSource uses only GitHub's public structured APIs. Resolve discovers
// the repository's actual default branch, pins its head commit, downloads a
// bounded archive as data, and records a local digest. It does not read auth
// files, execute repository code, or claim independent signature verification.
type GitHubSource struct {
	Client  *http.Client
	APIBase string
	mu      sync.Mutex
	cache   map[string][]byte
}

func (g *GitHubSource) Resolve(ctx context.Context, reference supplychain.Reference) (supplychain.ResolvedSource, error) {
	if reference.Kind != supplychain.KindSkill || reference.Version != "" {
		return supplychain.ResolvedSource{}, errors.New("GitHub skill discovery accepts only a floating source request; the result is pinned")
	}
	owner, repository, err := githubCoordinates(reference.Source)
	if err != nil {
		return supplychain.ResolvedSource{}, err
	}
	base := strings.TrimSuffix(g.APIBase, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	var metadata struct {
		DefaultBranch string `json:"default_branch"`
		ArchiveURL    string `json:"archive_url"`
	}
	if err := g.getJSON(ctx, base+"/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repository), &metadata); err != nil {
		return supplychain.ResolvedSource{}, fmt.Errorf("discover GitHub repository: %w", err)
	}
	if metadata.DefaultBranch == "" || len(metadata.DefaultBranch) > 256 {
		return supplychain.ResolvedSource{}, errors.New("GitHub did not expose a bounded default branch")
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if err := g.getJSON(ctx, base+"/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(repository)+"/commits/"+url.PathEscape(metadata.DefaultBranch), &commit); err != nil {
		return supplychain.ResolvedSource{}, fmt.Errorf("resolve GitHub default branch: %w", err)
	}
	if !immutableRevision(commit.SHA) {
		return supplychain.ResolvedSource{}, errors.New("GitHub did not resolve an immutable commit")
	}
	archiveURL := base + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repository) + "/tarball/" + commit.SHA
	archive, err := g.getBytes(ctx, archiveURL, maxGitHubArchiveBytes)
	if err != nil {
		return supplychain.ResolvedSource{}, fmt.Errorf("fetch immutable GitHub archive: %w", err)
	}
	digest := sha256.Sum256(archive)
	resolved := supplychain.ResolvedSource{
		ID: reference.ID, Kind: supplychain.KindSkill, Source: reference.Source,
		Revision: commit.SHA, LogicalVersion: commit.SHA, DefaultBranch: metadata.DefaultBranch,
		Integrity: supplychain.Integrity{Algorithm: "sha256", Digest: hex.EncodeToString(digest[:]), SignatureStatus: "not_exposed", AttestationStatus: "not_exposed", TrustLevel: "commit_pinned_local_digest"},
	}
	if err := resolved.Validate(); err != nil {
		return supplychain.ResolvedSource{}, err
	}
	g.mu.Lock()
	if g.cache == nil {
		g.cache = map[string][]byte{}
	}
	g.cache[cacheKey(resolved)] = append([]byte(nil), archive...)
	g.mu.Unlock()
	return resolved, nil
}

func (g *GitHubSource) Fetch(_ context.Context, source supplychain.ResolvedSource) (io.ReadCloser, error) {
	if err := source.Validate(); err != nil {
		return nil, err
	}
	g.mu.Lock()
	archive := append([]byte(nil), g.cache[cacheKey(source)]...)
	g.mu.Unlock()
	if len(archive) == 0 {
		return nil, errors.New("immutable GitHub archive was not resolved in this process")
	}
	return io.NopCloser(bytes.NewReader(archive)), nil
}

func (g *GitHubSource) getJSON(ctx context.Context, endpoint string, target any) error {
	data, err := g.getBytes(ctx, endpoint, 1<<20)
	if err != nil {
		return err
	}
	// GitHub responses have many fields by design, so decode through a raw map
	// and then project only the allowlisted fields needed by the caller.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	projected := map[string]json.RawMessage{}
	for _, key := range []string{"default_branch", "archive_url", "sha"} {
		if value, ok := raw[key]; ok {
			projected[key] = value
		}
	}
	clean, err := json.Marshal(projected)
	if err != nil {
		return err
	}
	return decoderFor(clean).Decode(target)
}

func decoderFor(data []byte) *json.Decoder {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder
}

func (g *GitHubSource) getBytes(ctx context.Context, endpoint string, maximum int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	client := g.Client
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maximum {
		return nil, errors.New("GitHub response exceeds bounded size")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, errors.New("GitHub response exceeds bounded size")
	}
	return data, nil
}

func githubCoordinates(value string) (string, string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, "github.com") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", "", errors.New("skill source must be an HTTPS GitHub repository")
	}
	parts := strings.Split(strings.Trim(path.Clean(parsed.Path), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(parts[0]+parts[1], "\\\x00") {
		return "", "", errors.New("invalid GitHub repository path")
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git"), nil
}

func cacheKey(source supplychain.ResolvedSource) string {
	return source.ID + "\x00" + source.Source + "\x00" + source.Revision + "\x00" + source.Integrity.Digest
}
