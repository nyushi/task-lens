package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func synthetic(n, size int) Batch {
	a := []any{}
	for x := 0; x < n; x++ {
		a = append(a, map[string]any{"id": fmt.Sprintf("p%02d", x), "content": strings.Repeat("字", size), "revision": 1, "continuity": "0", "metadata": map[string]any{"agent_status": "working"}})
	}
	return Batch{"connected": true, "observations": a}
}
func newTestIncremental(t *testing.T, o IncrementalOptions) (*Incremental, *time.Time) {
	t.Helper()
	i, e := NewIncremental(&CommandTransform{Prompt: "synthetic contract"}, o)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Unix(10000, 0)
	i.now = func() time.Time { return now }
	i.invoke = func(_ context.Context, raw []byte) ([]byte, error) {
		var v struct {
			Input struct {
				Namespace string `json:"namespace"`
			} `json:"input"`
		}
		if e := json.Unmarshal(raw, &v); e != nil {
			return nil, e
		}
		id := v.Input.Namespace + "task"
		r := emptyResult()
		r.Tasks[id] = Task{ID: id, Purpose: str("synthetic summary"), State: "doing", Attention: "none", Tree: []Node{}}
		r.Roots = []string{id}
		return json.Marshal(r)
	}
	return i, &now
}
func tickIncremental(t *testing.T, i *Incremental, in Input, s *Store) {
	t.Helper()
	if _, e := Tick(context.Background(), in, i, s); e != nil {
		t.Fatal(e)
	}
}
func TestLargeInputBudgetMeasurement(t *testing.T) {
	in := &StaticInput{synthetic(15, 10000)}
	s := NewStore()
	i, now := newTestIncremental(t, IncrementalOptions{})
	tickIncremental(t, i, in, s)
	*now = now.Add(31 * time.Second)
	tickIncremental(t, i, in, s)
	if i.usage.Calls != 0 || len(i.usage.Held) != 15 {
		t.Fatal(i.usage)
	}
	t.Logf("150000 characters initial/default: calls=%d input_bytes=%d output_bytes=%d held=%d", i.usage.Calls, i.usage.InputBytes, i.usage.OutputBytes, len(i.usage.Held))
	// Explicit operator choice: bootstrap only the last 1000 chars of each source.
	i, now = newTestIncremental(t, IncrementalOptions{BootstrapTailChars: 1000, MaxTotalOutputBytes: 256000})
	s = NewStore()
	tickIncremental(t, i, in, s)
	*now = now.Add(31 * time.Second)
	for x := 0; x < 8; x++ {
		tickIncremental(t, i, in, s)
		*now = now.Add(31 * time.Second)
	}
	if i.usage.Calls != 15 {
		t.Fatal(i.usage)
	}
	initial := i.usage
	tickIncremental(t, i, in, s)
	if i.usage.Calls != 15 {
		t.Fatal("unchanged called")
	}
	obs := in.Batch["observations"].([]any)
	p := obs[0].(map[string]any)
	p["content"] = p["content"].(string) + strings.Repeat("追", 100)
	p["revision"] = 2
	tickIncremental(t, i, in, s)
	*now = now.Add(61 * time.Second)
	tickIncremental(t, i, in, s)
	if i.usage.Calls != 16 {
		t.Fatal(i.usage)
	}
	t.Logf("explicit tail1000 bootstrap: calls=%d input_bytes=%d output_bytes=%d; one source +100 chars: calls=%d input_bytes=%d output_bytes=%d", initial.Calls, initial.InputBytes, initial.OutputBytes, i.usage.Calls-initial.Calls, i.usage.InputBytes-initial.InputBytes, i.usage.OutputBytes-initial.OutputBytes)
	// Continuous output: no send before max wait; one pending source only.
	before := i.usage.Calls
	for x := 0; x < 20; x++ {
		p["content"] = p["content"].(string) + "出"
		*now = now.Add(10 * time.Second)
		tickIncremental(t, i, in, s)
	}
	if i.usage.Calls != before {
		t.Fatal("uncoalesced output")
	}
	*now = now.Add(31 * time.Second)
	tickIncremental(t, i, in, s)
	if i.usage.Calls != before+1 {
		t.Fatal(i.usage)
	}
	t.Logf("20 changes/200s + quiet: calls=%d cumulative_input_bytes=%d", i.usage.Calls-before, i.usage.InputBytes)
	// Loss/reset requires a full snapshot; oversized resync is held, not silently tailed.
	p["continuity"] = "reconnect"
	tickIncremental(t, i, in, s)
	*now = now.Add(61 * time.Second)
	tickIncremental(t, i, in, s)
	if i.usage.Calls != before+1 || i.usage.Held["p00"] == "" {
		t.Fatal("unsafe resync", i.usage)
	}
	t.Logf("reconnect with 10120 chars: calls=0 reason=%s", i.usage.Held["p00"])
}
func TestIncrementalSafety(t *testing.T) {
	in := &StaticInput{synthetic(2, 20)}
	s := NewStore()
	i, now := newTestIncremental(t, IncrementalOptions{})
	tickIncremental(t, i, in, s)
	*now = now.Add(31 * time.Second)
	tickIncremental(t, i, in, s)
	if len(s.Snapshot().Tasks) != 2 {
		t.Fatal("missing tasks")
	}
	original := i.invoke
	obs := in.Batch["observations"].([]any)
	p := obs[0].(map[string]any)
	p["content"] = "changed"
	tickIncremental(t, i, in, s)
	*now = now.Add(61 * time.Second)
	i.invoke = func(c context.Context, b []byte) ([]byte, error) {
		p["content"] = "changed again"
		return original(c, b)
	}
	tickIncremental(t, i, in, s)
	if i.usage.Discarded != 1 {
		t.Fatal("stale result accepted")
	}
	calls := i.usage.Calls
	tickIncremental(t, i, in, s)
	if i.usage.Calls != calls {
		t.Fatal("immediate retry")
	}
	i.invoke = original
	*now = now.Add(61 * time.Second)
	tickIncremental(t, i, in, s)
	// Another source changing during conversion must not discard the target result.
	p["content"] = "next"
	tickIncremental(t, i, in, s)
	*now = now.Add(61 * time.Second)
	i.invoke = func(c context.Context, b []byte) ([]byte, error) {
		obs[1].(map[string]any)["content"] = "unrelated"
		return original(c, b)
	}
	tickIncremental(t, i, in, s)
	if i.usage.Discarded != 1 {
		t.Fatal("unrelated source invalidated target")
	}
	// Session budget is not refunded on errors.
	j, clock := newTestIncremental(t, IncrementalOptions{MaxCalls: 1})
	j.invoke = func(context.Context, []byte) ([]byte, error) { return nil, fmt.Errorf("failure") }
	ss := NewStore()
	tickIncremental(t, j, in, ss)
	*clock = clock.Add(31 * time.Second)
	tickIncremental(t, j, in, ss)
	*clock = clock.Add(time.Hour)
	tickIncremental(t, j, in, ss)
	if j.usage.Calls != 1 {
		t.Fatal("budget bypass")
	}
	// Cross-source references / task ownership never accepted.
	r := emptyResult()
	r.Tasks["foreign"] = Task{ID: "foreign", Purpose: str("x"), State: "done", Attention: "none", Tree: []Node{}}
	r.Roots = []string{"foreign"}
	if validateOwned(r, "src:") == nil {
		t.Fatal("foreign task allowed")
	}
}
func TestScreenEdit(t *testing.T) {
	for _, tc := range []struct {
		a, b, insert string
		removed      int
	}{{"abc", "axc", "x", 1}, {"abc", "bcX", "bcX", 3}, {"abc", "ab", "", 1}, {"字字", "字追加字", "追加", 0}} {
		d, e := screenChange(map[string]any{"content": tc.a}, map[string]any{"content": tc.b}, 0, 2000)
		if e != nil || d["insertedText"] != tc.insert || d["removedChars"] != tc.removed {
			t.Fatal(d, e)
		}
	}
}

func TestIncrementalLimitsAndRecovery(t *testing.T) {
	in := &StaticInput{synthetic(1, 50)}
	s := NewStore()
	i, now := newTestIncremental(t, IncrementalOptions{})
	tickIncremental(t, i, in, s)
	*now = now.Add(31 * time.Second)
	tickIncremental(t, i, in, s)
	p := in.Batch["observations"].([]any)[0].(map[string]any)
	before := i.usage.Calls
	p["revision"] = 999
	p["observedAt"] = 100
	*now = now.Add(61 * time.Second)
	tickIncremental(t, i, in, s)
	if i.usage.Calls != before {
		t.Fatal("revision-only change sent")
	}
	p["continuity"] = "restart"
	p["content"] = "small recovered viewport"
	tickIncremental(t, i, in, s)
	*now = now.Add(61 * time.Second)
	tickIncremental(t, i, in, s)
	if i.usage.Calls != before+1 {
		t.Fatal("recovery blocked")
	}
	saved := in.Batch
	in.Batch = Batch{"connected": true, "observations": []any{}}
	tickIncremental(t, i, in, s)
	for _, task := range s.Snapshot().Tasks {
		if !task.Stale {
			t.Fatal("missing source not stale")
		}
	}
	in.Batch = saved
	// Prompt itself participates in the wire-size limit, before invoking the process.
	j, clock := newTestIncremental(t, IncrementalOptions{MaxInputBytes: 1000})
	j.CLI.Prompt = strings.Repeat("P", 1001)
	ss := NewStore()
	tickIncremental(t, j, in, ss)
	*clock = clock.Add(time.Minute)
	tickIncremental(t, j, in, ss)
	if j.usage.Calls != 0 {
		t.Fatal("oversize prompt sent")
	}
	// A never-quiet source eventually gets a bounded attempt at max wait.
	k, clock := newTestIncremental(t, IncrementalOptions{MaxWaitSeconds: 40})
	ss = NewStore()
	tickIncremental(t, k, in, ss)
	for x := 0; x < 4; x++ {
		*clock = clock.Add(10 * time.Second)
		p["content"] = fmt.Sprint(x)
		tickIncremental(t, k, in, ss)
	}
	if k.usage.Calls != 1 {
		t.Fatal("continuous source starvation", k.usage)
	}
}
func TestIncrementalSubprocess(t *testing.T) {
	cli := &CommandTransform{Options: CommandOptions{Command: os.Args[0], Argv: []string{"-test.run=TestHelperProcess", "--", "incremental"}}, Prompt: "dummy"}
	i, e := NewIncremental(cli, IncrementalOptions{})
	if e != nil {
		t.Fatal(e)
	}
	now := time.Unix(10000, 0)
	i.now = func() time.Time { return now }
	in := &StaticInput{synthetic(1, 5)}
	s := NewStore()
	tickIncremental(t, i, in, s)
	now = now.Add(time.Minute)
	tickIncremental(t, i, in, s)
	if len(s.Snapshot().Tasks) != 1 || i.usage.Calls != 1 {
		t.Fatal(i.usage)
	}
}
func TestV2Examples(t *testing.T) {
	raw, e := os.ReadFile("examples/input.json")
	if e != nil {
		t.Fatal(e)
	}
	var input struct {
		ContractVersion int `json:"contractVersion"`
		Input           struct {
			Namespace string `json:"namespace"`
		} `json:"input"`
	}
	if e = json.Unmarshal(raw, &input); e != nil || input.ContractVersion != 2 {
		t.Fatal(e)
	}
	raw, e = os.ReadFile("examples/output.json")
	if e != nil {
		t.Fatal(e)
	}
	var r Result
	if e = json.Unmarshal(raw, &r); e != nil {
		t.Fatal(e)
	}
	if e = validateOwned(r, input.Input.Namespace); e != nil {
		t.Fatal(e)
	}
}
func TestPreviousEvidenceOmitted(t *testing.T) {
	r := emptyResult()
	r.Sources["s"] = Source{"content": strings.Repeat("secret synthetic", 10000), "revision": 1, "id": "s"}
	raw, e := json.Marshal(compactPrevious(r))
	if e != nil || len(raw) > 250 || strings.Contains(string(raw), "secret synthetic") {
		t.Fatal("prior evidence sent", len(raw), e)
	}
}
