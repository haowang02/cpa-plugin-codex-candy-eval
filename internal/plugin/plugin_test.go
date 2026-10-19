package plugin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func setupTest(t *testing.T) {
	t.Helper()
	previousCredentials := configuredCredentials
	previousHost, previousPath, previousLoaded := hostCall, statePath, loaded
	previousLoadError, previousStorageError := stateLoadError, storageError
	previousRecords, previousRunning, previousInspections := records, running, inspections
	previousRunCtx, previousStopRuns, previousRetryDelay := runCtx, stopRuns, retryDelay
	t.Cleanup(func() {
		tasks.Wait()
		configuredCredentials = previousCredentials
		hostCall, statePath, loaded = previousHost, previousPath, previousLoaded
		stateLoadError, storageError = previousLoadError, previousStorageError
		records, running, inspections = previousRecords, previousRunning, previousInspections
		runCtx, stopRuns, retryDelay = previousRunCtx, previousStopRuns, previousRetryDelay
	})
	configuredCredentials = map[string]credential{}
	statePath, loaded = filepath.Join(t.TempDir(), "state.json"), false
	stateLoadError, storageError = nil, ""
	records, running, inspections = emptyState(), map[string]*progress{}, map[string]inspection{}
	runCtx, stopRuns = context.WithCancel(context.Background())
	retryDelay = time.Millisecond
	hostCall = func(method string, _ any) (json.RawMessage, error) {
		t.Errorf("unexpected host call: %s", method)
		return nil, fmt.Errorf("unexpected host call: %s", method)
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
	hostCall = func(string, any) (json.RawMessage, error) { return json.RawMessage(`{"files":[]}`), nil }
	for path, status := range map[string]int{"/state/": http.StatusOK, "/missing": http.StatusNotFound} {
		request, _ := json.Marshal(managementRequest{Method: http.MethodGet, Path: managementBase + path})
		var env struct {
			Result managementResponse `json:"result"`
		}
		if json.Unmarshal(HandleMethod("management.handle", request), &env); env.Result.StatusCode != status {
			t.Errorf("GET %s = %d, want %d", path, env.Result.StatusCode, status)
		}
	}
}

// The page inlines its template parts, and an inline script ends at the first "</script" anywhere in its text.
func TestPage(t *testing.T) {
	page := string(uiHTML)
	variants, _ := json.Marshal(pelicanVariants)
	for _, want := range []string{"版本 v" + pluginVersion, "<code>" + html.EscapeString(candyPrompt) + "</code>", "PELICAN_VARIANTS = " + string(variants) + ";",
		`"requests":3`, `"gpt-6.1-sol":"gpt-6-astra"`, detectorLicense} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if placeholder := regexp.MustCompile(`/\*[A-Z_]+\*/|<!--[A-Z_]+-->`).FindString(page); placeholder != "" {
		t.Errorf("unresolved placeholder %s", placeholder)
	}
	if got, want := strings.Count(strings.ToLower(page), "</script"), strings.Count(uiTemplate, "</script>"); got != want {
		t.Errorf("page has %d closing script tags, its template %d", got, want)
	}
}

func TestStateCredentialPlans(t *testing.T) {
	setupTest(t)
	token := func(plan string) string {
		claims, _ := json.Marshal(map[string]any{"https://api.openai.com/auth": map[string]string{"chatgpt_plan_type": plan}})
		return "header." + base64.RawURLEncoding.EncodeToString(claims) + ".signature"
	}
	files := []hostAuthFile{
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
		Credentials []credentialView `json:"credentials"`
	}
	if err := json.Unmarshal(response.Body, &state); err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("state response = %d, decode error = %v", response.StatusCode, err)
	}
	want := []string{"free", "plus", "prolite", "", "", ""}
	if len(state.Credentials) != len(want) {
		t.Fatalf("got %d credentials, want %d", len(state.Credentials), len(want))
	}
	for i, plan := range want {
		if state.Credentials[i].PlanType != plan || state.Credentials[i].Candies == nil {
			t.Errorf("credential %s: plan = %q, want %q; candies = %v", state.Credentials[i].ID, state.Credentials[i].PlanType, plan, state.Credentials[i].Candies)
		}
	}
	if !state.Credentials[2].Disabled {
		t.Error("disabled credential lost its status")
	}
	for _, secret := range [][]byte{[]byte("private-access-token"), []byte("id_token"), []byte("signature")} {
		if bytes.Contains(response.Body, secret) {
			t.Errorf("state response contains credential data: %s", secret)
		}
	}
}

func TestQuiesceStopsTests(t *testing.T) {
	setupTest(t)
	gate, started := make(chan struct{}), make(chan struct{}, 3)
	hostCall = streamHost(func(method string, _ any) (json.RawMessage, error) {
		if method == "host.auth.list" {
			return json.RawMessage(`{"files":[{"id":"candy","provider":"codex"},{"id":"fingerprint","provider":"codex"}]}`), nil
		}
		select {
		case started <- struct{}{}:
		default:
		}
		<-gate
		return mockModelResponse("21"), nil
	})
	defer func() { close(gate); tasks.Wait() }()
	candy := []byte(`{"credential_ids":["candy"],"model":"gpt-5.5","runs":10}`)
	fingerprint := []byte(`{"credential_ids":["fingerprint"],"model":"gpt-5.5","concurrency":2}`)
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
		stopping := runCtx.Err() != nil
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
	// Abandoned requests leave no candy record; the fingerprint run keeps what it collected.
	if len(records.Candies["candy"]) != 0 || records.Fingerprints["fingerprint"][0].Status != "cancelled" || len(running) != 0 {
		t.Fatal("quiesce did not stop the tests")
	}
	HandleMethod("plugin.reconfigure", nil)
	if runCtx.Err() != nil {
		t.Fatal("reconfiguration did not resume the plugin")
	}
}
