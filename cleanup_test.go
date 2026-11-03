package main

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func membership(scope string, ids ...string) Batch {
	obs := []any{}
	for _, id := range ids {
		obs = append(obs, map[string]any{"id": id, "scope": scope, "content": "synthetic"})
	}
	return Batch{"connected": true, "observations": obs, "source_snapshots": sourceSnapshot(scope, obs)}
}
func ownedStore(t *testing.T, c CleanupOptions) *Store {
	s := NewStore()
	s.cleanup = c
	r := emptyResult()
	for _, id := range []string{"a", "b"} {
		r.Tasks[id] = Task{ID: id, Purpose: str(id), State: "doing", Attention: "none", Tree: []Node{}}
		r.Roots = append(r.Roots, id)
	}
	s.Observe("v", true)
	if _, e := s.Commit(0, "v", r); e != nil {
		t.Fatal(e)
	}
	s.TrackOwners(map[string]Ownership{"a": {"p1", "one"}, "b": {"p2", "two"}})
	return s
}
func TestCleanupScopesAndDefaults(t *testing.T) {
	now := time.Unix(1000, 0)
	s := ownedStore(t, CleanupOptions{})
	s.Reconcile(membership("one"), now)
	if len(s.Snapshot().Tasks) != 2 {
		t.Fatal("default deleted")
	}
	s = ownedStore(t, CleanupOptions{RemoveMissing: true})
	s.Reconcile(membership("one"), now)
	if len(s.Snapshot().Tasks) != 1 || s.Snapshot().Tasks["b"].ID != "b" {
		t.Fatal("deleted independent scope")
	}
	s = ownedStore(t, CleanupOptions{RemoveMissing: true})
	b := membership("one")
	b["source_snapshots"].([]any)[0].(map[string]any)["complete"] = false
	s.Reconcile(b, now)
	if len(s.Snapshot().Tasks) != 2 {
		t.Fatal("partial deleted")
	}
	b = membership("one")
	b["source_snapshots"].([]any)[0].(map[string]any)["source_ids"] = []any{"missing observation"}
	s.Reconcile(b, now)
	if len(s.Snapshot().Tasks) != 2 {
		t.Fatal("inconsistent list deleted")
	}
	// Startup has no previously owned cards; unknown fixture ownership is retained.
	s = NewStore()
	s.cleanup = CleanupOptions{RemoveMissing: true}
	s.Reconcile(membership("one"), now)
	if len(s.Snapshot().Tasks) != 0 {
		t.Fatal("startup")
	}
}
func TestCleanupGraceAndRecovery(t *testing.T) {
	start := time.Unix(1000, 0)
	s := ownedStore(t, CleanupOptions{RemoveMissing: true, MissingAfterSeconds: 60})
	absent := membership("one")
	s.Reconcile(absent, start)
	s.Reconcile(absent, start.Add(59*time.Second))
	if len(s.Snapshot().Tasks) != 2 {
		t.Fatal("delay ignored")
	}
	s.PauseCleanup()
	s.Reconcile(absent, start.Add(time.Hour))
	if len(s.Snapshot().Tasks) != 2 {
		t.Fatal("outage counted")
	}
	s.Reconcile(membership("one", "p1"), start.Add(time.Hour+30*time.Second))
	s.Reconcile(absent, start.Add(2*time.Hour))
	if len(s.Snapshot().Tasks) != 2 {
		t.Fatal("reappearance did not reset")
	}
	s.Reconcile(absent, start.Add(2*time.Hour+60*time.Second))
	if len(s.Snapshot().Tasks) != 1 {
		t.Fatal("expired source retained")
	}
}

type failedInput struct{}

func (failedInput) Read(context.Context) (Batch, error) { return nil, fmt.Errorf("synthetic failure") }
func (failedInput) Close() error                        { return nil }
func TestCleanupFailedReadAndReferences(t *testing.T) {
	s := ownedStore(t, CleanupOptions{RemoveMissing: true})
	if _, e := Tick(context.Background(), failedInput{}, ObservationsOnly{}, s); e == nil {
		t.Fatal("expected failure")
	}
	if len(s.Snapshot().Tasks) != 2 {
		t.Fatal("failure deleted")
	}
	s.mu.Lock()
	b := s.snapshot.Tasks["b"]
	b.Tree = []Node{{ID: "ref", State: "doing", Attention: "none", Current: boolean(false), SourceRefs: []string{}, Content: Content{Kind: "task", TaskID: "a"}}}
	s.snapshot.Tasks["b"] = b
	s.mu.Unlock()
	s.Reconcile(membership("one"), time.Now())
	if len(s.Snapshot().Tasks) != 2 {
		t.Fatal("dangling task reference")
	}
}
func TestIncrementalCleanupDoesNotResurrect(t *testing.T) {
	i, now := newTestIncremental(t, IncrementalOptions{})
	in := &StaticInput{membership("one", "p1")}
	s := NewStore()
	s.cleanup = CleanupOptions{RemoveMissing: true}
	tickIncremental(t, i, in, s)
	*now = now.Add(time.Minute)
	tickIncremental(t, i, in, s)
	if len(s.Snapshot().Tasks) != 1 {
		t.Fatal("no initial result")
	}
	updated := s.Snapshot().Tasks[namespace("p1")+"task"].UpdatedAt
	*now = now.Add(time.Minute)
	tickIncremental(t, i, in, s)
	if s.Snapshot().Tasks[namespace("p1")+"task"].UpdatedAt != updated {
		t.Fatal("poll refreshed timestamp")
	}
	in.Batch = membership("one")
	tickIncremental(t, i, in, s)
	tickIncremental(t, i, in, s)
	if len(s.Snapshot().Tasks) != 0 || len(i.sources) != 0 {
		t.Fatal("resurrected closed owner")
	}
	in.Batch = membership("one", "p1")
	tickIncremental(t, i, in, s)
	*now = now.Add(time.Minute)
	tickIncremental(t, i, in, s)
	if len(s.Snapshot().Tasks) != 1 {
		t.Fatal("reopened source failed")
	}
}
func TestObservationsKeepUnchangedTimestamp(t *testing.T) {
	b := membership("one", "p1")
	o := b["observations"].([]any)[0].(map[string]any)
	o["observedAt"] = float64(1000)
	r, e := (ObservationsOnly{}).Convert(context.Background(), b, NewStore().Snapshot())
	if e != nil {
		t.Fatal(e)
	}
	o["observedAt"] = float64(2000)
	next, e := (ObservationsOnly{}).Convert(context.Background(), b, Snapshot{Result: r})
	if e != nil || next.Tasks["observation:p1"].UpdatedAt != 1000 {
		t.Fatal("timestamp changed", e)
	}
}
func TestIncompleteCycleResetsGraceBeforeConversion(t *testing.T) {
	s := ownedStore(t, CleanupOptions{MissingAfterSeconds: 60})
	now := time.Unix(1000, 0)
	s.Reconcile(membership("one"), now)
	partial := membership("one")
	partial["source_snapshots"].([]any)[0].(map[string]any)["complete"] = false
	s.UpdatePresence(partial)
	s.Reconcile(membership("one"), now.Add(time.Hour))
	if len(s.Snapshot().Tasks) != 2 {
		t.Fatal("partial interval counted")
	}
}
