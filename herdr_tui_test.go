package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestTUIProtocol(t *testing.T) {
	dir, e := os.MkdirTemp("", "tui-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "herdr.sock")
	l, e := net.Listen("unix", path)
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 2; i++ {
			c, e := l.Accept()
			if e != nil {
				done <- e
				return
			}
			var req map[string]any
			e = json.NewDecoder(c).Decode(&req)
			if e != nil {
				c.Close()
				done <- e
				return
			}
			if req["id"] != "tasklens-read" || req["jsonrpc"] != nil {
				t.Error("core envelope used for TUI")
			}
			result := map[string]any{}
			switch req["method"] {
			case "pane.list":
				result = map[string]any{"type": "pane_list", "panes": []any{map[string]any{"pane_id": "p1", "label": "dummy", "agent_status": "working"}, map[string]any{"pane_id": "private"}}}
			case "pane.read":
				p := req["params"].(map[string]any)
				if p["pane_id"] != "p1" || p["source"] != "visible" || p["strip_ansi"] != true || p["lines"] != nil {
					t.Error("unexpected read", p)
				}
				result = map[string]any{"type": "pane_read", "read": map[string]any{"pane_id": "p1", "source": "visible", "text": "synthetic only", "revision": 3, "truncated": false}}
			default:
				t.Error("unexpected method")
			}
			e = json.NewEncoder(c).Encode(map[string]any{"id": req["id"], "result": result})
			c.Close()
			if e != nil {
				done <- e
				return
			}
		}
		done <- nil
	}()
	h, e := NewHerdrTUI(path, []string{"p1"})
	if e != nil {
		t.Fatal(e)
	}
	b, e := h.Read(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	obs := b["observations"].([]any)
	if len(obs) != 1 || obs[0].(map[string]any)["content"] != "synthetic only" {
		t.Fatal(b)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	for _, method := range []string{"pane.send_text", "terminal.read", "events.subscribe"} {
		if _, e = h.rpc.Call(context.Background(), method, nil); e == nil {
			t.Fatal("unexpected operation allowed")
		}
	}
	if _, e = NewHerdrTUI(path, nil); e == nil {
		t.Fatal("implicit pane access")
	}
}
