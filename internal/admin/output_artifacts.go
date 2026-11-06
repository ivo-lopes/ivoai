package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ivo-lopes/ivoai/internal/secrets"
	"github.com/ivo-lopes/ivoai/internal/workingcontext"
)

const maxAdministrativeOutputBytes = 16 << 20
const administrativeOutputTTL = 15 * time.Minute

type administrativeOutputRef struct {
	Result      workingcontext.ResultRef `json:"result"`
	DownloadURL string                   `json:"download_url"`
	Sanitized   bool                     `json:"sanitized"`
}

// The owner binds the host resource, OAuth client and exact authenticated
// credential (and therefore its grant). A new token never inherits old outputs.
// Token values, parameters and request receipts are not stored with the artifact.
func (s *Server) outputOwner(r *http.Request) (workingcontext.Ownership, error) {
	p, err := s.principal(r)
	if err != nil {
		return workingcontext.Ownership{}, err
	}
	digest := sha256.Sum256([]byte(s.instance + "\x00" + s.Resource() + "\x00" + p.ClientID + "\x00" + s.tokenHash(r)))
	return workingcontext.Ownership{SessionID: "sess_" + hex.EncodeToString(digest[:16])}, nil
}

func (s *Server) newOutputStore() (workingcontext.ArtifactStore, error) {
	limits := workingcontext.DefaultLimits()
	limits.MaxArtifactBytes = maxAdministrativeOutputBytes
	limits.DefaultTTL, limits.MaxTTL = administrativeOutputTTL, administrativeOutputTTL
	return workingcontext.NewLocalStore(filepath.Join(s.paths.StateDir, "admin", s.instance, "outputs"), workingcontext.LocalOptions{Limits: limits})
}

// Retain only the sanitized projection. Raw command output never enters a file.
// Refuse incomplete capture/quota failure without ever replaying the operation.
func (s *Server) retainOutput(ctx context.Context, r *http.Request, action string, sanitized string) (administrativeOutputRef, error) {
	owner, err := s.outputOwner(r)
	if err != nil || !s.rootValid() {
		return administrativeOutputRef{}, errors.New("authority unavailable")
	}
	_, _ = s.outputs.GC(ctx)
	ref, err := s.outputs.Put(ctx, workingcontext.PutRequest{Kind: workingcontext.ArtifactStdout, MediaType: "text/plain; charset=utf-8", Owner: owner, Sensitivity: workingcontext.SensitivityRestricted, TTL: administrativeOutputTTL}, strings.NewReader(sanitized))
	if err != nil {
		return administrativeOutputRef{}, err
	}
	return administrativeOutputRef{Result: workingcontext.ResultRef{Artifact: ref, Role: workingcontext.EvidencePrimary, AssociationID: action}, DownloadURL: s.basePath + "/api/admin/v1/outputs/" + ref.ID, Sanitized: true}, nil
}

func (s *Server) downloadOutput(w http.ResponseWriter, r *http.Request) {
	principal, _ := s.principal(r)
	verdict := "deny"
	defer func() { s.emit("output_download", verdict, principal.ClientID) }()
	owner, err := s.outputOwner(r)
	if err != nil {
		errorJSON(w, 401, "unauthorized")
		return
	}
	id := r.PathValue("id")
	if r.URL.RawQuery != "" || !workingcontext.ValidArtifactID(id) {
		errorJSON(w, 404, "output_unavailable")
		return
	}
	reader, ref, err := s.outputs.Read(r.Context(), owner, id)
	if err != nil {
		errorJSON(w, 404, "output_unavailable")
		return
	}
	defer reader.Close()
	// Read verifies artifact integrity before anything is returned. Recheck
	// authority after I/O and before headers; no bearer URL or public cache.
	body, err := io.ReadAll(io.LimitReader(reader, maxAdministrativeOutputBytes+1))
	if err != nil || len(body) > maxAdministrativeOutputBytes || !ref.Complete {
		errorJSON(w, 503, "output_unavailable")
		return
	}
	if _, err := s.outputOwner(r); err != nil || !s.rootValid() {
		errorJSON(w, 401, "unauthorized")
		return
	}
	if r.Context().Err() != nil {
		errorJSON(w, 503, "output_unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="ivoai-administrative-output.txt"`)
	_, _ = w.Write(body)
	verdict = "allow"
}

var activationCodeOutput = regexp.MustCompile(`\bivoai-web_[A-Za-z0-9_-]+\b`)

var privateKeyOutput = regexp.MustCompile(`(?s)-----BEGIN (?:[A-Z ]+ )?PRIVATE KEY-----.*?(?:-----END (?:[A-Z ]+ )?PRIVATE KEY-----|\z)`)

func (s *Server) outputSecrets(r *http.Request) ([]string, error) {
	values := []string{}
	if c, err := r.Cookie(s.cookie); err == nil {
		values = append(values, c.Value)
	}
	data, err := (secrets.Store{Path: s.paths.Secrets}).Load()
	if err != nil {
		return values, err
	}
	if data.Server != nil {
		values = append(values, data.Server.Token)
	}
	for _, item := range data.Servers {
		values = append(values, item.Token)
	}
	for _, item := range data.MCP {
		values = append(values, item.Bearer)
		for _, value := range item.Headers {
			values = append(values, value)
		}
	}
	return values, nil
}

func sanitizedAdministrativeOutput(raw string, input map[string]string, private []string) string {
	raw = normalizeAdministrativeOutput(raw)
	out := sanitizeOutput(activationCodeOutput.ReplaceAllString(privateKeyOutput.ReplaceAllString(raw, "[REDACTED PRIVATE KEY]"), "[REDACTED ACTIVATION CODE]"), input)
	if code := oneTimeCode(raw); code != "" {
		private = append(private, code)
	}
	for _, value := range private {
		if value != "" {
			out = strings.ReplaceAll(out, value, "[REDACTED]")
			if normalized := normalizeAdministrativeOutput(value); normalized != "" {
				out = strings.ReplaceAll(out, normalized, "[REDACTED]")
			}
		}
	}
	return out
}

// JSON escapes HTML/control characters, so a byte-bounded string can otherwise
// overflow the response envelope after a successful mutation and hide its ref.
func administrativeOutputPreview(sanitized string) string {
	preview := sanitized
	if len(preview) > maxResponseBytes/2 {
		preview = preview[:maxResponseBytes/2]
	}
	for {
		encoded, _ := json.Marshal(preview)
		if len(encoded) <= maxResponseBytes/2 {
			return preview
		}
		preview = preview[:len(preview)/2]
	}
}
