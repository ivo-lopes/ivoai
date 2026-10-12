package externalmcp

import (
	"bytes"
	"errors"
	"io"
	"net/http"
)

// localTransport reuses gateway authentication, task admission, tool filtering
// and output bounds without creating another listener or forwarding to a daemon.
// Only trusted native handlers accepting JSON responses may use this boundary.
type localTransport struct{ handler http.Handler }

func (t localTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w := &localResponse{header: make(http.Header)}
	t.handler.ServeHTTP(w, r)
	if w.overflow {
		return nil, errors.New("native capability response exceeded budget")
	}
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{StatusCode: status, Header: w.header, Body: io.NopCloser(bytes.NewReader(w.body.Bytes())), ContentLength: int64(w.body.Len()), Request: r}, nil
}

type localResponse struct {
	header   http.Header
	status   int
	body     bytes.Buffer
	overflow bool
}

func (w *localResponse) Header() http.Header { return w.header }
func (w *localResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *localResponse) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if len(p) > 1024*1024-w.body.Len() {
		w.overflow = true
		return 0, errors.New("native capability response exceeded budget")
	}
	return w.body.Write(p)
}
