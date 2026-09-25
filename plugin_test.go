package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestHasStandalone21(t *testing.T) {
	for text, want := range map[string]bool{
		"21": true, "最少取出**21**个": true, "答案：21。": true,
		"121": false, "210": false, "2 1": false,
	} {
		if got := hasStandalone21(text); got != want {
			t.Errorf("hasStandalone21(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestEvaluate(t *testing.T) {
	var sent struct {
		AuthID         string `json:"auth_id"`
		ForcedProvider string `json:"forced_provider"`
		Body           []byte `json:"body"`
	}
	hostCall = func(_ string, payload any) (json.RawMessage, error) {
		raw, _ := json.Marshal(payload)
		_ = json.Unmarshal(raw, &sent)
		body, _ := json.Marshal(map[string]any{
			"output": []any{
				map[string]any{"type": "reasoning"},
				map[string]any{"type": "message", "content": []any{
					map[string]any{"type": "output_text", "text": "答案是 "},
					map[string]any{"type": "output_text", "text": "21"},
				}},
			},
			"usage": map[string]any{"input_tokens": 499, "output_tokens": 900, "output_tokens_details": map[string]any{"reasoning_tokens": 850}},
		})
		return json.Marshal(map[string]any{"status_code": 200, "body": body})
	}

	r := evaluate("a.json", "gpt-5.6-sol", "low")
	if !r.OK || r.Answer != "答案是 21" || r.ReasoningTokens != 850 || r.Error != "" {
		t.Fatalf("result = %+v", r)
	}
	var payload struct {
		Model     string            `json:"model"`
		Reasoning map[string]string `json:"reasoning"`
	}
	if err := json.Unmarshal(sent.Body, &payload); err != nil || sent.AuthID != "a.json" || sent.ForcedProvider != "codex" ||
		payload.Model != "gpt-5.6-sol" || payload.Reasoning["effort"] != "low" {
		t.Fatalf("request = %+v, payload = %+v, err = %v", sent, payload, err)
	}
}

func TestRunAllKeepsRecentHistory(t *testing.T) {
	statePath = filepath.Join(t.TempDir(), "state.json")
	results = map[string][]result{"a.json": make([]result, 15)}
	running = map[string]*progress{}
	hostCall = func(method string, _ any) (json.RawMessage, error) {
		switch method {
		case "host.auth.list":
			return json.RawMessage(`{"files":[
				{"id":"a.json","name":"a.json","provider":"codex"},
				{"id":"off.json","name":"off.json","provider":"codex","disabled":true},
				{"id":"c.json","name":"c.json","provider":"claude"}]}`), nil
		case "host.model.execute":
			return nil, fmt.Errorf("usage limit reached")
		}
		return json.RawMessage(`{}`), nil
	}

	body, _ := json.Marshal(runRequest{All: true, Model: "gpt-5.6-sol", Runs: 99})
	request, _ := json.Marshal(managementRequest{Method: http.MethodPost, Path: managementBase + "/run", Body: body})
	var env struct {
		Result managementResponse `json:"result"`
	}
	_ = json.Unmarshal(handleMethod("management.handle", request), &env)
	if string(env.Result.Body) != `{"started":1}` {
		t.Fatalf("run = %d %s", env.Result.StatusCode, env.Result.Body)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		mu.Lock()
		idle := len(running) == 0
		mu.Unlock()
		if idle {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("runs did not finish")
		}
	}

	mu.Lock()
	defer mu.Unlock()
	history := results["a.json"]
	if len(history) != historyLimit || history[historyLimit-maxRuns-1].Error != "" || history[historyLimit-maxRuns].Error == "" {
		t.Fatalf("want 10 old and %d new results, got %+v", maxRuns, history)
	}
	if len(results["off.json"]) != 0 || len(results["c.json"]) != 0 {
		t.Fatalf("run all must skip disabled and non-Codex auth files: %+v", results)
	}
}

func TestRegistration(t *testing.T) {
	var reg struct {
		Result struct {
			Metadata map[string]any `json:"metadata"`
		} `json:"result"`
	}
	_ = json.Unmarshal(handleMethod("plugin.register", nil), &reg)
	// CLIProxyAPI refuses to register a plugin when any of these is empty.
	for _, field := range []string{"Name", "Version", "Author", "GitHubRepository"} {
		if value, _ := reg.Result.Metadata[field].(string); value == "" {
			t.Errorf("metadata %s is empty", field)
		}
	}
	prompt, _ := json.Marshal(candyPrompt)
	if !bytes.Contains(uiHTML, []byte("const PROMPT = "+string(prompt)+";")) {
		t.Error("ui.html no longer receives the prompt")
	}
}
