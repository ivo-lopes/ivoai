package opencodebridge

import (
	"encoding/json"
	"io"
	"net/http"
)

type PermissionView struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

func (b *Bridge) nativePermissionList(w http.ResponseWriter, r *http.Request) {
	pending := []PermissionView{}
	if b.nativePermissions != nil {
		pending = b.nativePermissions()
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pending)
}
func (b *Bridge) nativePermissionReply(w http.ResponseWriter, r *http.Request) {
	var reply struct {
		ID    string `json:"id"`
		Allow bool   `json:"allow"`
	}
	decoder := json.NewDecoder(io.LimitReader(r.Body, 4097))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&reply) != nil || decoder.Decode(new(any)) != io.EOF || !safeID(reply.ID) || b.replyNativePermission == nil {
		http.Error(w, "invalid native permission reply", 400)
		return
	}
	if err := b.replyNativePermission(r.Context(), reply.ID, reply.Allow); err != nil {
		http.Error(w, "native permission unavailable", 409)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
