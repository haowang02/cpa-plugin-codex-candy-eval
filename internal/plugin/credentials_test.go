package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

func TestCredentialInventoryAndSync(t *testing.T) {
	setupTest(t)
	hostCall = func(method string, _ any) (json.RawMessage, error) {
		if method != "host.auth.list" {
			t.Fatalf("unexpected call %s", method)
		}
		return json.RawMessage(`{"files":[
			{"id":"old.json","provider":"codex","source":"file"},
			{"id":"cooling.json","provider":"codex","source":"file","status":"error","status_message":"quota exhausted","unavailable":true,"next_retry_after":"2999-01-01T00:00:00Z"},
			{"id":"recovered.json","provider":"codex","source":"file","unavailable":true,"next_retry_after":"2000-01-01T00:00:00Z"},
			{"id":"claude.json","type":"claude","source":"file","disabled":true},
			{"id":"runtime","provider":"gemini","source":"memory","label":"secret-key","email":"secret-key","account_type":"api_key","account":"synthetic-secret-key"},
			{"id":"configured","provider":"codex","runtime_only":true,"disabled":true,"unavailable":true},
			{"provider":"codex","name":"missing-id"}]}`), nil
	}
	response := syncCredentialsResponse([]byte(`{"credentials":[{"id":"configured","provider":"codex","name":"Configured Codex","disabled":true,"email":"must-not-sync"},{"id":"compat","provider":"openai-compatible-demo","name":"Configured Demo"}]}`))
	if response.StatusCode != 200 {
		t.Fatal(string(response.Body))
	}
	all, err := credentials()
	if err != nil || len(all) != 7 {
		t.Fatalf("credentials = %+v, %v", all, err)
	}
	for _, auth := range all {
		if auth.ID == "configured" && (!auth.Disabled || auth.Unavailable || auth.Source != "ai_providers" || auth.Email != "") {
			t.Fatalf("config merge: %+v", auth)
		}
		if auth.ID == "runtime" && (auth.Name != "synthe…-key" || auth.Email != "") {
			t.Fatalf("unsafe runtime display: %+v", auth)
		}
		if auth.ID == "cooling.json" && (!auth.Unavailable || auth.StatusMessage != "quota exhausted" || auth.NextRetryAfter.IsZero()) || auth.ID == "recovered.json" && auth.Unavailable {
			t.Fatalf("unavailable state: %+v", auth)
		}
	}
	selected, err := selectedCredentials(nil, true)
	if err != nil || len(selected) != 4 {
		t.Fatalf("selected = %+v, %v", selected, err)
	}
	selected, _ = selectedCredentials([]string{"compat", "configured", "cooling.json"}, false)
	if len(selected) != 1 || selected[0].Provider != "openai-compatible-demo" {
		t.Fatalf("selection = %+v", selected)
	}
	for _, body := range []string{`{}`, `{"credentials":[{"id":"a","provider":"codex"},{"id":"a","provider":"codex"}]}`} {
		if res := syncCredentialsResponse([]byte(body)); res.StatusCode != 400 {
			t.Fatal("invalid sync accepted")
		}
	}
	if len(configuredCredentials) != 2 {
		t.Fatal("invalid sync replaced inventory")
	}
	syncCredentialsResponse([]byte(`{"credentials":[{"id":"configured","provider":"codex"},{"id":"old.json","provider":"codex","disabled":true}]}`))
	all, _ = credentials()
	for _, auth := range all {
		if auth.ID == "configured" && !auth.Disabled {
			t.Fatal("sync re-enabled a disabled host credential")
		}
		if auth.ID == "old.json" && (auth.Source != credentialSourceFile || auth.Disabled) {
			t.Fatal("sync overwrote an authentication file")
		}
	}
	syncCredentialsResponse([]byte(`{"credentials":[]}`))
	if len(configuredCredentials) != 0 {
		t.Fatal("removed configured credentials remained selectable")
	}
}

func TestModelRequiresCredentialRoute(t *testing.T) {
	setupTest(t)
	hostCall = func(string, any) (json.RawMessage, error) {
		t.Fatal("model executed without a complete credential route")
		return nil, nil
	}
	for _, auth := range []credential{{ID: "a"}, {Provider: "codex"}} {
		if _, _, err := executeModel(auth, "model", nil); err == nil {
			t.Fatal("missing route accepted")
		}
	}
}

func TestCredentialListResponse(t *testing.T) {
	setupTest(t)
	for _, body := range []string{`null`, `{}`, `{"files":null}`, `{"files":{}}`, `{"files":[]}`} {
		hostCall = func(string, any) (json.RawMessage, error) { return json.RawMessage(body), nil }
		response := stateResponse()
		want := 502
		if body == `{"files":[]}` {
			want = 200
		}
		if response.StatusCode != want {
			t.Fatalf("body=%s status=%d, want %d", body, response.StatusCode, want)
		}
	}
}

func TestCredentialModelCatalog(t *testing.T) {
	for _, tc := range []struct {
		model            string
		catalog          []string
		known, supported bool
	}{
		{"alias", nil, false, false},
		{"alias", []string{}, true, false},
		{"alias", []string{"upstream"}, true, false},
		{"prefix/alias", []string{"alias"}, true, false},
		{"prefix/alias", []string{"prefix/alias"}, true, true},
		{"alias(high)", []string{"alias"}, true, true},
		{"alias(high)", []string{"alias(high)"}, true, true},
	} {
		known, supported := credentialSupportsModel(map[string][]string{"a": tc.catalog}, "a", tc.model)
		if known != tc.known || supported != tc.supported {
			t.Errorf("%+v: %v %v", tc, known, supported)
		}
	}
}

func TestCandyPreflightPinnedProvidersAndNone(t *testing.T) {
	setupTest(t)
	configuredCredentials["config"] = credential{ID: "config", Provider: "openai-compatible-demo", Source: "ai_providers"}
	hostCall = func(method string, payload any) (json.RawMessage, error) {
		if method == "host.auth.list" {
			return json.RawMessage(`{"files":[{"id":"skip","provider":"claude"},{"id":"unknown","provider":"gemini"},{"id":"off","provider":"codex","disabled":true}]}`), nil
		}
		if method != "host.model.execute" {
			return nil, fmt.Errorf("unexpected %s", method)
		}
		var req struct {
			ID       string `json:"auth_id"`
			Provider string `json:"forced_provider"`
			Body     []byte `json:"body"`
		}
		raw, _ := json.Marshal(payload)
		_ = json.Unmarshal(raw, &req)
		if (req.ID != "config" || req.Provider != "openai-compatible-demo") && (req.ID != "unknown" || req.Provider != "gemini") {
			t.Errorf("unpinned or skipped request: %+v", req)
		}
		if bytes.Contains(req.Body, []byte(`"reasoning"`)) {
			t.Error("none sent a reasoning parameter")
		}
		return mockModelResponse("21"), nil
	}
	response := candyRunResponse([]byte(`{"all":true,"model":"alias","effort":"none","runs":2,"model_catalog":{"skip":[],"config":["alias"]}}`))
	if string(response.Body) != `{"started":2,"skipped":1,"unchecked":1}` {
		t.Fatalf("run: %s", response.Body)
	}
	tasks.Wait()
	if len(candyResults["skip"]) != 1 || !candyResults["skip"][0].Skipped || candyResults["skip"][0].DurationMS != 0 {
		t.Fatal("skip was executed or not recorded")
	}
	if len(candyResults["config"]) != 2 || len(candyResults["unknown"]) != 2 || len(candyResults["off"]) != 0 {
		t.Fatal("incorrect run scope")
	}
	candyResults = map[string][]candyResult{}
	loadState()
	if !candyResults["skip"][0].Skipped || !candyResults["config"][0].OK {
		t.Fatal("new credential history was not restored")
	}
}

func TestFingerprintPreflightSkipsWithoutRequests(t *testing.T) {
	setupTest(t)
	hostCall = func(method string, _ any) (json.RawMessage, error) {
		if method != "host.auth.list" {
			t.Fatalf("skipped fingerprint executed: %s", method)
		}
		return json.RawMessage(`{"files":[{"id":"a","provider":"claude"}]}`), nil
	}
	response := fingerprintRunResponse([]byte(`{"all":true,"model":"gpt-5.6-sol","mode":"strict","model_catalog":{"a":["claude-model"]}}`))
	if string(response.Body) != `{"started":0,"skipped":1}` {
		t.Fatalf("response = %s", response.Body)
	}
	if r := fingerprintResults["a"][0]; r.Status != "skipped" || r.Total != 0 || r.Done != 0 || r.Attribution.Status != "" {
		t.Fatalf("result = %+v", r)
	}
	fingerprintResults = map[string][]fingerprintResult{}
	loadState()
	if fingerprintResults["a"][0].Status != "skipped" {
		t.Fatal("skip history not persisted")
	}
}

func TestUnsupportedEffortRemainsRequestError(t *testing.T) {
	setupTest(t)
	hostCall = func(_ string, payload any) (json.RawMessage, error) {
		req := payload.(map[string]any)
		var body map[string]any
		_ = json.Unmarshal(req["body"].([]byte), &body)
		if body["reasoning"].(map[string]any)["effort"] != "xhigh" || req["forced_provider"] != "claude" {
			t.Error("effort or provider was changed")
		}
		return nil, &EnvelopeError{Code: "unsupported_effort", Message: "not supported", HTTPStatus: 400}
	}
	r := evaluateCandy(credential{ID: "a", Provider: "claude"}, "alias", "xhigh")
	if r.Skipped || r.OK || r.Error == "" {
		t.Fatalf("unsupported effort result = %+v", r)
	}
}
