package codexfrontend

import "encoding/json"

// Listing/preview remains native and transient. The mapping registry limits
// discovery to conversations actually owned by the current managed scope.
func (f *Facade) filterHistory(method string, message rpc) rpc {
	if f.options.ThreadAvailable == nil || len(message.Error) != 0 {
		return message
	}
	if method != "thread/list" && method != "thread/loaded/list" {
		return message
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(message.Result, &result) != nil {
		return invalidHistory(message)
	}
	if method == "thread/list" {
		var items []json.RawMessage
		if json.Unmarshal(result["data"], &items) != nil {
			return invalidHistory(message)
		}
		selected := make([]json.RawMessage, 0, len(items))
		for _, item := range items {
			var thread struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(item, &thread) == nil && f.options.ThreadAvailable(thread.ID) {
				selected = append(selected, item)
			}
		}
		result["data"], _ = json.Marshal(selected)
	} else {
		var items []string
		if json.Unmarshal(result["data"], &items) != nil {
			return invalidHistory(message)
		}
		selected := make([]string, 0, len(items))
		for _, id := range items {
			if f.options.ThreadAvailable(id) {
				selected = append(selected, id)
			}
		}
		result["data"], _ = json.Marshal(selected)
	}
	message.Result, _ = json.Marshal(result)
	return message
}

func invalidHistory(message rpc) rpc {
	message.Result = nil
	message.Error = json.RawMessage(`{"code":-32603,"message":"INVALID_NATIVE_HISTORY_RESPONSE"}`)
	return message
}

func (f *Facade) allowHistoryRead(message rpc) bool {
	if f.options.ThreadAvailable == nil {
		return true
	}
	switch message.Method {
	case "thread/read", "thread/turns/list", "thread/items/list", "thread/name/set":
		var params struct {
			ThreadID string `json:"threadId"`
		}
		if json.Unmarshal(message.Params, &params) != nil || !f.options.ThreadAvailable(params.ThreadID) {
			f.reject(message.ID, "NATIVE_SESSION_NOT_MANAGED")
			return false
		}
	}
	return true
}
