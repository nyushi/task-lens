package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"time"
)

// TUIRPC implements Herdr TUI's protocol, not herdr-core JSON-RPC.
type TUIRPC struct{ Socket string }

func (r TUIRPC) Call(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	expected := ""
	switch method {
	case "pane.list":
		expected = "pane_list"
	case "pane.read":
		if params["source"] != "visible" || params["strip_ansi"] != true {
			return nil, fmt.Errorf("visible plain-text reads required")
		}
		expected = "pane_read"
	default:
		return nil, fmt.Errorf("TUI operation prohibited")
	}
	c, e := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", r.Socket)
	if e != nil {
		return nil, e
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(25 * time.Second))
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	if e = json.NewEncoder(c).Encode(map[string]any{"id": "tasklens-read", "method": method, "params": params}); e != nil {
		return nil, e
	}
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 4096), maxRPC)
	if !sc.Scan() {
		return nil, fmt.Errorf("TUI response unavailable: %v", sc.Err())
	}
	var response struct {
		ID     string          `json:"id"`
		Result map[string]any  `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if e = json.Unmarshal(sc.Bytes(), &response); e != nil {
		return nil, e
	}
	if response.ID != "tasklens-read" || (len(response.Error) > 0 && string(response.Error) != "null") || response.Result["type"] != expected {
		return nil, fmt.Errorf("unexpected TUI response; check server version and socket")
	}
	return response.Result, nil
}

type HerdrTUIInput struct {
	rpc   TUIRPC
	panes map[string]bool
}

func NewHerdrTUI(socket string, panes []string) (*HerdrTUIInput, error) {
	if socket == "" || len(panes) == 0 {
		return nil, fmt.Errorf("TUI socket and explicit panes required")
	}
	h := &HerdrTUIInput{TUIRPC{socket}, map[string]bool{}}
	for _, id := range panes {
		if id == "" {
			return nil, fmt.Errorf("empty pane ID")
		}
		h.panes[id] = true
	}
	return h, nil
}
func (h *HerdrTUIInput) Close() error { return nil }
func (h *HerdrTUIInput) Read(ctx context.Context) (Batch, error) {
	list, e := h.rpc.Call(ctx, "pane.list", map[string]any{})
	if e != nil {
		return nil, e
	}
	panes, ok := list["panes"].([]any)
	if !ok {
		return nil, fmt.Errorf("invalid TUI panes")
	}
	obs := []any{}
	for _, p := range panes {
		pane, ok := p.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid TUI pane")
		}
		id, _ := pane["pane_id"].(string)
		if !h.panes[id] {
			continue
		}
		result, e := h.rpc.Call(ctx, "pane.read", map[string]any{"pane_id": id, "source": "visible", "strip_ansi": true})
		if e != nil {
			return nil, e
		}
		read, ok := result["read"].(map[string]any)
		if !ok || read["pane_id"] != id || read["source"] != "visible" {
			return nil, fmt.Errorf("invalid TUI read")
		}
		text, ok := read["text"].(string)
		if !ok {
			return nil, fmt.Errorf("invalid TUI text")
		}
		truncated, ok := read["truncated"].(bool)
		if !ok {
			return nil, fmt.Errorf("missing TUI coverage")
		}
		chars := []rune(text)
		if len(chars) > 12000 {
			text = string(chars[len(chars)-12000:])
			truncated = true
		}
		metadata := map[string]any{}
		for _, k := range []string{"pane_id", "terminal_id", "workspace_id", "tab_id", "label", "title", "agent", "agent_status", "revision"} {
			metadata[k] = pane[k]
		}
		label, _ := pane["label"].(string)
		if label == "" {
			label = id
		}
		obs = append(obs, map[string]any{"id": "herdr-tui:" + id, "label": label, "revision": read["revision"], "observedAt": float64(time.Now().Unix()), "content": text, "coverage": map[string]any{"complete": !truncated, "how": "visible", "why": "current viewport only; not conversation history"}, "metadata": metadata})
	}
	sort.Slice(obs, func(i, j int) bool {
		return obs[i].(map[string]any)["id"].(string) < obs[j].(map[string]any)["id"].(string)
	})
	return Batch{"connected": true, "observations": obs}, nil
}
