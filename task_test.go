package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func demoResult(t *testing.T) Result {
	t.Helper()
	raw, err := assets.ReadFile("fixtures/demo-tasks.json")
	if err != nil {
		t.Fatal(err)
	}
	var r Result
	if err = json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	return r
}
func demoBatch(t *testing.T) Batch {
	t.Helper()
	r := demoResult(t)
	raw, _ := json.Marshal(r)
	var v any
	_ = json.Unmarshal(raw, &v)
	return Batch{"connected": true, "fixture": v}
}
func TestResultValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Result)
	}{
		{"valid", func(r *Result) {}},
		{"cycle", func(r *Result) {
			v := r.Tasks["account-policy"]
			v.Tree[0].Content = Content{Kind: "task", TaskID: "example-1"}
			r.Tasks[v.ID] = v
		}},
		{"missing source", func(r *Result) {
			v := r.Tasks["example-1"]
			v.Tree[0].SourceRefs = []string{"missing"}
			r.Tasks[v.ID] = v
		}},
		{"duplicate node", func(r *Result) { v := r.Tasks["example-1"]; v.Tree = append(v.Tree, v.Tree[0]); r.Tasks[v.ID] = v }},
		{"invalid state", func(r *Result) { v := r.Tasks["example-1"]; v.State = "bad"; r.Tasks[v.ID] = v }},
		{"missing current", func(r *Result) { v := r.Tasks["example-1"]; v.Tree[0].Current = nil; r.Tasks[v.ID] = v }},
		{"missing task", func(r *Result) { delete(r.Tasks, "account-policy") }},
		{"duplicate root", func(r *Result) { r.Roots = append(r.Roots, r.Roots[0]) }},
		{"missing tree", func(r *Result) { v := r.Tasks["example-1"]; v.Tree = nil; r.Tasks[v.ID] = v }},
		{"deep tree", func(r *Result) {
			n := Node{ID: "leaf", State: "todo", Attention: "none", Current: boolean(false), SourceRefs: []string{}, Content: Content{Kind: "text", Text: str("leaf")}}
			for i := 0; i < 15; i++ {
				n = Node{ID: fmt.Sprint(i), State: "todo", Attention: "none", Current: boolean(false), SourceRefs: []string{}, Content: Content{Kind: "text", Text: str("node")}, Children: []Node{n}}
			}
			v := r.Tasks["example-1"]
			v.Tree = []Node{n}
			r.Tasks[v.ID] = v
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := demoResult(t)
			tc.mutate(&r)
			e := Validate(r)
			if (e == nil) != (tc.name == "valid") {
				t.Fatalf("unexpected validation: %v", e)
			}
		})
	}
}
func TestExamples(t *testing.T) {
	for _, path := range []string{"examples/output.json", "fixtures/codex-dummy-result.json"} {
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		var r Result
		if e = decodeJSON(raw, &r); e != nil {
			t.Fatal(e)
		}
		if e = Validate(r); e != nil {
			t.Fatal(e)
		}
	}
}
func TestStore(t *testing.T) {
	s := NewStore()
	s.Observe("v1", true)
	ok, e := s.Commit(0, "v1", demoResult(t))
	if !ok || e != nil {
		t.Fatal(e)
	}
	before := s.Snapshot()
	for _, tc := range []struct {
		rev     uint64
		version string
	}{{0, "v1"}, {1, "old"}} {
		if ok, _ = s.Commit(tc.rev, tc.version, demoResult(t)); ok {
			t.Fatal("stale result accepted")
		}
	}
	invalid := demoResult(t)
	invalid.Roots = []string{"missing"}
	if _, e = s.Commit(1, "v1", invalid); e == nil {
		t.Fatal("invalid result accepted")
	}
	if !reflect.DeepEqual(before, s.Snapshot()) {
		t.Fatal("invalid commit mutated store")
	}
	empty := Result{Tasks: map[string]Task{}, Roots: []string{}, Sources: map[string]Source{}}
	if ok, e = s.Commit(1, "v1", empty); !ok || e != nil {
		t.Fatal(e)
	}
	if !s.Snapshot().Tasks["example-1"].Stale {
		t.Fatal("missing task lost")
	}
}

type transformFunc func(context.Context, Batch, Snapshot) (Result, error)

func (f transformFunc) Convert(c context.Context, b Batch, s Snapshot) (Result, error) {
	return f(c, b, s)
}
func TestPipeline(t *testing.T) {
	ctx := context.Background()
	in := &StaticInput{demoBatch(t)}
	s := NewStore()
	if ok, e := Tick(ctx, in, FixtureTransform{}, s); !ok || e != nil {
		t.Fatal(e)
	}
	if ok, e := Tick(ctx, in, FixtureTransform{}, s); ok || e != nil {
		t.Fatal("duplicate conversion")
	}
	in.Batch["change"] = true
	tr := transformFunc(func(_ context.Context, b Batch, _ Snapshot) (Result, error) {
		in.Batch["change"] = false
		return demoResult(t), nil
	})
	if ok, e := Tick(ctx, in, tr, s); ok || e == nil {
		t.Fatal("racing source accepted")
	}
	if s.Snapshot().Revision != 1 {
		t.Fatal("revision changed")
	}
	tr = func(context.Context, Batch, Snapshot) (Result, error) { return Result{}, fmt.Errorf("fixture failure") }
	if _, e := Tick(ctx, in, tr, s); e == nil {
		t.Fatal("error lost")
	}
	if len(s.Snapshot().Tasks) != 6 {
		t.Fatal("old tasks lost")
	}
}
func TestObservedTimeDoesNotTriggerConversion(t *testing.T) {
	a := Batch{"observedAt": 1, "content": "same"}
	b := Batch{"observedAt": 2, "content": "same"}
	if versionOf(a) != versionOf(b) {
		t.Fatal("volatile time included")
	}
}
func TestHelperProcess(t *testing.T) {
	mode := ""
	for i, a := range os.Args {
		if a == "--" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
			break
		}
	}
	if mode == "" {
		return
	}
	switch mode {
	case "valid":
		var e struct {
			Input struct {
				Fixture Result `json:"fixture"`
			} `json:"input"`
		}
		if err := json.NewDecoder(os.Stdin).Decode(&e); err != nil {
			os.Exit(3)
		}
		_ = json.NewEncoder(os.Stdout).Encode(e.Input.Fixture)
	case "fail":
		fmt.Fprintln(os.Stderr, "synthetic failure")
		os.Exit(2)
	case "slow":
		time.Sleep(10 * time.Second)
	case "large":
		fmt.Print(strings.Repeat("x", 5000))
	case "invalid":
		fmt.Print("not json")
	}
	os.Exit(0)
}
func commandFixture(mode string) CommandOptions {
	return CommandOptions{Command: os.Args[0], Argv: []string{"-test.run=TestHelperProcess", "--", mode}, Timeout: 2, MaxOutput: 2000000}
}
func TestCommandTransform(t *testing.T) {
	for _, mode := range []string{"valid", "fail", "slow", "large", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			o := commandFixture(mode)
			if mode == "slow" {
				o.Timeout = .05
			}
			if mode == "large" {
				o.MaxOutput = 100
			}
			tr := &CommandTransform{Options: o, Prompt: "synthetic"}
			r, e := tr.Convert(context.Background(), demoBatch(t), NewStore().Snapshot())
			if mode == "valid" {
				if e != nil {
					t.Fatal(e)
				}
				if e = Validate(r); e != nil {
					t.Fatal(e)
				}
			} else if e == nil {
				t.Fatal("failure accepted")
			}
			if mode == "fail" && !strings.Contains(tr.LastStderr, "synthetic failure") {
				t.Fatal("stderr missing")
			}
		})
	}
}
func TestWeb(t *testing.T) {
	h := webHandler(NewStore(), 8765, "token", "demo")
	for _, tc := range []struct {
		method, path, host string
		code               int
	}{{"GET", "/token/", "127.0.0.1:8765", 200}, {"GET", "/token/state", "localhost:8765", 200}, {"POST", "/token/state", "localhost:8765", 405}, {"GET", "/token/", "evil.example", 403}, {"GET", "/wrong/", "localhost:8765", 404}} {
		r := httptest.NewRequest(tc.method, "http://"+tc.host+tc.path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("%s: %d", tc.path, w.Code)
		}
		if tc.code == 200 && w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("cache header missing")
		}
		if tc.path == "/token/" && tc.code == 200 && !strings.Contains(w.Body.String(), "treeView") {
			t.Fatal("embedded UI missing")
		}
	}
}
