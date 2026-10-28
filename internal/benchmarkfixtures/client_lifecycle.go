package benchmarkfixtures

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
)

// ClientIdentity is host correlation, never a grant or model-provided field.
type ClientIdentity struct {
	AttemptID         string `json:"attempt_id"`
	Scenario          string `json:"scenario"`
	Arm               string `json:"arm"`
	FixtureSHA256     string `json:"fixture_sha256"`
	HarnessSHA256     string `json:"harness_sha256"`
	CompositionSHA256 string `json:"composition_sha256"`
	ClientSHA256      string `json:"client_sha256"`
	IsolationSHA256   string `json:"isolation_sha256"`
}

type ClientLifecycle struct {
	SchemaVersion int            `json:"schema_version"`
	Clock         string         `json:"clock"`
	Identity      ClientIdentity `json:"identity"`
	PID           int            `json:"pid"`
	StartTicks    uint64         `json:"start_ticks"`
	StartedNS     int64          `json:"started_ns"`
	ReapedNS      int64          `json:"reaped_ns"`
	ExitCode      int            `json:"exit_code"`
}

//go:embed client_lifecycle.go client_lifecycle_linux.go client_lifecycle_other.go
var lifecycleSources embed.FS

func ClientLifecycleSHA256() string {
	hash := sha256.New()
	for _, name := range []string{"client_lifecycle.go", "client_lifecycle_linux.go", "client_lifecycle_other.go"} {
		body, _ := lifecycleSources.ReadFile(name)
		hash.Write([]byte(name + "\x00"))
		hash.Write(body)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
