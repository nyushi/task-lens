package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixtureBatch(t *testing.T, declarations map[string][]Ownership, unknown ...string) Batch {
	t.Helper()
	r := emptyResult()
	for id := range declarations {
		r.Tasks[id] = Task{ID: id, Purpose: str(id), State: "doing", Attention: "none", Tree: []Node{}}
		r.Roots = append(r.Roots, id)
	}
	for _, id := range unknown {
		r.Tasks[id] = Task{ID: id, Purpose: str(id), State: "doing", Attention: "none", Tree: []Node{}}
		r.Roots = append(r.Roots, id)
	}
	raw, _ := json.Marshal(r)
	var f map[string]any
	_ = json.Unmarshal(raw, &f)
	raw, _ = json.Marshal(declarations)
	var owners any
	_ = json.Unmarshal(raw, &owners)
	f["owners"] = owners
	b := Batch{"connected": true, "observations": []any{}, "fixture": f}
	scopes := map[string]map[string]bool{}
	for _, group := range declarations {
		for _, o := range group {
			if o.Scope == "" {
				o.Scope = "default"
			}
			if scopes[o.Scope] == nil {
				scopes[o.Scope] = map[string]bool{}
			}
			scopes[o.Scope][o.Source] = true
		}
	}
	snapshots := []any{}
	for scope, ids := range scopes {
		list := []any{}
		for id := range ids {
			list = append(list, id)
		}
		snapshots = append(snapshots, map[string]any{"scope": scope, "complete": true, "source_ids": list})
	}
	b["source_snapshots"] = snapshots
	return b
}
func fixtureTick(t *testing.T, in Input, s *Store) {
	t.Helper()
	if _, e := Tick(context.Background(), in, FixtureTransform{}, s); e != nil {
		t.Fatal(e)
	}
}
func TestFixtureFileClosePaneExpiry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.json")
	write := func(b Batch) {
		raw, _ := json.Marshal(b)
		if e := os.WriteFile(path, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	b := fixtureBatch(t, map[string][]Ownership{"a": {{"pane-a", "one"}}, "b": {{"pane-b", "two"}}}, "unknown")
	// Preserve a nested tree without passing through an observation converter.
	f := b["fixture"].(map[string]any)
	tasks := f["tasks"].(map[string]any)
	tasks["a"].(map[string]any)["tree"] = []any{map[string]any{"id": "n", "state": "doing", "attention": "none", "current": true, "sourceRefs": []any{}, "content": map[string]any{"kind": "text", "text": "synthetic tree"}}}
	write(b)
	in := &FileInput{Path: path, Fixture: true}
	s := NewStore()
	s.cleanup = CleanupOptions{MissingAfterSeconds: 1}
	fixtureTick(t, in, s)
	if len(s.Snapshot().Tasks["a"].Tree) != 1 {
		t.Fatal("tree lost")
	}
	closed := fixtureBatch(t, map[string][]Ownership{"b": {{"pane-b", "two"}}}, "unknown")
	closed["source_snapshots"] = append(closed["source_snapshots"].([]any), map[string]any{"scope": "one", "complete": true, "source_ids": []any{}})
	write(closed)
	fixtureTick(t, in, s)
	if len(s.Snapshot().Tasks) != 3 {
		t.Fatal("deleted before grace")
	}
	time.Sleep(1100 * time.Millisecond)
	fixtureTick(t, in, s)
	if len(s.Snapshot().Tasks) != 2 || s.Snapshot().Tasks["b"].ID == "" || s.Snapshot().Tasks["unknown"].ID == "" {
		t.Fatal("wrong cards deleted")
	}
	fixtureTick(t, in, s)
	if len(s.Snapshot().Tasks) != 2 {
		t.Fatal("resurrected")
	}
}
func TestFixtureSharedOwnersAndIncomplete(t *testing.T) {
	b := fixtureBatch(t, map[string][]Ownership{"shared": {{"p1", "one"}, {"p2", "two"}}, "a": {{"p1", "one"}}}, "unknown")
	s := NewStore()
	s.cleanup = CleanupOptions{MissingAfterSeconds: 10}
	in := &StaticInput{b}
	fixtureTick(t, in, s)
	closed := fixtureBatch(t, map[string][]Ownership{})
	closed["source_snapshots"] = []any{map[string]any{"scope": "one", "complete": true, "source_ids": []any{}}}
	now := time.Now()
	s.Reconcile(closed, now)
	s.Reconcile(closed, now.Add(11*time.Second))
	if s.Snapshot().Tasks["shared"].ID == "" || s.Snapshot().Tasks["unknown"].ID == "" || s.Snapshot().Tasks["a"].ID != "" {
		t.Fatal("shared/unknown scope protection")
	}
	closed["source_snapshots"] = append(closed["source_snapshots"].([]any), map[string]any{"scope": "two", "complete": true, "source_ids": []any{}})
	s.Reconcile(closed, now)
	if _, e := Tick(context.Background(), failedInput{}, FixtureTransform{}, s); e == nil {
		t.Fatal("expected failure")
	}
	s.Reconcile(closed, now.Add(time.Hour))
	if s.Snapshot().Tasks["shared"].ID == "" {
		t.Fatal("failed cycle counted")
	}
	partial := clone(closed)
	partial["source_snapshots"].([]any)[0].(map[string]any)["complete"] = false
	s.UpdatePresence(partial)
	s.Reconcile(closed, now.Add(2*time.Hour))
	if s.Snapshot().Tasks["shared"].ID == "" {
		t.Fatal("partial cycle counted")
	}
	s.Reconcile(closed, now.Add(2*time.Hour+11*time.Second))
	if s.Snapshot().Tasks["shared"].ID != "" {
		t.Fatal("all owners absent but retained")
	}
	// Empty startup and older ownerless fixtures remain valid.
	fresh := NewStore()
	fresh.cleanup = s.cleanup
	fixtureTick(t, &StaticInput{closed}, fresh)
	if len(fresh.Snapshot().Tasks) != 0 {
		t.Fatal("startup")
	}
}
func TestFixtureMembershipValidation(t *testing.T) {
	b := fixtureBatch(t, map[string][]Ownership{"a": {{"p1", "one"}}})
	if !completeMembership(b)["one"]["p1"] {
		t.Fatal("empty observations rejected")
	}
	delete(b, "observations")
	if !completeMembership(b)["one"]["p1"] {
		t.Fatal("omitted observations rejected")
	}
	bad := clone(b)
	bad["source_snapshots"].([]any)[0].(map[string]any)["source_ids"] = []any{"p2"}
	if completeMembership(bad)["one"] != nil {
		t.Fatal("mismatch accepted")
	}
	bad = clone(b)
	bad["observations"] = []any{map[string]any{"id": "p1", "scope": "other", "content": "synthetic"}}
	if len(completeMembership(bad)) != 0 {
		t.Fatal("overlapping scope accepted")
	}
	bad = clone(b)
	bad["fixture"].(map[string]any)["owners"].(map[string]any)["missing"] = []any{map[string]any{"source_id": "p1", "scope": "one"}}
	if _, _, e := fixtureResult(bad); e == nil {
		t.Fatal("invalid owner task accepted")
	}
}

func TestFixtureSurvivingTaskProtectsReference(t *testing.T) {
	b := fixtureBatch(t, map[string][]Ownership{"a": {{"p1", "one"}}}, "unknown")
	f := b["fixture"].(map[string]any)
	f["tasks"].(map[string]any)["unknown"].(map[string]any)["tree"] = []any{map[string]any{"id": "ref", "state": "doing", "attention": "none", "current": false, "sourceRefs": []any{}, "content": map[string]any{"kind": "task", "taskId": "a"}}}
	s := NewStore()
	s.cleanup = CleanupOptions{RemoveMissing: true}
	fixtureTick(t, &StaticInput{b}, s)
	closed := fixtureBatch(t, map[string][]Ownership{})
	closed["source_snapshots"] = []any{map[string]any{"scope": "one", "complete": true, "source_ids": []any{}}}
	fixtureTick(t, &StaticInput{closed}, s)
	if len(s.Snapshot().Tasks) != 2 {
		t.Fatal("shared referenced task removed")
	}
	// A current ownerless declaration explicitly clears prior ownership.
	b = fixtureBatch(t, map[string][]Ownership{}, "a")
	fixtureTick(t, &StaticInput{b}, s)
	if len(s.owners["a"]) != 0 {
		t.Fatal("old ownership inferred for ownerless task")
	}
}
