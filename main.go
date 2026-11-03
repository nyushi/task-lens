package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"
)

//go:embed index.html fixtures/demo-tasks.json
var assets embed.FS

type PluginConfig struct {
	Plugin  string          `json:"plugin"`
	Options json.RawMessage `json:"options"`
}
type Config struct {
	Input     PluginConfig `json:"input"`
	Transform PluginConfig `json:"transform"`
	Interval  float64      `json:"interval"`
	Mode      string       `json:"mode"`
}

func options(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, v)
}
func makeInput(ctx context.Context, c PluginConfig) (Input, error) {
	switch c.Plugin {
	case "demo", "adapters:DemoInput":
		raw, _ := assets.ReadFile("fixtures/demo-tasks.json")
		var r any
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		return &StaticInput{Batch{"connected": true, "fixture": r}}, nil
	case "recorded", "file", "adapters:RecordedTaskInput":
		var o struct {
			Path string `json:"path"`
		}
		if e := options(c.Options, &o); e != nil {
			return nil, e
		}
		if o.Path == "" {
			return nil, fmt.Errorf("path required")
		}
		return &FileInput{Path: o.Path, Fixture: c.Plugin != "file"}, nil
	case "command":
		var o CommandOptions
		if e := options(c.Options, &o); e != nil {
			return nil, e
		}
		return &CommandInput{o}, nil
	case "herdr-tui":
		var o struct {
			Socket string   `json:"socket"`
			Panes  []string `json:"panes"`
		}
		if e := options(c.Options, &o); e != nil {
			return nil, e
		}
		return NewHerdrTUI(o.Socket, o.Panes)
	case "herdr", "herdr-core", "adapters:HerdrInput":
		var o struct {
			Socket    string   `json:"socket"`
			Terminals []string `json:"terminals"`
		}
		if e := options(c.Options, &o); e != nil {
			return nil, e
		}
		return NewHerdr(ctx, o.Socket, o.Terminals)
	default:
		return nil, fmt.Errorf("unknown input plugin %q", c.Plugin)
	}
}
func makeTransform(c PluginConfig) (Transform, error) {
	switch c.Plugin {
	case "fixture", "transforms:FixtureTransform":
		return FixtureTransform{}, nil
	case "observations", "transforms:ObservationsOnly":
		return ObservationsOnly{}, nil
	case "command", "transforms:CommandTransform":
		var o CommandOptions
		if e := options(c.Options, &o); e != nil {
			return nil, e
		}
		prompt, e := os.ReadFile(o.PromptFile)
		if e != nil {
			return nil, e
		}
		var limits IncrementalOptions
		if e := options(c.Options, &limits); e != nil {
			return nil, e
		}
		return NewIncremental(&CommandTransform{Options: o, Prompt: string(prompt)}, limits)
	default:
		return nil, fmt.Errorf("unknown transform plugin %q", c.Plugin)
	}
}
func webHandler(s *Store, port int, token, mode string) http.Handler {
	page, _ := assets.ReadFile("index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "127.0.0.1:"+strconv.Itoa(port) && r.Host != "localhost:"+strconv.Itoa(port) {
			http.Error(w, "forbidden", 403)
			return
		}
		if r.Method != "GET" {
			w.Header().Set("Allow", "GET")
			http.Error(w, "read only", 405)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'")
		switch r.URL.Path {
		case "/" + token + "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(page)
		case "/" + token + "/state":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			v := s.Snapshot()
			v.Mode = mode
			_ = json.NewEncoder(w).Encode(v)
		default:
			http.NotFound(w, r)
		}
	})
}
func fixtureCommand() error {
	var envelope struct {
		Input struct {
			Fixture   Result `json:"fixture"`
			Namespace string `json:"namespace"`
		} `json:"input"`
	}
	if err := json.NewDecoder(io.LimitReader(os.Stdin, 8000000)).Decode(&envelope); err != nil {
		return err
	}
	if ns := envelope.Input.Namespace; ns != "" {
		r := emptyResult()
		id := ns + "demo"
		r.Tasks[id] = Task{ID: id, Purpose: str("架空のテスト"), State: "doing", Attention: "review", Tree: []Node{}}
		r.Roots = append(r.Roots, id)
		return json.NewEncoder(os.Stdout).Encode(r)
	}
	if err := Validate(envelope.Input.Fixture); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(envelope.Input.Fixture)
}
func execute(ctx context.Context, args []string) error {
	if len(args) > 0 && args[0] == "codex-transform" {
		return codexTransform(ctx)
	}
	if len(args) > 0 && args[0] == "fixture-transform" {
		return fixtureCommand()
	}
	f := flag.NewFlagSet("task-lens", flag.ContinueOnError)
	path := f.String("config", "", "JSON config (default: embedded demo)")
	port := f.Int("port", 8765, "localhost port")
	if err := f.Parse(args); err != nil {
		return err
	}
	c := Config{Input: PluginConfig{Plugin: "demo"}, Transform: PluginConfig{Plugin: "fixture"}, Interval: 120, Mode: "ダミーデータ"}
	if *path != "" {
		raw, e := os.ReadFile(*path)
		if e != nil {
			return e
		}
		c = Config{}
		if e = decodeJSON(raw, &c); e != nil {
			return e
		}
	}
	if c.Interval == 0 {
		c.Interval = 120
	}
	if c.Interval < 15 {
		c.Interval = 15
	}
	if c.Mode == "" {
		c.Mode = "読み取り専用"
	}
	in, e := makeInput(ctx, c.Input)
	if e != nil {
		return e
	}
	defer in.Close()
	tr, e := makeTransform(c.Transform)
	if e != nil {
		return e
	}
	s := NewStore()
	if _, e = Tick(ctx, in, tr, s); e != nil {
		return fmt.Errorf("initial conversion failed: %w", e)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go Run(ctx, in, tr, s, time.Duration(c.Interval*float64(time.Second)))
	listener, e := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(*port)))
	if e != nil {
		return e
	}
	actual := listener.Addr().(*net.TCPAddr).Port
	random := make([]byte, 24)
	if _, e = rand.Read(random); e != nil {
		listener.Close()
		return e
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	server := &http.Server{Handler: webHandler(s, actual, token, c.Mode), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Printf("http://127.0.0.1:%d/%s/\n", actual, token)
	e = server.Serve(listener)
	if e == http.ErrServerClosed {
		return nil
	}
	return e
}
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := execute(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, strings.TrimSpace(err.Error()))
		os.Exit(1)
	}
}
