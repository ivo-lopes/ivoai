package quota

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/ivo-lopes/ivoai/internal/platform"
)

// AccountReference hashes identity metadata reported by the official client.
// It never opens auth files and never returns email, credentials or raw output.
// Providers without an exposed identity deliberately return no resume proof.
func AccountReference(parent context.Context, binary, provider string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 12*time.Second)
	defer cancel()
	if provider == "claude" {
		result, err := (platform.ExecRunner{}).Run(ctx, binary, []string{"auth", "status"}, platform.RunOptions{Timeout: 10 * time.Second, Env: []string{"DISABLE_AUTOUPDATER=1"}})
		if err != nil || !claudeAuthenticated(result.Stdout) {
			return "", errors.New("PROVIDER_AUTH_UNAVAILABLE")
		}
		var value struct {
			Email          string `json:"email"`
			OrganizationID string `json:"orgId"`
		}
		if json.Unmarshal([]byte(result.Stdout), &value) != nil || value.Email == "" {
			return "", nil
		}
		return identityReference(provider, value.Email+"\x00"+value.OrganizationID), nil
	}
	if provider != "codex" {
		return "", nil
	}
	cmd := exec.CommandContext(ctx, binary, "app-server", "--stdio")
	cmd.Env = subscriptionEnvironment(binary)
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	input, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err = cmd.Start(); err != nil {
		return "", errors.New("PROVIDER_AUTH_UNAVAILABLE")
	}
	defer func() { _ = input.Close(); _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() }()
	encoder := json.NewEncoder(input)
	scanner := bufio.NewScanner(io.LimitReader(output, 2<<20))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	if encoder.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "ivoai-continuity", "version": "1"}}}) != nil {
		return "", errors.New("PROVIDER_AUTH_UNAVAILABLE")
	}
	if _, err = waitRPC(scanner, 1); err != nil {
		return "", errors.New("PROVIDER_AUTH_UNAVAILABLE")
	}
	_ = encoder.Encode(map[string]any{"method": "initialized"})
	if encoder.Encode(map[string]any{"id": 2, "method": "account/read", "params": map[string]any{"refreshToken": false}}) != nil {
		return "", errors.New("PROVIDER_AUTH_UNAVAILABLE")
	}
	response, err := waitRPC(scanner, 2)
	if err != nil {
		return "", errors.New("PROVIDER_AUTH_UNAVAILABLE")
	}
	var value struct {
		Result struct {
			Account *struct {
				Type  string  `json:"type"`
				Email *string `json:"email"`
			} `json:"account"`
		} `json:"result"`
	}
	if json.Unmarshal(response, &value) != nil || value.Result.Account == nil {
		return "", errors.New("PROVIDER_AUTH_UNAVAILABLE")
	}
	account := value.Result.Account
	if account.Type != "chatgpt" || account.Email == nil || *account.Email == "" {
		return "", nil
	}
	return identityReference(provider, *account.Email), nil
}

func identityReference(provider, identity string) string {
	digest := sha256.Sum256([]byte(provider + "\x00" + strings.ToLower(strings.TrimSpace(identity))))
	return "account_" + hex.EncodeToString(digest[:])
}
