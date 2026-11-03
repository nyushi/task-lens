package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// codexTransform adapts a single-task, flat-node Codex response to the shared contract.
// It only runs when explicitly selected as a CLI command.
func codexTransform(ctx context.Context) error {
	var envelope struct {
		Prompt   string `json:"prompt"`
		Input    Batch  `json:"input"`
		Previous Result `json:"previous"`
	}
	if err := json.NewDecoder(io.LimitReader(os.Stdin, 8000000)).Decode(&envelope); err != nil {
		return err
	}
	object := func(p map[string]any) map[string]any {
		keys := []string{}
		for k := range p {
			keys = append(keys, k)
		}
		return map[string]any{"type": "object", "properties": p, "required": keys, "additionalProperties": false}
	}
	state := map[string]any{"type": "string", "enum": []string{"done", "doing", "todo", "unknown"}}
	attention := map[string]any{"type": "string", "enum": []string{"decision", "review", "none", "unknown"}}
	text := map[string]any{"type": "string"}
	schema := object(map[string]any{"purpose": text, "state": state, "attention": attention, "nodes": map[string]any{"type": "array", "items": object(map[string]any{"id": text, "text": text, "state": state, "attention": attention, "current": map[string]any{"type": "boolean"}})}})
	dir, err := os.MkdirTemp("", "tasklens-codex-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	schemaPath, resultPath := filepath.Join(dir, "schema.json"), filepath.Join(dir, "result.json")
	raw, _ := json.Marshal(schema)
	if err = os.WriteFile(schemaPath, raw, 0600); err != nil {
		return err
	}
	input, _ := json.Marshal(map[string]any{"input": envelope.Input, "previous": envelope.Previous})
	ns, _ := envelope.Input["namespace"].(string)
	prompt := envelope.Prompt + "\nUse only the supplied input. Do not use tools, read files, or access network resources. Return only the schema JSON.\n" + string(input)
	_, _, err = commandRun(ctx, CommandOptions{Command: "codex", Argv: []string{"exec", "--ignore-user-config", "--ephemeral", "--sandbox", "read-only", "--skip-git-repo-check", "--cd", dir, "--output-schema", schemaPath, "--output-last-message", resultPath, "--color", "never", "-"}, Timeout: 120}, []byte(prompt))
	if err != nil {
		return fmt.Errorf("Codex: %w", err)
	}
	raw, err = os.ReadFile(resultPath)
	if err != nil {
		return err
	}
	var value struct {
		Purpose   string `json:"purpose"`
		State     string `json:"state"`
		Attention string `json:"attention"`
		Nodes     []struct {
			ID        string `json:"id"`
			Text      string `json:"text"`
			State     string `json:"state"`
			Attention string `json:"attention"`
			Current   *bool  `json:"current"`
		} `json:"nodes"`
	}
	if err = decodeJSON(raw, &value); err != nil {
		return err
	}
	task := Task{ID: ns + "codex-task", Purpose: &value.Purpose, State: value.State, Attention: value.Attention, Tree: []Node{}}
	for _, n := range value.Nodes {
		task.Tree = append(task.Tree, Node{ID: ns + n.ID, State: n.State, Attention: n.Attention, Current: n.Current, SourceRefs: []string{ns + "input"}, Content: Content{Kind: "text", Text: str(n.Text)}})
	}
	result := Result{Tasks: map[string]Task{task.ID: task}, Roots: []string{task.ID}, Sources: map[string]Source{ns + "input": {"id": "input", "content": string(input)}}}
	if err = Validate(result); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
