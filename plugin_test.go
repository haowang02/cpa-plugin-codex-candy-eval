package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func setupTest(t *testing.T) {
	t.Helper()
	previousHost, previousPath, previousLoaded := hostCall, statePath, loaded
	previousResults, previousRunning := results, running
	t.Cleanup(func() {
		hostCall, statePath, loaded = previousHost, previousPath, previousLoaded
		results, running = previousResults, previousRunning
	})
	statePath, loaded = filepath.Join(t.TempDir(), "state.json"), false
	results, running = map[string][]result{}, map[string]*progress{}
	hostCall = func(method string, _ any) (json.RawMessage, error) {
		t.Errorf("unexpected host call: %s", method)
		return nil, fmt.Errorf("unexpected host call: %s", method)
	}
}

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
	setupTest(t)
	var sent struct {
		AuthID         string `json:"auth_id"`
		ForcedProvider string `json:"forced_provider"`
		Body           []byte `json:"body"`
	}
	hostCall = func(method string, payload any) (json.RawMessage, error) {
		if method != "host.model.execute" {
			t.Fatalf("unexpected host call: %s", method)
		}
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
	if !r.OK || r.Answer != "答案是 21" || r.InputTokens != 499 || r.OutputTokens != 900 || r.ReasoningTokens != 850 || r.Error != "" {
		t.Fatalf("result = %+v", r)
	}
	var payload struct {
		Model     string            `json:"model"`
		Input     string            `json:"input"`
		Reasoning map[string]string `json:"reasoning"`
	}
	if err := json.Unmarshal(sent.Body, &payload); err != nil || sent.AuthID != "a.json" || sent.ForcedProvider != "codex" ||
		payload.Model != "gpt-5.6-sol" || payload.Input != candyPrompt || payload.Reasoning["effort"] != "low" {
		t.Fatalf("request = %+v, payload = %+v, err = %v", sent, payload, err)
	}
}

func TestRunAllKeepsRecentHistory(t *testing.T) {
	setupTest(t)
	results = map[string][]result{"a.json": make([]result, 15)}
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
	setupTest(t)
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

func TestStateAccountPlans(t *testing.T) {
	setupTest(t)
	token := func(plan string) string {
		claims, _ := json.Marshal(map[string]any{"https://api.openai.com/auth": map[string]string{"chatgpt_plan_type": plan}})
		return "header." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
	}
	files := []authFile{
		{ID: "free", Name: "a.json", Provider: "codex", PlanType: " free "},
		{ID: "plus", Name: "b.json", Provider: "codex", AuthIndex: "plus"},
		{ID: "prolite", Name: "c.json", Provider: "codex", AuthIndex: "prolite", Disabled: true},
		{ID: "invalid", Name: "f.json", Provider: "codex", AuthIndex: "invalid"},
		{ID: "missing", Name: "g.json", Provider: "codex", AuthIndex: "missing"},
		{ID: "legacy", Name: "h.json", Provider: "codex"},
	}
	hostCall = func(method string, payload any) (json.RawMessage, error) {
		if method == "host.auth.list" {
			return json.Marshal(map[string]any{"files": files})
		}
		if method != "host.auth.get" {
			t.Fatalf("unexpected host call: %s", method)
		}
		index := payload.(map[string]string)["auth_index"]
		metadata := map[string]string{"access_token": "private-access-token"}
		switch index {
		case "plus":
			metadata["plan_type"] = index
			metadata["id_token"] = token("free") // Explicit metadata takes precedence.
		case "prolite":
			metadata["id_token"] = token(index)
		case "invalid":
			metadata["id_token"] = "malformed.jwt.token"
		case "missing":
			return nil, fmt.Errorf("auth file unavailable")
		default:
			t.Fatalf("unexpected credential read: %s", index)
		}
		return json.Marshal(map[string]any{"json": metadata})
	}
	response := stateResponse()
	var state struct {
		Auths []authView `json:"auths"`
	}
	if err := json.Unmarshal(response.Body, &state); err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("state response = %d, decode error = %v", response.StatusCode, err)
	}
	want := []string{"free", "plus", "prolite", "", "", ""}
	if len(state.Auths) != len(want) {
		t.Fatalf("got %d accounts, want %d", len(state.Auths), len(want))
	}
	for i, plan := range want {
		if state.Auths[i].PlanType != plan || state.Auths[i].Results == nil {
			t.Errorf("account %s: plan = %q, want %q; results = %v", state.Auths[i].ID, state.Auths[i].PlanType, plan, state.Auths[i].Results)
		}
	}
	if !state.Auths[2].Disabled {
		t.Error("disabled account lost its status")
	}
	for _, secret := range [][]byte{[]byte("private-access-token"), []byte("id_token"), []byte("signature")} {
		if bytes.Contains(response.Body, secret) {
			t.Errorf("state response contains credential data: %s", secret)
		}
	}
}

func TestStatePersistence(t *testing.T) {
	setupTest(t)
	for i := range historyLimit + 5 {
		results["a"] = append(results["a"], result{InputTokens: int64(i)})
	}
	if err := saveStateLocked(); err != nil {
		t.Fatal(err)
	}
	results = map[string][]result{}
	loadState()
	if history := results["a"]; len(history) != historyLimit || history[0].InputTokens != 5 {
		t.Fatalf("restored history must keep the latest %d results: %+v", historyLimit, history)
	}
	running["a"] = &progress{Done: 1, Total: 2}
	request := managementRequest{Method: http.MethodDelete, Path: managementBase + "/results"}
	if response := handleManagement(request); response.StatusCode != http.StatusOK || len(results) != 0 || running["a"].Done != 1 {
		t.Fatalf("clear must remove history and preserve in-flight runs: %+v", response)
	}
	loaded = false
	results["a"] = []result{{OK: true}}
	loadState()
	if len(results) != 0 {
		t.Fatal("cleared records reappeared after reloading state")
	}
	results["keep"] = []result{{Answer: "21", OK: true}}
	statePath = filepath.Join(t.TempDir(), "missing", "state.json")
	if response := handleManagement(request); response.StatusCode != http.StatusInternalServerError || len(results["keep"]) != 1 {
		t.Fatalf("failed persistence must report an error and preserve history: %+v", response)
	}
}

func TestEvaluateErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"upstream error", http.StatusTooManyRequests, `{"error":{"message":"quota exhausted"}}`},
		{"invalid response", http.StatusOK, `not json`},
		{"missing answer", http.StatusOK, `{"output":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupTest(t)
			hostCall = func(_ string, _ any) (json.RawMessage, error) {
				return json.Marshal(map[string]any{"status_code": tc.status, "body": []byte(tc.body)})
			}
			if r := evaluate("a", "gpt-5.6-sol", "low"); r.OK || r.Error == "" {
				t.Fatalf("request failure must not be graded as an answer: %+v", r)
			}
		})
	}
}
