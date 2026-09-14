package agents

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func OpenCodeSessionInventory(parent context.Context, binary, cwd string) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "session", "list", "--format", "json", "--max-count", "100")
	cmd.Dir = cwd
	cmd.Stderr = io.Discard
	output, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, errors.New("NATIVE_INVENTORY_UNAVAILABLE")
	}
	body, readErr := io.ReadAll(io.LimitReader(output, 1<<20+1))
	if len(body) > 1<<20 {
		_ = cmd.Process.Kill()
	}
	err = cmd.Wait()
	if readErr != nil || err != nil || len(body) > 1<<20 {
		return nil, errors.New("NATIVE_INVENTORY_UNAVAILABLE")
	}
	ids := map[string]bool{}
	if strings.TrimSpace(string(body)) == "" {
		return ids, nil
	}
	var items []struct {
		ID        string `json:"id"`
		Directory string `json:"directory"`
	}
	if json.Unmarshal(body, &items) != nil {
		return nil, errors.New("NATIVE_INVENTORY_UNAVAILABLE")
	}
	for _, item := range items {
		if item.Directory == cwd && strings.HasPrefix(item.ID, "ses_") && len(item.ID) < 128 {
			ids[item.ID] = true
		}
	}
	return ids, nil
}

// CodexThreadInventory is a bounded, read-only native metadata query. It does
// not read rollouts, credentials, or user messages and never starts a turn.
func CodexThreadInventory(parent context.Context, binary, cwd string) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "app-server", "--stdio")
	cmd.Env = os.Environ()
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		return nil, errors.New("NATIVE_INVENTORY_UNAVAILABLE")
	}
	defer func() { _ = input.Close(); _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() }()
	encoder := json.NewEncoder(input)
	scanner := bufio.NewScanner(io.LimitReader(output, 2<<20))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	read := func(id int) (json.RawMessage, error) {
		for scanner.Scan() {
			var reply struct {
				ID     *int            `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if json.Unmarshal(scanner.Bytes(), &reply) == nil && reply.ID != nil && *reply.ID == id {
				if len(reply.Error) > 0 {
					return nil, errors.New("NATIVE_INVENTORY_UNAVAILABLE")
				}
				return reply.Result, nil
			}
		}
		return nil, errors.New("NATIVE_INVENTORY_UNAVAILABLE")
	}
	if encoder.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "ivoai-session-inventory", "version": "1"}, "capabilities": map[string]bool{"experimentalApi": true}}}) != nil {
		return nil, errors.New("NATIVE_INVENTORY_UNAVAILABLE")
	}
	if _, err = read(1); err != nil {
		return nil, err
	}
	_ = encoder.Encode(map[string]any{"method": "initialized"})
	if encoder.Encode(map[string]any{"id": 2, "method": "thread/list", "params": map[string]any{"limit": 100, "cwd": cwd, "sourceKinds": []string{"cli"}}}) != nil {
		return nil, errors.New("NATIVE_INVENTORY_UNAVAILABLE")
	}
	body, err := read(2)
	if err != nil {
		return nil, err
	}
	var result struct {
		Data []struct {
			ID  string `json:"id"`
			CWD string `json:"cwd"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &result) != nil {
		return nil, errors.New("NATIVE_INVENTORY_UNAVAILABLE")
	}
	ids := map[string]bool{}
	for _, thread := range result.Data {
		if thread.CWD == cwd && len(thread.ID) == 36 {
			ids[thread.ID] = true
		}
	}
	return ids, nil
}
