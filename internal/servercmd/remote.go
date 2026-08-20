package servercmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/config"
	"github.com/ivo-lopes/ivoai/internal/connections"
	"github.com/ivo-lopes/ivoai/internal/secrets"
)

func (r *runner) remote(ctx context.Context, args []string) error {
	path := ""
	if len(args) == 1 && (args[0] == "status" || args[0] == "doctor") {
		path = "/v1/remote/" + args[0]
	} else if len(args) == 2 && args[0] == "connector" && args[1] == "list" {
		path = "/v1/remote/connectors"
	} else {
		return errors.New("usage: ivoai server remote <status|doctor|connector list>")
	}
	paths, err := config.ResolvePaths()
	if err != nil {
		return err
	}
	store := config.NewStore(paths)
	cfg, err := store.Load()
	if err != nil {
		return err
	}
	if cfg.Connections.Server.Status != "connected" || cfg.Connections.Server.URL == "" {
		return errors.New("no ivoai server is connected")
	}
	secretData, err := (secrets.Store{Path: paths.Secrets}).Load()
	if err != nil {
		return err
	}
	if secretData.Server == nil || secretData.Server.Token == "" {
		return errors.New("connected server credential is unavailable")
	}
	base, err := connections.ValidateBaseURL(cfg.Connections.Server.URL)
	if err != nil {
		return fmt.Errorf("stored server URL is unsafe: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base.String(), "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+secretData.Server.Token)
	resp, err := connections.SecureHTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("remote server request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("remote server returned HTTP %d", resp.StatusCode)
	}
	var response any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&response); err != nil {
		return fmt.Errorf("decode remote response: %w", err)
	}
	b, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(r.out, string(b))
	return err
}
