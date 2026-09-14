package codexfrontend

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/ivo-lopes/ivoai/internal/platform"
	"github.com/ivo-lopes/ivoai/internal/session"
)

// NativeThread intentionally excludes preview, name, turns, instructions and
// authentication. Discovery does not adopt anything or execute a thread.
type NativeThread struct {
	ID        string `json:"id"`
	Directory string `json:"cwd"`
	Provider  string `json:"modelProvider"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// DiscoverNative uses the official protocol, not a database parser or file
// copy. A supplied ID requests metadata for exactly that conversation.
func DiscoverNative(ctx context.Context, binary, cwd, runtimeRoot string, state NativeState, id string) ([]NativeThread, error) {
	if id != "" && !session.ValidNativeUUID(id) {
		return nil, errors.New("INVALID_NATIVE_SESSION")
	}
	if err := platform.EnsurePrivateDir(runtimeRoot); err != nil {
		return nil, errors.New("NATIVE_DISCOVERY_RUNTIME_UNAVAILABLE")
	}
	view, err := os.MkdirTemp(runtimeRoot, "native-discovery-")
	if err != nil {
		return nil, errors.New("NATIVE_DISCOVERY_RUNTIME_UNAVAILABLE")
	}
	defer os.RemoveAll(view) // only this freshly allocated directory; links are not followed
	if err := state.PrepareView(view); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	args := []string{"-c", "sqlite_home=" + quote(state.SQLiteHome), "-c", `features.hooks=false`, "-c", `features.plugins=false`, "-c", `features.remote_plugin=false`, "-c", `features.apps=false`, "app-server", "--listen", "stdio://"}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = cwd
	// No inherited provider credentials or personal configuration. Discovery
	// requires no model request, auth operation, or MCP startup.
	cmd.Env = []string{"HOME=" + view, "CODEX_HOME=" + view, "PATH=/usr/bin:/bin"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = time.Second
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, errors.New("NATIVE_DISCOVERY_START_FAILED")
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		_ = input.Close()
		return nil, errors.New("NATIVE_DISCOVERY_START_FAILED")
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = input.Close()
		_ = output.Close()
		return nil, errors.New("NATIVE_DISCOVERY_START_FAILED")
	}
	defer func() { _ = input.Close(); cancel(); _ = cmd.Wait() }()
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 4096), maxFrame)
	encoder := json.NewEncoder(input)
	sequence := 0
	call := func(method string, params any, result any) error {
		sequence++
		if encoder.Encode(map[string]any{"id": sequence, "method": method, "params": params}) != nil {
			return errors.New("NATIVE_DISCOVERY_PROTOCOL_FAILED")
		}
		for scanner.Scan() {
			var message rpc
			if json.Unmarshal(scanner.Bytes(), &message) != nil {
				return errors.New("NATIVE_DISCOVERY_PROTOCOL_FAILED")
			}
			if string(message.ID) != strconv.Itoa(sequence) || message.Method != "" {
				continue
			}
			if len(message.Error) != 0 {
				return errors.New("NATIVE_SESSION_NOT_AVAILABLE")
			}
			if result != nil && json.Unmarshal(message.Result, result) != nil {
				return errors.New("NATIVE_DISCOVERY_PROTOCOL_FAILED")
			}
			return nil
		}
		return errors.New("NATIVE_DISCOVERY_UNAVAILABLE")
	}
	if err := call("initialize", map[string]any{"clientInfo": map[string]string{"name": "ivoai_discovery", "version": "1"}}, nil); err != nil {
		return nil, err
	}
	if encoder.Encode(map[string]string{"method": "initialized"}) != nil {
		return nil, errors.New("NATIVE_DISCOVERY_PROTOCOL_FAILED")
	}
	var result []NativeThread
	if id != "" {
		var response struct {
			Thread NativeThread `json:"thread"`
		}
		if err := call("thread/read", map[string]any{"threadId": id, "includeTurns": false}, &response); err != nil {
			return nil, err
		}
		if response.Thread.ID != id {
			return nil, errors.New("NATIVE_DISCOVERY_ID_MISMATCH")
		}
		result = []NativeThread{response.Thread}
	} else {
		var response struct {
			Data []NativeThread `json:"data"`
		}
		if err := call("thread/list", map[string]any{"limit": 100, "cwd": cwd, "modelProviders": []string{"openai", "ivoai"}, "sourceKinds": []string{"cli", "vscode", "appServer", "exec"}}, &response); err != nil {
			return nil, err
		}
		result = response.Data
	}
	for _, thread := range result {
		if !session.ValidNativeUUID(thread.ID) || thread.Directory != cwd || len(thread.Directory) > 4096 || strings.IndexFunc(thread.Directory, unicode.IsControl) >= 0 || platform.Redact(thread.Directory) != thread.Directory || (thread.Provider != "openai" && thread.Provider != "ivoai") {
			return nil, errors.New("NATIVE_SESSION_NOT_PORTABLE")
		}
	}
	return result, nil
}
