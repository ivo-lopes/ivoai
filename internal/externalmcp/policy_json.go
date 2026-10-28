package externalmcp

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

// ValidPolicyRequest prevents the admission parser and an upstream parser from
// seeing different RPC identities. Arguments remain untrusted data; duplicate
// keys, case aliases of RPC keys, trailing values and excessive depth fail
// closed. This is used at explicitly host-filtered capability boundaries.
func ValidPolicyRequest(raw []byte) bool {
	if len(raw) == 0 || len(raw) > 4<<20 || !utf8.Valid(raw) {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	nodes := 0
	var read func(int) bool
	read = func(depth int) bool {
		nodes++
		if depth > 32 || nodes > 65536 {
			return false
		}
		token, err := d.Token()
		if err != nil {
			return false
		}
		delim, isDelim := token.(json.Delim)
		if !isDelim {
			return true
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[name] {
					return false
				}
				seen[name] = true
				if depth == 0 {
					for _, reserved := range []string{"jsonrpc", "method", "id", "params"} {
						if strings.EqualFold(name, reserved) && name != reserved {
							return false
						}
					}
				}
				if depth == 1 && strings.EqualFold(name, "name") && name != "name" {
					return false
				}
				if !read(depth + 1) {
					return false
				}
			}
			token, err := d.Token()
			return err == nil && token == json.Delim('}')
		case '[':
			for d.More() {
				if !read(depth + 1) {
					return false
				}
			}
			token, err := d.Token()
			return err == nil && token == json.Delim(']')
		}
		return false
	}
	if !read(0) {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}
