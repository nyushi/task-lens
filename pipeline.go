package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

type Batch = map[string]any
type Input interface {
	Read(context.Context) (Batch, error)
	Close() error
}
type Transform interface {
	Convert(context.Context, Batch, Snapshot) (Result, error)
}

func versionOf(b Batch) string {
	var stable func(any) any
	stable = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			m := map[string]any{}
			for k, v := range x {
				if k != "observedAt" {
					m[k] = stable(v)
				}
			}
			return m
		case []any:
			a := make([]any, len(x))
			for i, v := range x {
				a[i] = stable(v)
			}
			return a
		default:
			return v
		}
	}
	raw, _ := json.Marshal(stable(b))
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
func connected(b Batch) bool { v, _ := b["connected"].(bool); return v }
func Tick(ctx context.Context, in Input, tr Transform, s *Store) (bool, error) {
	b, err := in.Read(ctx)
	if err != nil {
		s.Observe("disconnected", false)
		s.Fail(err)
		return false, err
	}
	v := versionOf(b)
	previous := s.Snapshot()
	if s.Observe(v, connected(b)) {
		return false, nil
	}
	r, err := tr.Convert(ctx, b, previous)
	if err != nil {
		s.Fail(err)
		return false, err
	}
	latest, err := in.Read(ctx)
	if err != nil {
		s.Observe("disconnected", false)
		s.Fail(err)
		return false, err
	}
	lv := versionOf(latest)
	s.Observe(lv, connected(latest))
	if lv != v {
		err = fmt.Errorf("input changed during transform")
		s.Fail(err)
		return false, err
	}
	ok, err := s.Commit(previous.Revision, v, r)
	if err != nil {
		s.Fail(err)
	}
	return ok, err
}
func Run(ctx context.Context, in Input, tr Transform, s *Store, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = Tick(ctx, in, tr, s)
		}
	}
}
