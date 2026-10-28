package knowledgerouter

import (
	"bytes"
	"io"
	"net/http"

	"github.com/ivo-lopes/ivoai/internal/externalmcp"
)

func (r *Router) copyPolicyResponse(w http.ResponseWriter, request *http.Request, rpc rpcRequest, kind string, response upstreamResponse) {
	policy := r.policy()
	if policy == nil {
		copyMCPResponse(w, response)
		return
	}
	if rpc.Method == "tools/call" && !policy(request.Context(), kind, rpc.Params.Name) {
		writeRPCError(w, rpc.ID, -32024, "MCP_DENIED: admitted turn capability expired")
		return
	}
	if rpc.Method == "tools/list" && response.status == http.StatusOK {
		reply := &http.Response{Header: response.header.Clone(), Body: io.NopCloser(bytes.NewReader(response.body)), ContentLength: int64(len(response.body))}
		if err := externalmcp.FilterInventory(reply, func(name string) bool { return policy(request.Context(), kind, name) }); err != nil {
			writeRPCError(w, rpc.ID, -32024, "MCP_DENIED: inventory projection unavailable")
			return
		}
		body, err := io.ReadAll(reply.Body)
		reply.Body.Close()
		if err != nil {
			writeRPCError(w, rpc.ID, -32024, "MCP_DENIED: inventory projection unavailable")
			return
		}
		response.body, response.header = body, reply.Header
	}
	copyMCPResponse(w, response)
}
