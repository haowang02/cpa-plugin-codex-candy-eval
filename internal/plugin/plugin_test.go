package plugin

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTest(t *testing.T) {
	t.Helper()
	previousHost, previousPath, previousLoaded := hostCall, statePath, loaded
	previousLoadError, previousQuiescing := stateLoadError, quiescing
	previousResults, previousRunning := candyResults, candyRunning
	previousFPResults, previousFPRunning, previousFPError := fingerprintResults, fingerprintRunning, storageError
	t.Cleanup(func() {
		tasks.Wait()
		hostCall, statePath, loaded = previousHost, previousPath, previousLoaded
		stateLoadError, quiescing = previousLoadError, previousQuiescing
		candyResults, candyRunning = previousResults, previousRunning
		fingerprintResults, fingerprintRunning, storageError = previousFPResults, previousFPRunning, previousFPError
	})
	statePath, loaded = filepath.Join(t.TempDir(), "state.json"), false
	stateLoadError, quiescing = nil, false
	candyResults, candyRunning = map[string][]candyResult{}, map[string]*candyProgress{}
	fingerprintResults, fingerprintRunning, storageError = map[string][]fingerprintResult{}, map[string]*fingerprintProgress{}, ""
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

	r := evaluateCandy("a.json", "gpt-5.6-sol", "low")
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
	candyResults = map[string][]candyResult{"a.json": make([]candyResult, 15)}
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

	body, _ := json.Marshal(candyRunRequest{All: true, Model: "gpt-5.6-sol", Runs: 99})
	request, _ := json.Marshal(managementRequest{Method: http.MethodPost, Path: managementBase + "/run", Body: body})
	var env struct {
		Result managementResponse `json:"result"`
	}
	_ = json.Unmarshal(HandleMethod("management.handle", request), &env)
	if string(env.Result.Body) != `{"started":1}` {
		t.Fatalf("run = %d %s", env.Result.StatusCode, env.Result.Body)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		mu.Lock()
		idle := len(candyRunning) == 0
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
	history := candyResults["a.json"]
	if len(history) != candyHistoryLimit || history[candyHistoryLimit-candyMaxRuns-1].Error != "" || history[candyHistoryLimit-candyMaxRuns].Error == "" {
		t.Fatalf("want 10 old and %d new results, got %+v", candyMaxRuns, history)
	}
	if len(candyResults["off.json"]) != 0 || len(candyResults["c.json"]) != 0 {
		t.Fatalf("run all must skip disabled and non-Codex auth files: %+v", candyResults)
	}
}

func TestRegistration(t *testing.T) {
	setupTest(t)
	var reg struct {
		Result struct {
			Metadata map[string]any `json:"metadata"`
		} `json:"result"`
	}
	_ = json.Unmarshal(HandleMethod("plugin.register", nil), &reg)
	// CLIProxyAPI refuses to register a plugin when any of these is empty.
	for _, field := range []string{"Name", "Version", "Author", "GitHubRepository"} {
		if value, _ := reg.Result.Metadata[field].(string); value == "" {
			t.Errorf("metadata %s is empty", field)
		}
	}
	if !bytes.Contains(uiHTML, []byte(`<code id="question">`+candyPrompt+`</code>`)) {
		t.Error("displayed question differs from the evaluation prompt")
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
		{ID: "no-index", Name: "h.json", Provider: "codex"},
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
	if err := os.WriteFile(statePath, []byte(`{"results":{"old":[{"answer":"21","ok":true}]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	loadState()
	if len(candyResults["old"]) != 1 || len(fingerprintResults) != 0 {
		t.Fatal("independent candy history was not restored")
	}
	loaded = false
	for i := range candyHistoryLimit + 5 {
		candyResults["a"] = append(candyResults["a"], candyResult{InputTokens: int64(i)})
	}
	if err := saveStateLocked(); err != nil {
		t.Fatal(err)
	}
	candyResults = map[string][]candyResult{}
	loadState()
	if history := candyResults["a"]; len(history) != candyHistoryLimit || history[0].InputTokens != 5 {
		t.Fatalf("restored history must keep the latest %d results: %+v", candyHistoryLimit, history)
	}
	candyRunning["a"] = &candyProgress{Done: 1, Total: 2}
	request := managementRequest{Method: http.MethodDelete, Path: managementBase + "/results"}
	if response := handleManagement(request); response.StatusCode != http.StatusOK || len(candyResults) != 0 || candyRunning["a"].Done != 1 {
		t.Fatalf("clear must remove history and preserve in-flight runs: %+v", response)
	}
	loaded = false
	candyResults["a"] = []candyResult{{OK: true}}
	loadState()
	if len(candyResults) != 0 {
		t.Fatal("cleared records reappeared after reloading state")
	}
	candyResults["keep"] = []candyResult{{Answer: "21", OK: true}}
	statePath = filepath.Join(t.TempDir(), "missing", "state.json")
	if response := handleManagement(request); response.StatusCode != http.StatusInternalServerError || len(candyResults["keep"]) != 1 {
		t.Fatalf("failed persistence must report an error and preserve history: %+v", response)
	}
	if storageError == "" {
		t.Fatal("save failure must be visible in the page state")
	}
}

func TestUnreadableStateIsPreserved(t *testing.T) {
	setupTest(t)
	data := []byte(`{"results":`)
	if err := os.WriteFile(statePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	loadState()
	if storageError == "" || saveStateLocked() == nil {
		t.Fatal("unreadable history must report an error and block replacement")
	}
	got, err := os.ReadFile(statePath)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("unreadable history was overwritten")
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
		{"incomplete answer", http.StatusOK, `{"status":"incomplete","output":[{"type":"message","content":[{"type":"output_text","text":"21"}]}]}`},
		{"cancelled answer", http.StatusOK, `{"status":"cancelled","output":[{"type":"message","content":[{"type":"output_text","text":"21"}]}]}`},
		{"missing status code", 0, `{"output":[]}`},
		{"host panic", 0, "panic"},
		{"failed answer", http.StatusOK, `{"status":"failed","error":{"message":"failed"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupTest(t)
			hostCall = func(_ string, _ any) (json.RawMessage, error) {
				if tc.body == "panic" {
					panic("host request failed")
				}
				return json.Marshal(map[string]any{"status_code": tc.status, "body": []byte(tc.body)})
			}
			if r := evaluateCandy("a", "gpt-5.6-sol", "low"); r.OK || r.Error == "" {
				t.Fatalf("request failure must not be graded as an answer: %+v", r)
			}
		})
	}
}

func TestQuiesceDrainsBothTests(t *testing.T) {
	setupTest(t)
	gate, started := make(chan struct{}), make(chan struct{}, 3)
	hostCall = func(method string, _ any) (json.RawMessage, error) {
		if method == "host.auth.list" {
			return json.RawMessage(`{"files":[{"id":"candy","provider":"codex"},{"id":"fingerprint","provider":"codex"}]}`), nil
		}
		select {
		case started <- struct{}{}:
		default:
		}
		<-gate
		return mockModelResponse("21"), nil
	}
	defer func() { close(gate); tasks.Wait() }()
	candy := []byte(`{"auth_ids":["candy"],"model":"gpt-5.5","runs":10}`)
	fingerprint := []byte(`{"auth_ids":["fingerprint"],"model":"gpt-5.5","mode":"quick"}`)
	if candyRunResponse(candy).StatusCode != 200 || fingerprintRunResponse(fingerprint).StatusCode != 200 {
		t.Fatal("could not start tests")
	}
	for range 3 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("test did not start")
		}
	}
	stopped := make(chan struct{})
	go func() { Quiesce(); close(stopped) }()
	for deadline := time.Now().Add(time.Second); ; time.Sleep(time.Millisecond) {
		mu.Lock()
		stopping := quiescing
		mu.Unlock()
		if stopping {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("plugin did not enter quiesce")
		}
	}
	if candyRunResponse(candy).StatusCode != 503 || fingerprintRunResponse(fingerprint).StatusCode != 503 {
		t.Fatal("quiescing plugin accepted new tasks")
	}
	for range 3 {
		gate <- struct{}{}
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("plugin did not finish in-flight tasks")
	}
	if len(candyResults["candy"]) != 1 || fingerprintResults["fingerprint"][0].Status != "cancelled" {
		t.Fatal("quiesce did not stop further collection")
	}
	HandleMethod("plugin.reconfigure", nil)
	if quiescing {
		t.Fatal("reconfiguration did not resume the plugin")
	}
}
