package benchmarkfixtures

import (
	"bytes"
	"strings"
	"testing"
)

func TestConcurrentCompositionContractKeepsMatrixUnsupported(t *testing.T) {
	value, err := ConcurrentProjectContract()
	if err != nil || len(value.Children) != 2 || value.MaxClients != 2 {
		t.Fatalf("contract unavailable: %v", err)
	}
	scenario, ok := Lookup(value.Scenario)
	if !ok || scenario.RunnerSupported || scenario.Execution != "unavailable" {
		t.Fatal("composition declaration falsely enabled runtime")
	}
	digest, err := ConcurrentProjectContractSHA256()
	if err != nil || len(digest) != 64 {
		t.Fatal("composition source binding unavailable")
	}
	t.Logf("CONCURRENT_CONTRACT_SHA256=%s", digest)
}

func TestConcurrentCompositionRejectsUnboundedMutableOrUnknownChildren(t *testing.T) {
	body, _ := inputs.ReadFile("data/concurrent-projects.json")
	for name, changed := range map[string][]byte{
		"unknown field":  bytes.Replace(body, []byte(`"max_clients": 2`), []byte(`"max_clients": 2, "command": "anything"`), 1),
		"case alias":     bytes.Replace(body, []byte(`"max_clients"`), []byte(`"MAX_CLIENTS"`), 1),
		"three clients":  bytes.Replace(body, []byte(`"max_clients": 2`), []byte(`"max_clients": 3`), 1),
		"no overlap":     bytes.Replace(body, []byte(`"client_overlap_required": true`), []byte(`"client_overlap_required": false`), 1),
		"mutable":        bytes.Replace(body, []byte(`"read_only"`), []byte(`"workspace_edit"`), 1),
		"duplicate key":  bytes.Replace(body, []byte(`"max_clients": 2`), []byte(`"max_clients": 3, "max_clients": 2`), 1),
		"wrong fixture":  bytes.Replace(body, []byte(`"python-deps"`), []byte(`"go-core"`), 1),
		"unknown child":  bytes.Replace(body, []byte(`"dependencies"`), []byte(`"multiple_workers"`), 1),
		"same child":     bytes.Replace(body, []byte(`{"scenario": "dependencies", "fixture": "python-deps"}`), []byte(`{"scenario": "implementation", "fixture": "go-core"}`), 1),
		"nested dup key": bytes.Replace(body, []byte(`"fixture": "python-deps"`), []byte(`"fixture": "go-core", "fixture": "python-deps"`), 1),
		"trailing JSON":  append(append([]byte{}, body...), []byte(` {}`)...),
		"oversized":      []byte(strings.Repeat(" ", 4097)),
	} {
		t.Run(name, func(t *testing.T) {
			if bytes.Equal(changed, body) {
				t.Fatal("adverse input did not change")
			}
			if _, err := parseConcurrentContract(changed); err == nil {
				t.Fatal("unsafe composition admitted")
			}
		})
	}
}
