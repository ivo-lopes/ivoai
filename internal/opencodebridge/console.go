package opencodebridge

import (
	"encoding/json"
	"io"
	"net/http"
)

type ConsoleSession struct {
	ID        string `json:"id"`
	NativeID  string `json:"native_id,omitempty"`
	Primary   string `json:"primary"`
	Mode      string `json:"mode"`
	State     string `json:"state"`
	Resumable bool   `json:"resumable"`
}

func (b *Bridge) consoleSessionList(w http.ResponseWriter, _ *http.Request) {
	values := []ConsoleSession{}
	if b.consoleSessions != nil {
		for _, v := range b.consoleSessions() {
			if len(values) >= 128 {
				break
			}
			if !safeID(v.ID) || v.NativeID != "" && !safeID(v.NativeID) {
				continue
			}
			values = append(values, v)
		}
	}
	writeJSON(w, 200, values)
}

func (b *Bridge) consoleResume(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID      string `json:"id"`
		Confirm bool   `json:"confirm"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	d.DisallowUnknownFields()
	if d.Decode(&request) != nil || d.Decode(new(any)) != io.EOF || !request.Confirm || !safeID(request.ID) {
		writeJSON(w, 400, map[string]string{"error": "EXPLICIT_CONFIRMATION_REQUIRED"})
		return
	}
	if !b.writer.TryLock() {
		writeJSON(w, 409, map[string]string{"error": "TURN_ACTIVE"})
		return
	}
	defer b.writer.Unlock()
	if b.consoleSessions != nil && b.selectConversation != nil {
		for _, value := range b.consoleSessions() {
			if value.ID == request.ID && value.Resumable && safeID(value.NativeID) {
				if err := b.selectConversation(value.NativeID); err != nil {
					writeJSON(w, 409, map[string]string{"error": "SESSION_RESUME_UNAVAILABLE"})
					return
				}
				writeJSON(w, 200, map[string]string{"native_id": value.NativeID})
				return
			}
		}
	}
	writeJSON(w, 409, map[string]string{"error": "SESSION_RESUME_REQUIRES_SESSION_CONTROL"})
}

func (b *Bridge) consoleCatalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"revision": b.catalog.revision, "models": b.catalog.Entries(), "availability": "session catalog; eligibility revalidated at turn admission"})
}

// Only explicit, fixed-domain decisions cross this boundary. No command, path,
// token, arbitrary config key or tool grant can be supplied by the frontend.
func (b *Bridge) consoleAct(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Action  string `json:"action"`
		Confirm bool   `json:"confirm"`
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	d.DisallowUnknownFields()
	if d.Decode(&request) != nil || d.Decode(new(any)) != io.EOF || !request.Confirm {
		writeJSON(w, 400, map[string]string{"error": "EXPLICIT_CONFIRMATION_REQUIRED"})
		return
	}
	switch request.Action {
	case "hooks.validate", "hooks.repair", "profile.economic", "profile.balanced", "profile.quality", "profile.custom":
	default:
		writeJSON(w, 400, map[string]string{"error": "UNSUPPORTED_CONSOLE_ACTION"})
		return
	}
	if b.consoleAction == nil {
		writeJSON(w, 409, map[string]string{"error": "CONSOLE_ACTION_UNAVAILABLE"})
		return
	}
	if err := b.consoleAction(r.Context(), request.Action); err != nil {
		writeJSON(w, 409, map[string]string{"error": "CONSOLE_ACTION_FAILED"})
		return
	}
	writeJSON(w, 200, map[string]string{"state": "applied", "effective": "profiles: next session; hooks: validated owned wiring"})
}
