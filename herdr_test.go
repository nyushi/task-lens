package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestRPCAllowlist(t *testing.T) {
	if validateRPC("terminal.send", nil) == nil {
		t.Fatal("write allowed")
	}
	if validateRPC("terminal.read", map[string]any{"lines": 100}) == nil {
		t.Fatal("scroll read allowed")
	}
	for _, method := range []string{"terminal.list", "events.subscribe", "rpc.discover"} {
		if e := validateRPC(method, nil); e != nil {
			t.Fatal(e)
		}
	}
}
func TestOfficialSchema(t *testing.T) {
	raw, e := os.ReadFile("evidence/hc.openrpc.json")
	if e != nil {
		t.Fatal(e)
	}
	var schema struct {
		Methods []struct {
			Name   string `json:"name"`
			Params []struct {
				Name     string `json:"name"`
				Required bool   `json:"required"`
			} `json:"params"`
		} `json:"methods"`
	}
	if e = json.Unmarshal(raw, &schema); e != nil {
		t.Fatal(e)
	}
	calls := map[string]map[string]bool{"terminal.list": {"all": true}, "terminal.read": {"terminal": true, "lines": true}, "events.subscribe": {}}
	for method, args := range calls {
		found := false
		for _, m := range schema.Methods {
			if m.Name != method {
				continue
			}
			found = true
			allowed := map[string]bool{}
			for _, p := range m.Params {
				allowed[p.Name] = true
				if p.Required && !args[p.Name] {
					t.Fatal("missing required param")
				}
			}
			for k := range args {
				if !allowed[k] {
					t.Fatal("invalid param")
				}
			}
		}
		if !found {
			t.Fatal("missing method")
		}
	}
}
func TestUnixRPCAndEvents(t *testing.T) {
	dir, e := os.MkdirTemp("", "tl-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "core.sock")
	listener, e := net.Listen("unix", path)
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	var wg sync.WaitGroup
	defer wg.Wait()
	go func() {
		for {
			c, e := listener.Accept()
			if e != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer c.Close()
				var req struct {
					Method string         `json:"method"`
					Params map[string]any `json:"params"`
				}
				if e := json.NewDecoder(bufio.NewReader(c)).Decode(&req); e != nil {
					return
				}
				enc := json.NewEncoder(c)
				switch req.Method {
				case "terminal.read":
					if req.Params["lines"] != float64(0) {
						return
					}
					_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"text": "synthetic", "how": "screen", "complete": true}})
				case "events.subscribe":
					_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{"seq": 1, "terminals": []any{}}})
					_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "missed", "params": map[string]any{"seq": 2}})
				}
			}()
		}
	}()
	rpc := RPC{path}
	r, e := rpc.Call(context.Background(), "terminal.read", map[string]any{"terminal": "t1", "lines": 0})
	if e != nil || r["text"] != "synthetic" {
		t.Fatal(r, e)
	}
	var methods []string
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e = rpc.Events(ctx, func(m string, _ map[string]any) { methods = append(methods, m) }); e == nil {
		t.Fatal("closed stream not reported")
	}
	if len(methods) != 2 || methods[0] != "snapshot" || methods[1] != "missed" {
		t.Fatal(methods)
	}
}
func TestHerdrEventInvalidation(t *testing.T) {
	h := &HerdrInput{allowed: map[string]bool{"t1": true}}
	h.notice("snapshot", nil)
	if h.epoch != 1 || !h.subscribed {
		t.Fatal("snapshot not observed")
	}
	h.notice("event", map[string]any{"terminal": "other", "type": "status"})
	h.notice("event", map[string]any{"terminal": "t1", "type": "size"})
	if h.epoch != 1 {
		t.Fatal("unrelated event")
	}
	h.notice("event", map[string]any{"terminal": "t1", "type": "status"})
	h.notice("missed", nil)
	if h.epoch != 3 {
		t.Fatal("inflight results not invalidated")
	}
}
