package codexfrontend

import (
	"encoding/json"
	"fmt"

	"github.com/coder/websocket"
)

// Request IDs are local to a connection. The upstream stdio transport has one
// namespace, so even primary requests use an opaque, monotonically unique ID.
type clientRequest struct {
	client *websocket.Conn
	id     json.RawMessage
	method string
}

func (f *Facade) disconnect(client *websocket.Conn, primary bool) {
	_ = client.CloseNow()
	f.mu.Lock()
	delete(f.clients, client)
	for id, request := range f.pending {
		if request.client == client {
			delete(f.pending, id)
		}
	}
	f.mu.Unlock()
	if primary {
		f.cancel()
	}
}

func (f *Facade) handleClient(client *websocket.Conn, primary bool, message rpc) {
	// Only the primary owns interactive approvals. A temporary discovery client
	// cannot answer a primary decision, even with a guessed request ID.
	if message.Method == "" {
		if primary {
			f.handle(message)
		}
		return
	}
	if !primary && message.Method == "initialized" {
		return
	}
	f.mu.Lock()
	if len(message.ID) != 0 {
		if len(f.pending) >= 256 {
			f.mu.Unlock()
			_ = client.Close(websocket.StatusPolicyViolation, "request limit")
			return
		}
		f.requestSequence++
		id, _ := json.Marshal(fmt.Sprintf("ivoai-rpc-%d", f.requestSequence))
		f.pending[string(id)] = clientRequest{client, append(json.RawMessage(nil), message.ID...), message.Method}
		message.ID = id
	}
	initialized := append(json.RawMessage(nil), f.initializedResult...)
	f.mu.Unlock()
	if !primary {
		switch message.Method {
		case "initialize":
			// The upstream process has already completed its single initialize
			// handshake. A picker negotiates the same pinned protocol locally.
			if len(initialized) == 0 {
				f.reject(message.ID, "PRIMARY_INITIALIZATION_PENDING")
			} else {
				f.send(rpc{ID: message.ID, Result: initialized})
			}
			return
		case "thread/list", "thread/loaded/list", "thread/read", "thread/turns/list", "thread/items/list", "model/list":
			// Discovery only; execution, subscription changes and approvals stay
			// on the primary connection. Turn notifications are primary-only.
		default:
			f.reject(message.ID, "PICKER_METHOD_DENIED")
			return
		}
	}
	f.handle(message)
}
