package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ivo-lopes/ivoai/internal/core"
	"github.com/ivo-lopes/ivoai/internal/opencodebridge"
)

func (e OpenCodeExecutor) startControlled(ctx context.Context, request core.SessionRequest, observe func(core.SessionObservation)) error {
	if len(request.Args) != 0 {
		return errors.New("controlled OpenCode requires structured prompt/model, not CLI arguments")
	}
	options := e.Options
	options.ResumeSessionID = request.ResumeID
	s, err := e.OpenSession(ctx, options)
	if err != nil {
		return err
	}
	defer s.Close(context.Background())
	if observe != nil {
		observe(core.SessionObservation{})
	}
	payload := map[string]any{"parts": []map[string]string{{"type": "text", "text": request.Prompt}}}
	if request.Model != "" {
		provider, model, ok := strings.Cut(request.Model, "/")
		if !ok || provider == "" || model == "" {
			return errors.New("OpenCode model must be provider/model")
		}
		payload["model"] = map[string]string{"providerID": provider, "modelID": model}
	}
	body, err := s.Prompt(ctx, payload, false)
	if err != nil {
		return err
	}
	var response struct {
		Info struct {
			Error      json.RawMessage `json:"error"`
			Finish     string          `json:"finish"`
			ModelID    string          `json:"modelID"`
			ProviderID string          `json:"providerID"`
		} `json:"info"`
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
	}
	if json.Unmarshal(body, &response) != nil || len(response.Info.Error) > 0 && string(response.Info.Error) != "null" {
		return errors.New("OpenCode executor failed; partial output not accepted")
	}
	if request.Model != "" && response.Info.ProviderID+"/"+response.Info.ModelID != request.Model {
		return errors.New("OpenCode explicit model did not match the effective response")
	}
	var output strings.Builder
	for _, part := range response.Parts {
		if part.Type == "text" {
			output.WriteString(part.Text)
		}
	}
	if output.Len() == 0 || response.Info.Finish == "" {
		return errors.New("OpenCode executor stream incomplete")
	}
	if e.Runtime.Out != nil {
		_, err = fmt.Fprint(e.Runtime.Out, output.String())
	}
	return err
}

// OpenCodeSession is a controlled session, independent of the optional native
// TUI. It reuses the managed loopback transport, auth, bounds and process lease.
type OpenCodeSession struct {
	backend *opencodebridge.Managed
	id      string
	mu      sync.Mutex
	closed  bool
}

func (e OpenCodeExecutor) OpenSession(ctx context.Context, options opencodebridge.ManagedOptions) (*OpenCodeSession, error) {
	if options.OpenCodePath == "" {
		options.OpenCodePath = e.Runtime.AgentPath
	}
	if options.Version == "" {
		options.Version = e.Version
	}
	if options.Bridge == nil {
		options.NativeExecutor = true
	}
	backend, err := opencodebridge.StartManaged(ctx, options)
	if err != nil {
		return nil, err
	}
	operation := "create"
	if options.ResumeSessionID != "" {
		operation = "get"
	}
	body, err := backend.APIRequest(ctx, operation, options.ResumeSessionID, nil, map[string]string{"title": "IVOAI controlled session"})
	var session struct {
		ID string `json:"id"`
	}
	if err == nil {
		err = json.Unmarshal(body, &session)
	}
	if err != nil || session.ID == "" {
		_ = backend.Close(context.Background())
		return nil, errors.New("OpenCode controlled session could not be created or resumed")
	}
	return &OpenCodeSession{backend: backend, id: session.ID}, nil
}

func (s *OpenCodeSession) ID() string        { return s.id }
func (s *OpenCodeSession) Transport() string { return "HTTP_SSE" }
func (s *OpenCodeSession) Request(ctx context.Context, operation string, query url.Values, payload any) (json.RawMessage, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, errors.New("OpenCode controlled session is closed")
	}
	return s.backend.APIRequest(ctx, operation, s.id, query, payload)
}
func (s *OpenCodeSession) Prompt(ctx context.Context, payload any, async bool) (json.RawMessage, error) {
	operation := "prompt"
	if async {
		operation = "prompt-async"
	}
	return s.Request(ctx, operation, nil, payload)
}
func (s *OpenCodeSession) Events(ctx context.Context, consume func(json.RawMessage) error) error {
	return s.backend.Events(ctx, consume)
}
func (s *OpenCodeSession) Cancel(ctx context.Context) error {
	_, err := s.Request(ctx, "abort", nil, nil)
	return err
}
func (s *OpenCodeSession) ReplyPermission(ctx context.Context, permissionID, reply string) error {
	if reply != "once" && reply != "always" && reply != "reject" {
		return errors.New("invalid permission reply")
	}
	_, err := s.backend.APIRequest(ctx, "permission-reply", permissionID, nil, map[string]string{"reply": reply})
	return err
}
func (s *OpenCodeSession) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Abort before shutting down; do not leave an invisible authoritative writer.
	_, _ = s.backend.APIRequest(ctx, "abort", s.id, nil, nil)
	return s.backend.Close(ctx)
}
