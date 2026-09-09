package cli

import (
	"errors"
	"io"
	"os"
	"strings"

	"github.com/ivo-lopes/ivoai/internal/app"
	"golang.org/x/term"
)

// A real terminal always uses no-echo input. Pipes are bounded and are never
// copied into an error, command argument, status line, or debug message.
func readMCPSecret(a *app.App) (string, error) {
	if file, ok := a.In.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		return a.Prompt("Credential (hidden): ", true)
	}
	body, err := io.ReadAll(io.LimitReader(a.In, 8195))
	if err != nil {
		return "", errors.New("unable to read credential from stdin")
	}
	value := strings.TrimSuffix(strings.TrimSuffix(string(body), "\n"), "\r")
	if len(value) == 0 || len(value) > 8192 {
		return "", errors.New("credential input is empty or exceeds limit")
	}
	return value, nil
}
