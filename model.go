package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"unicode/utf8"
)

type Content struct {
	Kind   string  `json:"kind"`
	Text   *string `json:"text,omitempty"`
	TaskID string  `json:"taskId,omitempty"`
}
type Node struct {
	ID         string   `json:"id"`
	State      string   `json:"state"`
	Attention  string   `json:"attention"`
	Current    *bool    `json:"current"`
	SourceRefs []string `json:"sourceRefs"`
	Content    Content  `json:"content"`
	Children   []Node   `json:"children,omitempty"`
	Detail     string   `json:"detail,omitempty"`
}
type Task struct {
	ID        string  `json:"id"`
	Purpose   *string `json:"purpose"`
	State     string  `json:"state"`
	Attention string  `json:"attention"`
	Tree      []Node  `json:"tree"`
	UpdatedAt float64 `json:"updatedAt,omitempty"`
	Stale     bool    `json:"stale,omitempty"`
}
type Source = map[string]any
type Result struct {
	Tasks   map[string]Task   `json:"tasks"`
	Roots   []string          `json:"roots"`
	Sources map[string]Source `json:"sources"`
}
type Snapshot struct {
	Usage *Usage `json:"usage,omitempty"`
	Result
	Revision  uint64  `json:"revision"`
	Connected bool    `json:"connected"`
	Error     *string `json:"error"`
	Mode      string  `json:"mode,omitempty"`
}

func str(s string) *string  { return &s }
func boolean(b bool) *bool  { return &b }
func stateOK(s string) bool { return s == "done" || s == "doing" || s == "todo" || s == "unknown" }
func attentionOK(s string) bool {
	return s == "decision" || s == "review" || s == "none" || s == "unknown"
}
func clone[T any](v T) T { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }
func Validate(r Result) error {
	if r.Tasks == nil || r.Roots == nil || r.Sources == nil || len(r.Tasks) > 200 {
		return fmt.Errorf("invalid result fields")
	}
	seenRoots := map[string]bool{}
	for _, id := range r.Roots {
		if _, ok := r.Tasks[id]; !ok || seenRoots[id] {
			return fmt.Errorf("invalid root %q", id)
		}
		seenRoots[id] = true
	}
	edges := map[string][]string{}
	count := 0
	for id, t := range r.Tasks {
		if id == "" || t.ID != id || t.Purpose == nil || utf8.RuneCountInString(*t.Purpose) > 2000 || !stateOK(t.State) || !attentionOK(t.Attention) || t.Tree == nil {
			return fmt.Errorf("invalid task %q", id)
		}
		ids := map[string]bool{}
		var walk func([]Node, int) error
		walk = func(nodes []Node, depth int) error {
			if depth > 12 {
				return fmt.Errorf("tree depth exceeded")
			}
			for _, n := range nodes {
				count++
				if count > 2000 || n.ID == "" || ids[n.ID] || !stateOK(n.State) || !attentionOK(n.Attention) || n.Current == nil || n.SourceRefs == nil {
					return fmt.Errorf("invalid node %q", n.ID)
				}
				ids[n.ID] = true
				for _, ref := range n.SourceRefs {
					if _, ok := r.Sources[ref]; !ok {
						return fmt.Errorf("missing source %q", ref)
					}
				}
				switch n.Content.Kind {
				case "text":
					if n.Content.Text == nil || utf8.RuneCountInString(*n.Content.Text) > 2000 {
						return fmt.Errorf("invalid text")
					}
				case "task":
					if _, ok := r.Tasks[n.Content.TaskID]; !ok {
						return fmt.Errorf("missing task")
					}
					edges[id] = append(edges[id], n.Content.TaskID)
				default:
					return fmt.Errorf("invalid content kind")
				}
				if err := walk(n.Children, depth+1); err != nil {
					return err
				}
			}
			return nil
		}
		if err := walk(t.Tree, 0); err != nil {
			return err
		}
	}
	visiting := map[string]bool{}
	heights := map[string]int{}
	var visit func(string) (int, error)
	visit = func(id string) (int, error) {
		if visiting[id] {
			return 0, fmt.Errorf("task cycle")
		}
		if h := heights[id]; h > 0 {
			return h, nil
		}
		visiting[id] = true
		h := 1
		for _, child := range edges[id] {
			c, e := visit(child)
			if e != nil {
				return 0, e
			}
			if c+1 > h {
				h = c + 1
			}
		}
		visiting[id] = false
		if h > 12 {
			return 0, fmt.Errorf("task depth exceeded")
		}
		heights[id] = h
		return h, nil
	}
	for id := range r.Tasks {
		if _, err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

type Store struct {
	mu       sync.Mutex
	snapshot Snapshot
	version  string
}

func NewStore() *Store {
	return &Store{snapshot: Snapshot{Result: Result{Tasks: map[string]Task{}, Roots: []string{}, Sources: map[string]Source{}}}}
}
func (s *Store) Snapshot() Snapshot { s.mu.Lock(); defer s.mu.Unlock(); return clone(s.snapshot) }
func (s *Store) Observe(version string, connected bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	unchanged := s.version == version && s.snapshot.Error == nil
	s.version = version
	s.snapshot.Connected = connected
	return unchanged
}
func (s *Store) Fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg := "入力または変換に失敗しました。前回の表示を維持しています。"
	s.snapshot.Error = &msg
}
func (s *Store) Commit(base uint64, version string, r Result) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if base != s.snapshot.Revision || version != s.version {
		return false, nil
	}
	r = clone(r)
	if err := Validate(r); err != nil {
		return false, err
	}
	for id, t := range s.snapshot.Tasks {
		if _, ok := r.Tasks[id]; !ok {
			t.Stale = true
			r.Tasks[id] = t
			for _, root := range s.snapshot.Roots {
				if root == id {
					r.Roots = append(r.Roots, id)
				}
			}
		}
	}
	for id, source := range s.snapshot.Sources {
		if _, ok := r.Sources[id]; !ok {
			r.Sources[id] = source
		}
	}
	if err := Validate(r); err != nil {
		return false, err
	}
	s.snapshot.Result = r
	s.snapshot.Revision++
	s.snapshot.Error = nil
	return true, nil
}
