package codexfrontend

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestPickerConnectionRequestIsolationAndLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	f := &Facade{ctx: ctx, cancel: cancel, remoteToken: "synthetic-local", upstream: writer, initializedResult: json.RawMessage(`{"userAgent":"fixture"}`)}
	server := httptest.NewServer(http.HandlerFunc(f.connect))
	defer server.Close()
	dial := func() *websocket.Conn {
		t.Helper()
		c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer synthetic-local"}}})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	main, picker := dial(), dial()
	defer main.CloseNow()
	defer picker.CloseNow()
	write := func(c *websocket.Conn, method string) {
		t.Helper()
		body, _ := json.Marshal(rpc{ID: json.RawMessage(`1`), Method: method, Params: json.RawMessage(`{}`)})
		if err := c.Write(ctx, websocket.MessageText, body); err != nil {
			t.Fatal(err)
		}
	}
	read := func(c *websocket.Conn) rpc {
		t.Helper()
		_, body, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var result rpc
		if json.Unmarshal(body, &result) != nil {
			t.Fatal("invalid response")
		}
		return result
	}
	write(picker, "initialize")
	if got := read(picker); string(got.ID) != "1" || len(got.Error) != 0 {
		t.Fatal("picker handshake failed")
	}
	requests := make(chan rpc, 2)
	go func() {
		decoder := json.NewDecoder(reader)
		for i := 0; i < 2; i++ {
			var request rpc
			if decoder.Decode(&request) != nil {
				return
			}
			requests <- request
		}
	}()
	write(main, "thread/list")
	a := <-requests
	write(picker, "thread/read")
	b := <-requests
	if string(a.ID) == string(b.ID) {
		t.Fatal("request ID collision")
	}
	// Deliberately return responses out of order.
	f.send(rpc{ID: b.ID, Result: json.RawMessage(`{"origin":"picker"}`)})
	f.send(rpc{ID: a.ID, Result: json.RawMessage(`{"origin":"main"}`)})
	if got := read(main); string(got.ID) != "1" || !strings.Contains(string(got.Result), "main") {
		t.Fatal("main response crossover")
	}
	if got := read(picker); string(got.ID) != "1" || !strings.Contains(string(got.Result), "picker") {
		t.Fatal("picker response crossover")
	}
	write(picker, "turn/start")
	if got := read(picker); !strings.Contains(string(got.Error), "PICKER_METHOD_DENIED") {
		t.Fatal("picker execution admitted")
	}
	_ = picker.Close(websocket.StatusNormalClosure, "picker cancelled")
	f.send(rpc{Method: "thread/status/changed", Params: json.RawMessage(`{}`)})
	if got := read(main); got.Method != "thread/status/changed" {
		t.Fatal("main lost after picker close")
	}
	if ctx.Err() != nil {
		t.Fatal("picker cancelled main")
	}
	_ = main.Close(websocket.StatusNormalClosure, "finished")
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("main disconnect did not stop session")
	}
}
