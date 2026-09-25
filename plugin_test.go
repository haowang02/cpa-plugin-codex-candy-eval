package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHasStandalone21(t *testing.T) {
	cases := map[string]bool{
		"21": true, "答案是 21 个": true, "最少取出21个": true, "**21**": true,
		"121": false, "210": false, "2 1": false, "": false, "答案是 20": false,
	}
	for text, want := range cases {
		if got := hasStandalone21(text); got != want {
			t.Errorf("hasStandalone21(%q) = %v, want %v", text, got, want)
		}
	}
}

// fakeHost serves host.auth.list and host.model.execute; answers maps auth ID to reply text.
func fakeHost(t *testing.T, answers map[string]string, seen chan<- map[string]any) {
	t.Helper()
	hostCall = func(method string, payload any) (json.RawMessage, error) {
		switch method {
		case "host.auth.list":
			return json.RawMessage(`{"files":[
				{"id":"b.json","name":"b.json","provider":"codex","email":"b@x"},
				{"id":"a.json","name":"a.json","type":"codex","email":"a@x"},
				{"id":"off.json","name":"off.json","provider":"codex","disabled":true},
				{"id":"c.json","name":"c.json","provider":"claude"}]}`), nil
		case "host.model.execute":
			raw, _ := json.Marshal(payload)
			var req map[string]any
			_ = json.Unmarshal(raw, &req)
			if seen != nil {
				seen <- req
			}
			id := req["auth_id"].(string)
			if id == "b.json" {
				return nil, fmt.Errorf("host_call_failed: usage limit reached")
			}
			body, _ := json.Marshal(map[string]any{
				"output": []any{
					map[string]any{"type": "reasoning"},
					map[string]any{"type": "message", "content": []any{map[string]any{"type": "output_text", "text": answers[id]}}},
				},
				"usage": map[string]any{"input_tokens": 300, "output_tokens": 900, "output_tokens_details": map[string]any{"reasoning_tokens": 850}},
			})
			return json.Marshal(map[string]any{"status_code": 200, "body": body})
		case "host.log":
			return json.RawMessage(`{}`), nil
		}
		return nil, fmt.Errorf("unexpected host call %s", method)
	}
}

func setup(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	statePath = filepath.Join(dir, "state.json")
	mu.Lock()
	loaded, results, running = false, map[string][]result{}, map[string]*progress{}
	mu.Unlock()
}

func manage(t *testing.T, method, path string, body any) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	request, _ := json.Marshal(managementRequest{Method: method, Path: path, Body: raw})
	var env struct {
		OK     bool               `json:"ok"`
		Result managementResponse `json:"result"`
	}
	if err := json.Unmarshal(handleMethod("management.handle", request), &env); err != nil || !env.OK {
		t.Fatalf("management.handle %s %s failed: %v", method, path, err)
	}
	var decoded map[string]any
	_ = json.Unmarshal(env.Result.Body, &decoded)
	return env.Result.StatusCode, decoded
}

func waitIdle(t *testing.T) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		mu.Lock()
		idle := len(running) == 0
		mu.Unlock()
		if idle {
			return
		}
	}
	t.Fatal("runs did not finish")
}

func TestRunAllAndState(t *testing.T) {
	setup(t)
	seen := make(chan map[string]any, 10)
	fakeHost(t, map[string]string{"a.json": "最少需要取出 **21** 个糖果。"}, seen)
	handleMethod("plugin.register", nil)

	status, body := manage(t, http.MethodPost, managementBase+"/run", runRequest{All: true, Model: "gpt-5.5", Effort: "high", Runs: 2})
	if status != http.StatusOK || body["started"].(float64) != 2 {
		t.Fatalf("run all = %d %v", status, body)
	}
	waitIdle(t)

	req := <-seen
	if req["forced_provider"] != "codex" || req["entry_protocol"] != "openai-response" || req["stream"] != false {
		t.Fatalf("unexpected execute request %v", req)
	}
	var payload map[string]any
	rawBody, _ := json.Marshal(req["body"])
	var bodyBytes []byte
	_ = json.Unmarshal(rawBody, &bodyBytes)
	_ = json.Unmarshal(bodyBytes, &payload)
	if payload["model"] != "gpt-5.5" || payload["reasoning"].(map[string]any)["effort"] != "high" || !strings.Contains(payload["input"].(string), "五角星形") {
		t.Fatalf("unexpected model payload %v", payload)
	}

	_, state := manage(t, http.MethodGet, managementBase+"/state", nil)
	auths := state["auths"].([]any)
	if len(auths) != 3 {
		t.Fatalf("want 3 codex auths, got %v", auths)
	}
	a := auths[0].(map[string]any)
	aResults := a["results"].([]any)
	if a["id"] != "a.json" || len(aResults) != 2 {
		t.Fatalf("auth a = %v", a)
	}
	first := aResults[0].(map[string]any)
	if first["ok"] != true || first["reasoning_tokens"].(float64) != 850 || first["answer"] != "最少需要取出 **21** 个糖果。" {
		t.Fatalf("result a = %v", first)
	}
	bResults := auths[1].(map[string]any)["results"].([]any)
	if len(bResults) != 2 || !strings.Contains(bResults[0].(map[string]any)["error"].(string), "usage limit") {
		t.Fatalf("result b = %v", bResults)
	}
	if off := auths[2].(map[string]any); len(off["results"].([]any)) != 0 {
		t.Fatalf("disabled auth must be skipped by run all: %v", off)
	}

	// Results survive a reload from the state file.
	mu.Lock()
	loaded, results = false, map[string][]result{}
	mu.Unlock()
	loadState()
	if len(results["a.json"]) != 2 {
		t.Fatalf("state file not reloaded: %v", results)
	}

	// Clearing removes the history from memory and from the state file.
	if status, _ := manage(t, http.MethodDelete, managementBase+"/results", nil); status != http.StatusOK {
		t.Fatalf("clear results = %d", status)
	}
	mu.Lock()
	loaded, results = false, map[string][]result{"stale": {{}}}
	mu.Unlock()
	loadState()
	if len(results) != 0 {
		t.Fatalf("results not cleared: %v", results)
	}
}

func TestRunSingleAndValidation(t *testing.T) {
	setup(t)
	fakeHost(t, map[string]string{"a.json": "答案是 20"}, nil)

	if status, _ := manage(t, http.MethodPost, managementBase+"/run", runRequest{AuthIDs: []string{"a.json"}}); status != http.StatusBadRequest {
		t.Fatalf("missing model should be rejected, got %d", status)
	}
	if status, _ := manage(t, http.MethodPost, managementBase+"/run", runRequest{AuthIDs: []string{"c.json"}, Model: "gpt-5.5"}); status != http.StatusBadRequest {
		t.Fatalf("non-codex auth should be rejected, got %d", status)
	}
	mu.Lock()
	for range 15 {
		results["a.json"] = append(results["a.json"], result{OK: true, Model: "old"})
	}
	mu.Unlock()
	status, body := manage(t, http.MethodPost, managementBase+"/run", runRequest{AuthIDs: []string{"a.json"}, Model: "gpt-5.5", Runs: 99})
	if status != http.StatusOK || body["started"].(float64) != 1 {
		t.Fatalf("run single = %d %v", status, body)
	}
	waitIdle(t)
	mu.Lock()
	defer mu.Unlock()
	// Runs clamp to maxRuns; history keeps the newest historyLimit entries.
	history := results["a.json"]
	if len(history) != historyLimit || history[historyLimit-maxRuns-1].Model != "old" || history[historyLimit-maxRuns].OK || history[historyLimit-1].Model != "gpt-5.5" {
		t.Fatalf("unexpected history %v", history)
	}
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("state file missing: %v", err)
	}
}

func TestUIAndRegistration(t *testing.T) {
	setup(t)
	// CLIProxyAPI rejects plugins with empty Name, Version, Author or GitHubRepository.
	var plugin struct {
		OK     bool `json:"ok"`
		Result struct {
			Metadata     map[string]any  `json:"metadata"`
			Capabilities map[string]bool `json:"capabilities"`
		} `json:"result"`
	}
	_ = json.Unmarshal(handleMethod("plugin.register", nil), &plugin)
	for _, field := range []string{"Name", "Version", "Author", "GitHubRepository"} {
		if value, _ := plugin.Result.Metadata[field].(string); strings.TrimSpace(value) == "" {
			t.Errorf("metadata %s must not be empty", field)
		}
	}
	if !plugin.OK || !plugin.Result.Capabilities["management_api"] {
		t.Fatalf("plugin.register = %+v", plugin)
	}
	var reg struct {
		OK     bool `json:"ok"`
		Result struct {
			Resources []map[string]string `json:"resources"`
		} `json:"result"`
	}
	_ = json.Unmarshal(handleMethod("management.register", nil), &reg)
	if !reg.OK || reg.Result.Resources[0]["Path"] != uiPath {
		t.Fatalf("management.register = %+v", reg)
	}
	request, _ := json.Marshal(managementRequest{Method: http.MethodGet, Path: uiPath})
	var env struct {
		Result managementResponse `json:"result"`
	}
	_ = json.Unmarshal(handleMethod("management.handle", request), &env)
	prompt, _ := json.Marshal(candyPrompt)
	if env.Result.StatusCode != http.StatusOK || !strings.Contains(string(env.Result.Body), "const PROMPT = "+string(prompt)+";") {
		t.Fatalf("ui response %d must embed the prompt", env.Result.StatusCode)
	}
}
