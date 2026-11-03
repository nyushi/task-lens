package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Limits are enforced before starting any external process. Byte budgets are
// conservative accounting units, not provider token counts or billing limits.
type IncrementalOptions struct {
	MaxInputBytes       int     `json:"max_input_bytes"`
	MaxOutputBytes      int     `json:"max_output_bytes"`
	MaxTotalInputBytes  int     `json:"max_total_input_bytes"`
	MaxTotalOutputBytes int     `json:"max_total_output_bytes"`
	MaxCalls            int     `json:"max_calls"`
	MaxCallsPerTick     int     `json:"max_calls_per_tick"`
	MaxInitialChars     int     `json:"max_initial_chars"`
	BootstrapTailChars  int     `json:"bootstrap_tail_chars"`
	QuietSeconds        int     `json:"quiet_seconds"`
	MaxWaitSeconds      int     `json:"max_wait_seconds"`
	MinCallSeconds      int     `json:"min_call_seconds"`
	InputPrice          float64 `json:"input_price_per_million"`
	OutputPrice         float64 `json:"output_price_per_million"`
}

func (o *IncrementalOptions) defaults() error {
	if o.MaxInputBytes == 0 {
		o.MaxInputBytes = 16000
	}
	if o.MaxOutputBytes == 0 {
		o.MaxOutputBytes = 8000
	}
	if o.MaxTotalInputBytes == 0 {
		o.MaxTotalInputBytes = 128000
	}
	if o.MaxTotalOutputBytes == 0 {
		o.MaxTotalOutputBytes = 64000
	}
	if o.MaxCalls == 0 {
		o.MaxCalls = 32
	}
	if o.MaxCallsPerTick == 0 {
		o.MaxCallsPerTick = 2
	}
	if o.MaxInitialChars == 0 {
		o.MaxInitialChars = 2000
	}
	if o.QuietSeconds == 0 {
		o.QuietSeconds = 30
	}
	if o.MaxWaitSeconds == 0 {
		o.MaxWaitSeconds = 300
	}
	if o.MinCallSeconds == 0 {
		o.MinCallSeconds = 60
	}
	if o.MaxInputBytes < 1 || o.MaxOutputBytes < 1 || o.MaxTotalInputBytes < 1 || o.MaxTotalOutputBytes < 1 || o.MaxCalls < 1 || o.MaxCallsPerTick < 1 || o.MaxInitialChars < 1 || o.BootstrapTailChars < 0 || o.BootstrapTailChars > o.MaxInitialChars || o.QuietSeconds < 1 || o.MaxWaitSeconds < o.QuietSeconds || o.MinCallSeconds < 1 || o.InputPrice < 0 || o.OutputPrice < 0 {
		return fmt.Errorf("invalid incremental limits")
	}
	return nil
}

type Usage struct {
	Calls               int               `json:"calls"`
	InputBytes          int               `json:"input_bytes"`
	OutputBytes         int               `json:"output_bytes"`
	ReservedOutputBytes int               `json:"reserved_output_bytes"`
	Discarded           int               `json:"discarded"`
	EstimatedCost       float64           `json:"estimated_cost"`
	Held                map[string]string `json:"held"`
}
type sourceProgress struct {
	observed                    string
	changed, pending, attempted time.Time
	acknowledged                map[string]any
	result                      Result
}
type Incremental struct {
	CLI     *CommandTransform
	Limits  IncrementalOptions
	usage   Usage
	sources map[string]*sourceProgress
	now     func() time.Time
	// Tests replace the process boundary, never call an actual model.
	invoke func(context.Context, []byte) ([]byte, error)
}

func NewIncremental(cli *CommandTransform, limits IncrementalOptions) (*Incremental, error) {
	if e := limits.defaults(); e != nil {
		return nil, e
	}
	i := &Incremental{CLI: cli, Limits: limits, sources: map[string]*sourceProgress{}, now: time.Now, usage: Usage{Held: map[string]string{}}}
	i.invoke = func(ctx context.Context, raw []byte) ([]byte, error) {
		o := cli.Options
		o.MaxOutput = limits.MaxOutputBytes
		out, stderr, e := commandRun(ctx, o, raw)
		cli.LastStderr = stderr
		return out, e
	}
	return i, nil
}
func (i *Incremental) Convert(context.Context, Batch, Snapshot) (Result, error) {
	return Result{}, fmt.Errorf("incremental scheduler required")
}
func sourceVersion(o map[string]any) string {
	c := clone(o)
	delete(c, "revision")
	delete(c, "epoch")
	if m, ok := c["metadata"].(map[string]any); ok {
		delete(m, "revision")
	}
	return versionOf(c)
}
func observations(b Batch) (map[string]map[string]any, error) {
	a, ok := b["observations"].([]any)
	if !ok {
		return nil, fmt.Errorf("incremental CLI requires observations; whole-batch model input disabled")
	}
	out := map[string]map[string]any{}
	for _, v := range a {
		o, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid observation")
		}
		id, _ := o["id"].(string)
		if id == "" || out[id] != nil {
			return nil, fmt.Errorf("invalid observation ID")
		}
		if _, ok = o["content"].(string); !ok {
			return nil, fmt.Errorf("missing observation text")
		}
		out[id] = o
	}
	return out, nil
}
func emptyResult() Result {
	return Result{Tasks: map[string]Task{}, Roots: []string{}, Sources: map[string]Source{}}
}
func namespace(id string) string { return "src-" + versionOf(Batch{"id": id})[:16] + ":" }
func compactPrevious(r Result) Result {
	r = clone(r)
	for id, s := range r.Sources {
		r.Sources[id] = Source{"id": id, "sourceId": s["sourceId"], "revision": s["revision"], "note": "raw evidence omitted"}
	}
	return r
}

// A screen edit is not a durable log delta. Never infer history from overlap.
func screenChange(old, cur map[string]any, tail, maxInitial int) (map[string]any, error) {
	text := cur["content"].(string)
	r := []rune(text)
	if old == nil {
		partial := false
		if len(r) > maxInitial {
			if tail == 0 {
				return nil, fmt.Errorf("initial input exceeds character limit")
			}
			r = r[len(r)-tail:]
			partial = true
		}
		return map[string]any{"kind": "snapshot", "text": string(r), "partial": partial, "historyComplete": false}, nil
	}
	a := []rune(old["content"].(string))
	prefix := 0
	for prefix < len(a) && prefix < len(r) && a[prefix] == r[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(r)-prefix && a[len(a)-1-suffix] == r[len(r)-1-suffix] {
		suffix++
	}
	reset := versionOf(Batch{"continuity": old["continuity"]}) != versionOf(Batch{"continuity": cur["continuity"]})
	if reset {
		prefix = 0
		suffix = 0
	}
	return map[string]any{"kind": "screen_edit", "baseHash": versionOf(Batch{"text": string(a)}), "currentHash": versionOf(Batch{"text": text}), "prefixChars": prefix, "removedChars": len(a) - prefix - suffix, "insertedText": string(r[prefix : len(r)-suffix]), "suffixChars": suffix, "reset": reset, "historyComplete": false}, nil
}
func (i *Incremental) Tick(ctx context.Context, in Input, s *Store) (bool, error) {
	defer func() {
		i.usage.EstimatedCost = (float64(i.usage.InputBytes)*i.Limits.InputPrice + float64(i.usage.ReservedOutputBytes)*i.Limits.OutputPrice) / 1e6
		s.SetUsage(i.usage)
	}()
	b, e := in.Read(ctx)
	if e != nil {
		s.PauseCleanup()
		s.Observe("disconnected", false)
		s.Fail(e)
		return false, e
	}
	s.UpdatePresence(b)
	all, e := observations(b)
	if e != nil {
		s.PauseCleanup()
		s.Fail(e)
		return false, e
	}
	now := i.now()
	cleanupBatch := b
	readFailed := false
	ids := []string{}
	for id, o := range all {
		ids = append(ids, id)
		p := i.sources[id]
		if p == nil {
			p = &sourceProgress{result: emptyResult()}
			i.sources[id] = p
		}
		v := sourceVersion(o)
		if p.observed != v {
			p.observed = v
			p.changed = now
			if p.pending.IsZero() {
				p.pending = now
			}
		}
	}
	// Oldest pending source first, stable tie-break: a busy source cannot starve others.
	sort.Slice(ids, func(a, b int) bool {
		pa, pb := i.sources[ids[a]], i.sources[ids[b]]
		if pa.pending.Equal(pb.pending) {
			return ids[a] < ids[b]
		}
		return pa.pending.Before(pb.pending)
	})
	i.usage.Held = map[string]string{}
	calls := 0
	changed := false
	for _, id := range ids {
		p, o := i.sources[id], all[id]
		if p.acknowledged != nil && sourceVersion(p.acknowledged) == sourceVersion(o) {
			p.pending = time.Time{}
			continue
		}
		hold := func(reason string) { i.usage.Held[id] = reason }
		if now.Sub(p.changed) < time.Duration(i.Limits.QuietSeconds)*time.Second && now.Sub(p.pending) < time.Duration(i.Limits.MaxWaitSeconds)*time.Second {
			hold("coalescing")
			continue
		}
		if !p.attempted.IsZero() && now.Sub(p.attempted) < time.Duration(i.Limits.MinCallSeconds)*time.Second {
			hold("cooldown")
			continue
		}
		if calls >= i.Limits.MaxCallsPerTick {
			hold("per-tick call limit")
			continue
		}
		delta, e := screenChange(p.acknowledged, o, i.Limits.BootstrapTailChars, i.Limits.MaxInitialChars)
		if e != nil {
			hold(e.Error())
			continue
		}
		meta := clone(o)
		delete(meta, "content")
		delete(meta, "observedAt")
		ns := namespace(id)
		payload := map[string]any{"contractVersion": 2, "prompt": i.CLI.Prompt, "input": map[string]any{"source": meta, "change": delta, "namespace": ns, "instructions": "Return a complete Result for this source only. Prefix every Task, Node and Source ID with namespace. Preserve previous tasks unless evidence changes them. Screen edits are not log history; missing text does not mean completion. Cross-source references are forbidden; report dependencies as unresolved text. Do not use tools or fetch omitted context."}, "previous": compactPrevious(p.result)}
		raw, e := json.Marshal(payload)
		if e != nil {
			return false, e
		}
		if len(raw) > i.Limits.MaxInputBytes {
			hold("input byte limit (prompt + change + previous)")
			continue
		}
		if i.usage.Calls >= i.Limits.MaxCalls || i.usage.InputBytes+len(raw) > i.Limits.MaxTotalInputBytes || i.usage.ReservedOutputBytes+i.Limits.MaxOutputBytes > i.Limits.MaxTotalOutputBytes {
			hold("session budget exhausted")
			continue
		}
		p.attempted = now
		calls++
		i.usage.Calls++
		i.usage.InputBytes += len(raw)
		i.usage.ReservedOutputBytes += i.Limits.MaxOutputBytes
		out, e := i.invoke(ctx, raw)
		i.usage.OutputBytes += len(out)
		if e != nil {
			hold("command failed; budget retained")
			continue
		}
		if len(out) > i.Limits.MaxOutputBytes {
			hold("output byte limit")
			continue
		}
		var r Result
		if e = decodeJSON(out, &r); e == nil {
			e = validateOwned(r, ns)
		}
		if e != nil {
			hold("invalid or cross-source result")
			continue
		}
		latest, e := in.Read(ctx)
		if e != nil {
			readFailed = true
			s.PauseCleanup()
			hold("verification read failed")
			i.usage.Discarded++
			continue
		}
		s.UpdatePresence(latest)
		cleanupBatch = latest
		current, e := observations(latest)
		if e != nil || current[id] == nil || sourceVersion(current[id]) != sourceVersion(o) {
			hold("source changed during conversion")
			i.usage.Discarded++
			continue
		}
		// Unrelated source changes cannot invalidate this source's result.
		for key, task := range p.result.Tasks {
			if _, ok := r.Tasks[key]; !ok {
				task.Stale = true
				r.Tasks[key] = task
			}
		}
		for key, source := range p.result.Sources {
			if _, ok := r.Sources[key]; !ok {
				r.Sources[key] = source
			}
		}
		for _, root := range p.result.Roots {
			found := false
			for _, v := range r.Roots {
				if v == root {
					found = true
				}
			}
			if !found {
				r.Roots = append(r.Roots, root)
			}
		}
		if e := Validate(r); e != nil {
			hold("invalid merged history")
			continue
		}
		for tid, task := range r.Tasks {
			task.UpdatedAt = nowUnix(o, now)
			r.Tasks[tid] = task
		}
		p.result = r
		p.acknowledged = clone(o)
		p.pending = time.Time{}
		changed = true
	}
	merged := emptyResult()
	for id, p := range i.sources {
		stale := all[id] == nil || p.acknowledged == nil || sourceVersion(p.acknowledged) != sourceVersion(all[id])
		if all[id] == nil {
			i.usage.Held[id] = "source missing; history retained"
		}
		for k, t := range p.result.Tasks {
			t.Stale = t.Stale || stale
			merged.Tasks[k] = t
		}
		merged.Roots = append(merged.Roots, p.result.Roots...)
		for k, v := range p.result.Sources {
			merged.Sources[k] = v
		}
	}
	sort.Strings(merged.Roots)
	base := s.Snapshot()
	v := versionOf(b)
	s.Observe(v, connected(b))
	ok, e := s.Commit(base.Revision, v, merged)
	if ok {
		owners := map[string]Ownership{}
		for id, p := range i.sources {
			if p.acknowledged != nil {
				for tid := range p.result.Tasks {
					owners[tid] = Ownership{id, scopeOf(p.acknowledged)}
				}
			}
		}
		s.TrackOwners(owners)
	}
	if ok && !readFailed {
		removed := s.Reconcile(cleanupBatch, now)
		for id, p := range i.sources {
			hadRemoval := false
			for tid := range removed {
				if _, yes := p.result.Tasks[tid]; yes {
					hadRemoval = true
				}
				delete(p.result.Tasks, tid)
			}
			roots := []string{}
			for _, root := range p.result.Roots {
				if !removed[root] {
					roots = append(roots, root)
				}
			}
			p.result.Roots = roots
			if hadRemoval && len(p.result.Tasks) == 0 {
				delete(i.sources, id)
			}
		}
	}
	if e != nil {
		s.Fail(e)
	}
	return changed, e
}
func validateOwned(r Result, ns string) error {
	if e := Validate(r); e != nil {
		return e
	}
	for id := range r.Tasks {
		if !strings.HasPrefix(id, ns) {
			return fmt.Errorf("foreign task")
		}
	}
	for id := range r.Sources {
		if !strings.HasPrefix(id, ns) {
			return fmt.Errorf("foreign source")
		}
	}
	var walk func([]Node) bool
	walk = func(nodes []Node) bool {
		for _, n := range nodes {
			if !strings.HasPrefix(n.ID, ns) || !walk(n.Children) {
				return false
			}
		}
		return true
	}
	for _, t := range r.Tasks {
		if !walk(t.Tree) {
			return fmt.Errorf("foreign node")
		}
	}
	return nil
}
func (s *Store) SetUsage(u Usage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := clone(u)
	s.snapshot.Usage = &v
}

// Used in synthetic volume reports, not for tokenizer claims.
func inputChars(o map[string]any) int { return utf8.RuneCountInString(o["content"].(string)) }

func nowUnix(o map[string]any, now time.Time) float64 {
	if at, ok := o["observedAt"].(float64); ok && at > 0 {
		return at
	}
	return float64(now.Unix())
}
