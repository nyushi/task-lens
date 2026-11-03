package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"
)

const maxRPC = 4 * 1024 * 1024

type RPC struct{ Socket string }

func validateRPC(method string, params map[string]any) error {
	switch method {
	case "rpc.discover", "terminal.list", "events.subscribe":
		return nil
	case "terminal.read":
		if params["lines"] == 0 {
			return nil
		}
		return fmt.Errorf("screen-only reads required")
	default:
		return fmt.Errorf("write operations prohibited")
	}
}
func (r RPC) open(ctx context.Context, method string, params map[string]any) (net.Conn, *bufio.Scanner, error) {
	if err := validateRPC(method, params); err != nil {
		return nil, nil, err
	}
	c, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", r.Socket)
	if err != nil {
		return nil, nil, err
	}
	_ = c.SetDeadline(time.Now().Add(25 * time.Second))
	if err = json.NewEncoder(c).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params}); err != nil {
		c.Close()
		return nil, nil, err
	}
	scanner := bufio.NewScanner(c)
	scanner.Buffer(make([]byte, 4096), maxRPC)
	return c, scanner, nil
}
func (r RPC) Call(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	c, sc, err := r.open(ctx, method, params)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	if !sc.Scan() {
		if sc.Err() != nil {
			return nil, sc.Err()
		}
		return nil, fmt.Errorf("empty RPC response")
	}
	var response struct {
		Result map[string]any  `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err = json.Unmarshal(sc.Bytes(), &response); err != nil {
		return nil, err
	}
	if len(response.Error) > 0 && string(response.Error) != "null" {
		return nil, fmt.Errorf("Herdr RPC error")
	}
	if response.Result == nil {
		return nil, fmt.Errorf("missing result")
	}
	return response.Result, nil
}
func (r RPC) Events(ctx context.Context, notice func(string, map[string]any)) error {
	c, sc, err := r.open(ctx, "events.subscribe", map[string]any{})
	if err != nil {
		return err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	first := true
	for sc.Scan() {
		var msg map[string]any
		if err = json.Unmarshal(sc.Bytes(), &msg); err != nil {
			return err
		}
		if first {
			if _, ok := msg["result"]; !ok {
				return fmt.Errorf("subscription failed")
			}
			first = false
			_ = c.SetDeadline(time.Time{})
			notice("snapshot", nil)
		} else {
			method, _ := msg["method"].(string)
			params, _ := msg["params"].(map[string]any)
			notice(method, params)
		}
	}
	if sc.Err() != nil {
		return sc.Err()
	}
	return fmt.Errorf("event stream closed")
}

type HerdrInput struct {
	rpc        RPC
	allowed    map[string]bool
	mu         sync.Mutex
	readMu     sync.Mutex
	epoch      uint64
	revisions  map[string]uint64
	digests    map[string]string
	subscribed bool
	cancel     context.CancelFunc
	done       chan struct{}
}

func NewHerdr(ctx context.Context, socket string, terminals []string) (*HerdrInput, error) {
	if socket == "" || len(terminals) == 0 {
		return nil, fmt.Errorf("socket and explicit terminals required")
	}
	ctx, cancel := context.WithCancel(ctx)
	h := &HerdrInput{rpc: RPC{socket}, allowed: map[string]bool{}, revisions: map[string]uint64{}, digests: map[string]string{}, cancel: cancel, done: make(chan struct{})}
	for _, id := range terminals {
		if id == "" {
			cancel()
			return nil, fmt.Errorf("empty terminal")
		}
		h.allowed[id] = true
	}
	go func() {
		defer close(h.done)
		for ctx.Err() == nil {
			_ = h.rpc.Events(ctx, h.notice)
			h.mu.Lock()
			h.epoch++
			h.subscribed = false
			h.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
		}
	}()
	return h, nil
}
func (h *HerdrInput) notice(method string, p map[string]any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch method {
	case "snapshot", "missed":
		h.epoch++
		h.subscribed = true
	case "event":
		id, _ := p["terminal"].(string)
		kind, _ := p["type"].(string)
		if h.allowed[id] && kind != "size" && kind != "foreground" {
			h.epoch++
		}
	}
}
func (h *HerdrInput) Read(ctx context.Context) (Batch, error) {
	h.readMu.Lock()
	defer h.readMu.Unlock()
	list, err := h.rpc.Call(ctx, "terminal.list", map[string]any{"all": true})
	if err != nil {
		return nil, err
	}
	terms, ok := list["terminals"].([]any)
	if !ok {
		return nil, fmt.Errorf("invalid terminal list")
	}
	obs := []any{}
	listed := map[string]bool{}
	for _, v := range terms {
		t, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid terminal list entry")
		}
		id, _ := t["address"].(string)
		if id == "" || listed[id] {
			return nil, fmt.Errorf("invalid terminal identity")
		}
		listed[id] = true
		if !h.allowed[id] {
			continue
		}
		read, err := h.rpc.Call(ctx, "terminal.read", map[string]any{"terminal": id, "lines": 0})
		if err != nil {
			return nil, err
		}
		text, ok := read["text"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid terminal text")
		}
		runes := []rune(text)
		truncated := len(runes) > 12000
		if truncated {
			text = string(runes[len(runes)-12000:])
		}
		facts := map[string]any{}
		for _, k := range []string{"address", "name", "title", "state", "status", "turn", "created_ms"} {
			facts[k] = t[k]
		}
		coverage := map[string]any{"complete": read["complete"], "how": read["how"], "why": read["why"]}
		if truncated {
			coverage["complete"] = false
			coverage["why"] = "local text limit"
		}
		digest := versionOf(Batch{"facts": facts, "text": text, "coverage": coverage})
		h.mu.Lock()
		if h.digests[id] != digest {
			h.revisions[id]++
			h.digests[id] = digest
		}
		revision, epoch := h.revisions[id], h.epoch
		h.mu.Unlock()
		obs = append(obs, map[string]any{"id": "herdr:" + id, "label": t["name"], "revision": revision, "epoch": epoch, "observedAt": float64(time.Now().Unix()), "content": text, "coverage": coverage, "metadata": facts})
	}
	sort.Slice(obs, func(i, j int) bool {
		return obs[i].(map[string]any)["id"].(string) < obs[j].(map[string]any)["id"].(string)
	})
	h.mu.Lock()
	epoch, subscribed := h.epoch, h.subscribed
	h.mu.Unlock()
	return Batch{"source_snapshots": sourceSnapshot("default", obs), "connected": subscribed, "epoch": epoch, "observations": obs}, nil
}
func (h *HerdrInput) Close() error { h.cancel(); <-h.done; return nil }
