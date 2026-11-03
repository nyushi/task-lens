package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

type StaticInput struct{ Batch Batch }

func (i *StaticInput) Read(context.Context) (Batch, error) { return clone(i.Batch), nil }
func (i *StaticInput) Close() error                        { return nil }

type FileInput struct {
	Path    string
	Fixture bool
}

func (i *FileInput) Read(context.Context) (Batch, error) {
	raw, e := os.ReadFile(i.Path)
	if e != nil {
		return nil, e
	}
	var v any
	if e = decodeJSON(raw, &v); e != nil {
		return nil, e
	}
	if i.Fixture {
		if b, ok := v.(map[string]any); ok && b["fixture"] != nil {
			return b, nil
		}
		return Batch{"connected": true, "fixture": v}, nil
	}
	b, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("input must be JSON object")
	}
	return b, nil
}
func (i *FileInput) Close() error { return nil }

type CommandInput struct{ Options CommandOptions }

func (i *CommandInput) Read(ctx context.Context) (Batch, error) {
	raw, _, err := commandRun(ctx, i.Options, []byte("{}"))
	if err != nil {
		return nil, err
	}
	var b Batch
	err = decodeJSON(raw, &b)
	return b, err
}
func (i *CommandInput) Close() error { return nil }

type FixtureTransform struct{}

func fixtureResult(b Batch) (Result, map[string][]Ownership, error) {
	raw, e := json.Marshal(b["fixture"])
	var envelope struct {
		Tasks   map[string]Task        `json:"tasks"`
		Roots   []string               `json:"roots"`
		Sources map[string]Source      `json:"sources"`
		Owners  map[string][]Ownership `json:"owners,omitempty"`
	}
	if e == nil {
		e = decodeJSON(raw, &envelope)
	}
	r := Result{Tasks: envelope.Tasks, Roots: envelope.Roots, Sources: envelope.Sources}
	if e == nil {
		e = Validate(r)
	}
	if e != nil {
		return r, nil, e
	}
	for id, owners := range envelope.Owners {
		if _, ok := r.Tasks[id]; !ok || len(owners) == 0 {
			return r, nil, fmt.Errorf("invalid fixture owner task %q", id)
		}
		seen := map[Ownership]bool{}
		for i, owner := range owners {
			if owner.Scope == "" {
				owner.Scope = "default"
				owners[i] = owner
			}
			if owner.Source == "" || seen[owner] {
				return r, nil, fmt.Errorf("invalid fixture owner for %q", id)
			}
			seen[owner] = true
		}
	}
	return r, envelope.Owners, nil
}
func (FixtureTransform) Convert(_ context.Context, b Batch, _ Snapshot) (Result, error) {
	r, _, e := fixtureResult(b)
	return r, e
}

type ObservationsOnly struct{}

func (ObservationsOnly) Convert(_ context.Context, b Batch, previous Snapshot) (Result, error) {
	r := Result{Tasks: map[string]Task{}, Roots: []string{}, Sources: map[string]Source{}}
	obs, _ := b["observations"].([]any)
	for _, v := range obs {
		o, ok := v.(map[string]any)
		if !ok {
			return r, fmt.Errorf("invalid observation")
		}
		id, ok := o["id"].(string)
		if !ok || id == "" {
			return r, fmt.Errorf("invalid source ID")
		}
		name, _ := o["label"].(string)
		if name == "" {
			name = "未整理の入力"
		}
		tid := "observation:" + id
		at, _ := o["observedAt"].(float64)
		if old, ok := previous.Sources[id]; ok && sourceVersion(old) == sourceVersion(o) {
			at = previous.Tasks[tid].UpdatedAt
		}
		r.Sources[id] = o
		r.Tasks[tid] = Task{ID: tid, Purpose: str(name), State: "unknown", Attention: "unknown", Stale: !connected(b), UpdatedAt: at, Tree: []Node{{ID: "observation", State: "unknown", Attention: "unknown", Current: boolean(false), SourceRefs: []string{id}, Content: Content{Kind: "text", Text: str("作業の解釈は未設定です。根拠に取得内容を表示します。")}}}}
		r.Roots = append(r.Roots, tid)
	}
	sort.Strings(r.Roots)
	return r, nil
}
