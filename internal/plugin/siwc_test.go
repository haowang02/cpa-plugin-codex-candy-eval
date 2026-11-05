package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func siwcMetadata(enabled bool) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"json":{"use_siwc":%t,"siwc":{"access_token":"private-access","refresh_token":"private-refresh","client_id":"private-client"}}}`, enabled))
}

func TestSIWCProbe(t *testing.T) {
	for _, tc := range []struct {
		name, response, status, code string
		httpStatus                   int
	}{
		{"completed", `{"status":"completed","usage":{"input_tokens":3,"output_tokens":1}}`, "available", "", 0},
		{"rejected", `{"error":{"code":"subscription_sharing_user_not_eligible","message":"Not eligible"}}`, "restricted", "subscription_sharing_user_not_eligible", 403},
		{"flat rejection", `{"code":"subscription_sharing_user_not_eligible","message":"Not eligible"}`, "restricted", "subscription_sharing_user_not_eligible", 403},
		{"generic forbidden", `{"detail":"Forbidden"}`, "failed", "", 403},
		{"message is not evidence", `{"error":{"message":"subscription_sharing_user_not_eligible"}}`, "failed", "", 403},
		{"wrong route", `{"error":{"code":"subscription_sharing_route_not_supported"}}`, "failed", "subscription_sharing_route_not_supported", 403},
		{"quota", `{"error":{"code":"subscription_sharing_usage_limit_exceeded"}}`, "limited", "subscription_sharing_usage_limit_exceeded", 429},
		{"authentication", `{"error":{"message":"Expired"}}`, "auth_error", "", 401},
		{"unavailable", `{"error":{"code":"subscription_sharing_usage_unavailable"}}`, "failed", "subscription_sharing_usage_unavailable", 503},
		{"stream failure", `{"status":"failed","error":{"code":"subscription_sharing_user_not_eligible","message":"Not eligible"}}`, "restricted", "subscription_sharing_user_not_eligible", 0},
		{"error event", `{"type":"error","code":"subscription_sharing_user_not_eligible","message":"Not eligible"}`, "restricted", "subscription_sharing_user_not_eligible", 0},
		{"failed event", `{"type":"response.failed","response":{"error":{"code":"subscription_sharing_user_not_eligible"}}}`, "restricted", "subscription_sharing_user_not_eligible", 0},
		{"incomplete", `{"status":"incomplete"}`, "failed", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupTest(t)
			calls := 0
			hostCall = streamHost(func(method string, payload any) (json.RawMessage, error) {
				if method == "host.auth.get" {
					return siwcMetadata(true), nil
				}
				if method != "host.model.execute_stream" {
					t.Fatalf("unexpected call %s", method)
				}
				calls++
				request := payload.(map[string]any)
				if request["auth_id"] != "bound" || request["forced_provider"] != "codex" {
					t.Fatal("probe must pin the credential")
				}
				headers := request["headers"].(http.Header)
				if strings.Contains(headers.Get("User-Agent"), "codex") || headers.Get("Originator") != "" || headers.Get("X-OpenAI-Internal-Codex-Responses-Lite") != "" {
					t.Fatal("probe must not select the original Codex route")
				}
				var body map[string]any
				_ = json.Unmarshal(request["body"].([]byte), &body)
				if body["store"] != false || body["stream"] != true || len(body["input"].([]any)) != 1 || body["tools"] != nil {
					t.Fatalf("invalid probe body: %v", body)
				}
				if tc.httpStatus != 0 {
					return nil, &EnvelopeError{Code: "host_callback_failed", Message: tc.response, HTTPStatus: tc.httpStatus}
				}
				return json.RawMessage(tc.response), nil
			})
			r := evaluateSIWC(context.Background(), credential{ID: "bound", AuthIndex: "index", Source: credentialSourceFile, Provider: "codex"}, "model")
			if r.Status != tc.status || r.Code != tc.code || r.HTTPStatus != tc.httpStatus || calls != 1 {
				t.Fatalf("record=%+v calls=%d", r, calls)
			}
			if tc.name == "completed" && (r.InputTokens != 3 || r.OutputTokens != 1) {
				t.Fatalf("lost usage: %+v", r)
			}
		})
	}
}

func TestSIWCPreconditionsAndCredentialState(t *testing.T) {
	setupTest(t)
	auth := credential{ID: "bound", AuthIndex: "index", Source: credentialSourceFile, Provider: "codex"}
	for _, tc := range []struct {
		raw            json.RawMessage
		bound, enabled bool
	}{
		{siwcMetadata(false), true, false},
		{json.RawMessage(`{"json":{"use_siwc":true,"siwc":{"access_token":"partial"}}}`), false, true},
		{json.RawMessage(`{"json":{}}`), false, false},
	} {
		hostCall = func(method string, _ any) (json.RawMessage, error) {
			if method != "host.auth.get" {
				t.Fatal("ineligible credentials must not send model requests")
			}
			return tc.raw, nil
		}
		view := enrichCodexCredential(auth)
		if view.SIWCBound != tc.bound || view.UseSIWC != tc.enabled {
			t.Fatalf("incorrect state: %+v", view)
		}
		encoded, _ := json.Marshal(view)
		if strings.Contains(string(encoded), "private-") || strings.Contains(string(encoded), "partial") {
			t.Fatal("credential state exposed secrets")
		}
		if r := evaluateSIWC(context.Background(), auth, "model"); r.Status != "skipped" {
			t.Fatalf("record=%+v", r)
		}
	}
	metadata := mustDecode[struct {
		JSON codexMetadata `json:"json"`
	}](siwcMetadata(true)).JSON
	metadata.UseSIWC = json.RawMessage(`"true"`)
	if _, enabled := metadata.siwcState(); !enabled {
		t.Fatal("CPA accepts string switches")
	}
}

func TestSIWCSelection(t *testing.T) {
	setupTest(t)
	hostCall = streamHost(func(method string, _ any) (json.RawMessage, error) {
		switch method {
		case "host.auth.list":
			return json.RawMessage(`{"files":[{"id":"bound","auth_index":"index","provider":"codex"},{"id":"other","provider":"claude"}]}`), nil
		case "host.auth.get":
			return siwcMetadata(true), nil
		case "host.model.execute_stream":
			return mockModelResponse("OK"), nil
		default:
			t.Fatalf("unexpected call %s", method)
			return nil, nil
		}
	})
	if r := siwcRunResponse([]byte(`{"all":true,"model":"model"}`)); r.StatusCode != 200 {
		t.Fatalf("run=%s", r.Body)
	}
	tasks.Wait()
	if len(records.SIWC["bound"]) != 1 || len(records.SIWC["other"]) != 0 {
		t.Fatal("selection was not restricted to bound Codex credentials")
	}
	if r := siwcRunResponse([]byte(`{"credential_ids":["other"],"model":"model"}`)); r.StatusCode != 400 {
		t.Fatal("accepted an unrelated credential")
	}
}
