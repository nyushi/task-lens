package main

import (
	"fmt"
	"time"
)

type CleanupOptions struct {
	RemoveMissing       bool `json:"remove_missing"`
	MissingAfterSeconds int  `json:"missing_after_seconds"`
}
type Ownership struct{ Source, Scope string }

func scopeOf(o map[string]any) string {
	scope, _ := o["scope"].(string)
	if scope == "" {
		return "default"
	}
	return scope
}
func sourceSnapshot(scope string, obs []any) []any {
	ids := []any{}
	for _, v := range obs {
		ids = append(ids, v.(map[string]any)["id"])
	}
	return []any{map[string]any{"scope": scope, "complete": true, "source_ids": ids}}
}
func (s *Store) TrackOwners(owners map[string]Ownership) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, owner := range owners {
		if _, ok := s.snapshot.Tasks[id]; ok {
			s.owners[id] = owner
		}
	}
}
func (s *Store) PauseCleanup() { s.mu.Lock(); defer s.mu.Unlock(); s.missing = map[string]time.Time{} }

// Only an explicitly complete membership snapshot can establish disappearance.
// Unknown/failed scopes reset their timer; they can never expire another scope.
func (s *Store) Reconcile(b Batch, now time.Time) map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := map[string]bool{}
	if !s.cleanup.RemoveMissing && s.cleanup.MissingAfterSeconds == 0 {
		return removed
	}
	scopes := completeMembership(b)
	candidates := map[string]bool{}
	for id, owner := range s.owners {
		membership, complete := scopes[owner.Scope]
		complete = complete && membership != nil
		if !complete || membership[owner.Source] {
			delete(s.missing, id)
			continue
		}
		at, ok := s.missing[id]
		if !ok {
			at = now
			s.missing[id] = at
		}
		if s.cleanup.MissingAfterSeconds == 0 || now.Sub(at) >= time.Duration(s.cleanup.MissingAfterSeconds)*time.Second {
			candidates[id] = true
		}
	}
	// Keep referenced tasks if a surviving task still needs them.
	var refs func([]Node, func(string))
	refs = func(nodes []Node, visit func(string)) {
		for _, n := range nodes {
			if n.Content.Kind == "task" {
				visit(n.Content.TaskID)
			}
			refs(n.Children, visit)
		}
	}
	for changed := true; changed; {
		changed = false
		for id, t := range s.snapshot.Tasks {
			if candidates[id] {
				continue
			}
			refs(t.Tree, func(ref string) {
				if candidates[ref] {
					delete(candidates, ref)
					changed = true
				}
			})
		}
	}
	for id := range candidates {
		delete(s.snapshot.Tasks, id)
		delete(s.owners, id)
		delete(s.missing, id)
		removed[id] = true
	}
	if len(removed) > 0 {
		roots := []string{}
		for _, id := range s.snapshot.Roots {
			if !removed[id] {
				roots = append(roots, id)
			}
		}
		s.snapshot.Roots = roots
		used := map[string]bool{}
		var walk func([]Node)
		walk = func(nodes []Node) {
			for _, n := range nodes {
				for _, id := range n.SourceRefs {
					used[id] = true
				}
				walk(n.Children)
			}
		}
		for _, t := range s.snapshot.Tasks {
			walk(t.Tree)
		}
		for id := range s.snapshot.Sources {
			if !used[id] {
				delete(s.snapshot.Sources, id)
			}
		}
		s.snapshot.Revision++
	}
	return removed
}
func (c CleanupOptions) validate() error {
	if c.MissingAfterSeconds < 0 || c.MissingAfterSeconds > 31536000 {
		return fmt.Errorf("missing_after_seconds must be between 0 and 31536000")
	}
	return nil
}

func completeMembership(b Batch) map[string]map[string]bool {
	obs, e := observations(b)
	if e != nil || !connected(b) {
		return map[string]map[string]bool{}
	}
	scopes := map[string]map[string]bool{}
	seen := map[string]bool{}
	snapshots, _ := b["source_snapshots"].([]any)
	for _, raw := range snapshots {
		snap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		scope, _ := snap["scope"].(string)
		if scope == "" {
			continue
		}
		if seen[scope] {
			scopes[scope] = nil
			continue
		}
		seen[scope] = true
		if snap["complete"] != true {
			continue
		}
		ids, ok := snap["source_ids"].([]any)
		if !ok {
			continue
		}
		present := map[string]bool{}
		valid := true
		for _, v := range ids {
			id, ok := v.(string)
			if !ok || id == "" || present[id] || obs[id] == nil || scopeOf(obs[id]) != scope {
				valid = false
				break
			}
			present[id] = true
		}
		for id, o := range obs {
			if scopeOf(o) == scope && !present[id] {
				valid = false
			}
		}
		if valid {
			scopes[scope] = present
		}
	}

	return scopes
}

// Called even if transformation later fails: gaps must not count toward expiry.
func (s *Store) UpdatePresence(b Batch) {
	scopes := completeMembership(b)
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, owner := range s.owners {
		present := scopes[owner.Scope]
		if present == nil || present[owner.Source] {
			delete(s.missing, id)
		}
	}
}
