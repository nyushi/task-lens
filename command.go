//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type CommandOptions struct {
	Command    string   `json:"command"`
	Argv       []string `json:"argv"`
	PromptFile string   `json:"prompt_file"`
	Timeout    float64  `json:"timeout"`
	MaxOutput  int      `json:"max_output_bytes"`
}
type limitedBuffer struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	limit    int
	exceeded bool
	cancel   context.CancelFunc
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	remain := b.limit - b.buf.Len()
	if len(p) > remain {
		p = p[:remain]
		b.exceeded = true
		if b.cancel != nil {
			b.cancel()
		}
	}
	_, _ = b.buf.Write(p)
	return n, nil
}
func commandRun(ctx context.Context, o CommandOptions, input []byte) ([]byte, string, error) {
	if o.Command == "" {
		return nil, "", fmt.Errorf("command is required")
	}
	if o.Timeout == 0 {
		o.Timeout = 30
	}
	if o.MaxOutput == 0 {
		o.MaxOutput = 2000000
	}
	if o.Timeout <= 0 || o.Timeout > 300 || o.MaxOutput <= 0 || o.MaxOutput > 8000000 {
		return nil, "", fmt.Errorf("invalid command limits")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(o.Timeout*float64(time.Second)))
	defer cancel()
	out := &limitedBuffer{limit: o.MaxOutput, cancel: cancel}
	stderr := &limitedBuffer{limit: 8192}
	cmd := exec.CommandContext(ctx, o.Command, o.Argv...)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = out
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if out.exceeded {
		return nil, stderr.buf.String(), fmt.Errorf("stdout exceeds limit")
	}
	if ctx.Err() != nil {
		return nil, stderr.buf.String(), ctx.Err()
	}
	if err != nil {
		return nil, stderr.buf.String(), fmt.Errorf("command failed: %w", err)
	}
	return out.buf.Bytes(), stderr.buf.String(), nil
}
func decodeJSON(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected one JSON value")
	}
	return nil
}

type CommandTransform struct {
	Options    CommandOptions
	Prompt     string
	LastStderr string
}

func (t *CommandTransform) Convert(ctx context.Context, b Batch, p Snapshot) (Result, error) {
	raw, err := json.Marshal(map[string]any{"contractVersion": 1, "prompt": t.Prompt, "input": b, "previous": p})
	if err != nil {
		return Result{}, err
	}
	out, stderr, err := commandRun(ctx, t.Options, raw)
	t.LastStderr = stderr
	if err != nil {
		return Result{}, err
	}
	var r Result
	err = decodeJSON(out, &r)
	return r, err
}
