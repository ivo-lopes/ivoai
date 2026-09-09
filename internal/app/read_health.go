package app

import "github.com/ivo-lopes/ivoai/internal/terminalui"

func readHealthStatus(state string) statusValue {
	switch state {
	case "healthy":
		return statusValue{"ready / authenticated MCP read", terminalui.StatusSuccess}
	case "", "not_probed":
		return statusValue{"unknown / not probed", terminalui.StatusNeutral}
	case "not_configured":
		return statusValue{"not configured", terminalui.StatusNeutral}
	default:
		return statusValue{state + " / MCP read", terminalui.StatusWarning}
	}
}
