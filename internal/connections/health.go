package connections

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"
)

func (h ProfileHealth) MarshalJSON() ([]byte, error) {
	type wire ProfileHealth
	if h.State == "" {
		h.State = "not_probed"
	}
	if h.ContextState == "" {
		h.ContextState = "not_probed"
	}
	if h.MemoryState == "" {
		h.MemoryState = "not_probed"
	}
	return json.Marshal(wire(h))
}

// Public diagnostics classify failures without retaining response bodies or
// error strings. In particular, MCP read health never probes a hook/write URL.
func healthFailure(err error) string {
	if err == nil {
		return "healthy"
	}
	var transport *url.Error
	var network net.Error
	if errors.As(err, &transport) || errors.As(err, &network) {
		return "transport_error"
	}
	value := err.Error()
	if strings.Contains(value, "HTTP 401") || strings.Contains(value, "HTTP 403") {
		return "auth_error"
	}
	if strings.Contains(value, "HTTP 5") {
		return "upstream_error"
	}
	return "protocol_error"
}
