// Package plugin implements candy and fingerprint tests for Codex accounts.
package plugin

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

const (
	pluginID       = "cpa-codex-candy-eval"
	pluginVersion  = "0.1.9"
	ABIVersion     = 1
	schemaVersion  = 6
	managementBase = "/v0/management/plugins/" + pluginID
	uiPath         = "/v0/resource/plugins/" + pluginID + "/ui"
)

var hostCall func(method string, payload any) (json.RawMessage, error)

func SetHostCall(call func(string, any) (json.RawMessage, error)) {
	hostCall = call
}

//go:embed web/ui.html
var uiTemplate []byte

var uiHTML = func() []byte {
	models := make([]string, 0, len(fingerprintBaselines))
	for _, baseline := range fingerprintBaselines {
		models = append(models, baseline.Model)
	}
	config, _ := json.Marshal(map[string]any{"models": models, "modes": fingerprintModes, "default_concurrency": fingerprintDefaultConcurrency, "max_concurrency": fingerprintMaxConcurrency})
	return bytes.Replace(uiTemplate, []byte(`/*FINGERPRINT_CONFIG*/{}`), config, 1)
}()

// Lucide "candy" icon. Hosts render it in an img element, so the stroke color is fixed.
const logoSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#72787c" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><style>@media (prefers-color-scheme: dark) { :root { stroke: #9c9d9b; } }</style><path d="M10 7v10.9"/><path d="M14 6.1V17"/><path d="M16 7V3a1 1 0 0 1 1.707-.707 2.5 2.5 0 0 0 2.152.717 1 1 0 0 1 1.131 1.131 2.5 2.5 0 0 0 .717 2.152A1 1 0 0 1 21 8h-4"/><path d="M16.536 7.465a5 5 0 0 0-7.072 0l-2 2a5 5 0 0 0 0 7.07 5 5 0 0 0 7.072 0l2-2a5 5 0 0 0 0-7.07"/><path d="M8 17v4a1 1 0 0 1-1.707.707 2.5 2.5 0 0 0-2.152-.717 1 1 0 0 1-1.131-1.131 2.5 2.5 0 0 0-.717-2.152A1 1 0 0 1 3 16h4"/></svg>`

var (
	mu        sync.Mutex
	tasks     sync.WaitGroup
	quiescing bool
)

type Envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *EnvelopeError  `json:"error,omitempty"`
}

type EnvelopeError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

func (e *EnvelopeError) Error() string { return e.Code + ": " + e.Message }

type managementRequest struct {
	Method string `json:"Method"`
	Path   string `json:"Path"`
	Body   []byte `json:"Body"`
}

type managementResponse struct {
	StatusCode int         `json:"StatusCode"`
	Headers    http.Header `json:"Headers,omitempty"`
	Body       []byte      `json:"Body,omitempty"`
}

type authFile struct {
	ID        string `json:"id"`
	AuthIndex string `json:"auth_index"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Email     string `json:"email"`
	PlanType  string `json:"plan_type,omitempty"`
	Disabled  bool   `json:"disabled"`
}

type authView struct {
	ID                 string               `json:"id"`
	Name               string               `json:"name"`
	Email              string               `json:"email,omitempty"`
	PlanType           string               `json:"plan_type,omitempty"`
	Disabled           bool                 `json:"disabled"`
	Running            *candyProgress       `json:"running,omitempty"`
	Results            []candyResult        `json:"results"`
	FingerprintRunning *fingerprintProgress `json:"fingerprint_running,omitempty"`
	Fingerprints       []fingerprintResult  `json:"fingerprints"`
}

func HandleMethod(method string, request []byte) (response []byte) {
	defer func() {
		if recovered := recover(); recovered != nil {
			response = errorEnvelope("plugin_panic", fmt.Sprint(recovered), http.StatusInternalServerError)
		}
	}()
	switch method {
	case "plugin.register", "plugin.reconfigure":
		loadState()
		mu.Lock()
		quiescing = false
		mu.Unlock()
		return okEnvelope(map[string]any{
			"schema_version": schemaVersion,
			"metadata": map[string]any{
				"Name":             pluginID,
				"Version":          pluginVersion,
				"Author":           "haowang02",
				"GitHubRepository": "https://github.com/haowang02/cpa-plugin-codex-candy-eval",
				"Logo":             "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(logoSVG)),
				"ConfigFields":     []any{},
			},
			"capabilities": map[string]bool{"management_api": true},
		})
	case "management.register":
		return okEnvelope(map[string]any{
			"routes": []map[string]string{
				{"Method": http.MethodGet, "Path": managementBase + "/state", "Description": "View Codex test results"},
				{"Method": http.MethodPost, "Path": managementBase + "/run", "Description": "Run the candy test on Codex auth files"},
				{"Method": http.MethodDelete, "Path": managementBase + "/results", "Description": "Clear Codex candy test results"},
				{"Method": http.MethodPost, "Path": managementBase + "/fingerprint/run", "Description": "Collect and compare Codex fingerprints"},
				{"Method": http.MethodPost, "Path": managementBase + "/fingerprint/cancel", "Description": "Stop fingerprint collection"},
				{"Method": http.MethodDelete, "Path": managementBase + "/fingerprint/results", "Description": "Clear fingerprint history"},
			},
			"resources": []map[string]string{
				{"Path": uiPath, "Menu": "Codex 降智测试", "Description": "通过糖果题与模型指纹测试 Codex 认证文件"},
			},
		})
	case "management.handle":
		var req managementRequest
		if err := json.Unmarshal(request, &req); err != nil {
			return errorEnvelope("invalid_request", err.Error(), http.StatusBadRequest)
		}
		return okEnvelope(handleManagement(req))
	case "plugin.quiesce":
		Quiesce()
		return okEnvelope(map[string]any{})
	default:
		return errorEnvelope("unknown_method", "Unsupported plugin method: "+method, http.StatusNotFound)
	}
}

func handleManagement(req managementRequest) managementResponse {
	path := strings.TrimRight(req.Path, "/")
	switch {
	case req.Method == http.MethodGet && path == uiPath:
		return managementResponse{
			StatusCode: http.StatusOK,
			Headers: http.Header{
				"Content-Type":            {"text/html; charset=utf-8"},
				"Cache-Control":           {"no-store"},
				"X-Content-Type-Options":  {"nosniff"},
				"Content-Security-Policy": {"default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'self'"},
			},
			Body: uiHTML,
		}
	case req.Method == http.MethodGet && path == managementBase+"/state":
		return stateResponse()
	case req.Method == http.MethodPost && path == managementBase+"/run":
		return candyRunResponse(req.Body)
	case req.Method == http.MethodPost && path == managementBase+"/fingerprint/run":
		return fingerprintRunResponse(req.Body)
	case req.Method == http.MethodPost && path == managementBase+"/fingerprint/cancel":
		return fingerprintCancelResponse(req.Body)
	case req.Method == http.MethodDelete && path == managementBase+"/fingerprint/results":
		return clearHistoryResponse(true)
	case req.Method == http.MethodDelete && path == managementBase+"/results":
		return clearHistoryResponse(false)
	default:
		return jsonError(http.StatusNotFound, "Route not found: "+req.Method+" "+req.Path)
	}
}

func stateResponse() managementResponse {
	auths, err := codexAuths()
	if err != nil {
		return jsonError(http.StatusBadGateway, err.Error())
	}
	// Host calls must stay outside the results lock.
	for i := range auths {
		auths[i].PlanType = authPlanType(auths[i])
	}
	mu.Lock()
	defer mu.Unlock()
	views := make([]authView, 0, len(auths))
	for _, auth := range auths {
		view := authView{ID: auth.ID, Name: auth.Name, Email: auth.Email, PlanType: auth.PlanType, Disabled: auth.Disabled, Results: candyResults[auth.ID]}
		if view.Results == nil {
			view.Results = []candyResult{}
		}
		view.Running = candyRunning[auth.ID]
		view.FingerprintRunning = fingerprintRunning[auth.ID]
		view.Fingerprints = fingerprintResults[auth.ID]
		if view.Fingerprints == nil {
			view.Fingerprints = []fingerprintResult{}
		}
		views = append(views, view)
	}
	return jsonResponse(http.StatusOK, map[string]any{"auths": views, "storage_error": storageError})
}

func authPlanType(auth authFile) string {
	if plan := strings.TrimSpace(auth.PlanType); plan != "" {
		return plan
	}
	if auth.AuthIndex == "" {
		return ""
	}
	raw, err := hostCall("host.auth.get", map[string]string{"auth_index": auth.AuthIndex})
	if err != nil {
		return ""
	}
	var file struct {
		JSON struct {
			PlanType string `json:"plan_type"`
			IDToken  string `json:"id_token"`
		} `json:"json"`
	}
	if json.Unmarshal(raw, &file) != nil {
		return ""
	}
	if plan := strings.TrimSpace(file.JSON.PlanType); plan != "" {
		return plan
	}
	parts := strings.Split(file.JSON.IDToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ""
	}
	// These claims are used for a display label, never for authorization.
	var claims struct {
		Auth struct {
			PlanType string `json:"chatgpt_plan_type"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	return strings.TrimSpace(claims.Auth.PlanType)
}

func codexAuths() ([]authFile, error) {
	raw, err := hostCall("host.auth.list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("读取认证文件失败：%w", err)
	}
	var list struct {
		Files []authFile `json:"files"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("解析认证文件列表失败：%w", err)
	}
	auths := list.Files[:0]
	for _, file := range list.Files {
		if strings.EqualFold(file.Provider, "codex") {
			auths = append(auths, file)
		}
	}
	sort.Slice(auths, func(i, j int) bool { return auths[i].Name < auths[j].Name })
	return auths, nil
}

func selectedCodexAuths(ids []string, all bool) ([]authFile, error) {
	auths, err := codexAuths()
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	selected := auths[:0]
	for _, auth := range auths {
		if !auth.Disabled && (all || wanted[auth.ID]) {
			selected = append(selected, auth)
		}
	}
	return selected, nil
}

func Quiesce() {
	mu.Lock()
	quiescing = true
	for _, p := range fingerprintRunning {
		p.Phase = "cancelling"
		p.cancel()
	}
	mu.Unlock()
	tasks.Wait()
}

func truncate(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

func okEnvelope(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		return errorEnvelope("encode_failed", err.Error(), http.StatusInternalServerError)
	}
	data, _ := json.Marshal(Envelope{OK: true, Result: raw})
	return data
}

func errorEnvelope(code, message string, status int) []byte {
	data, _ := json.Marshal(Envelope{Error: &EnvelopeError{Code: code, Message: message, HTTPStatus: status}})
	return data
}

func jsonResponse(status int, v any) managementResponse {
	body, err := json.Marshal(v)
	if err != nil {
		return jsonError(http.StatusInternalServerError, "编码响应失败："+err.Error())
	}
	return managementResponse{StatusCode: status, Headers: http.Header{"Content-Type": {"application/json; charset=utf-8"}}, Body: body}
}

func jsonError(status int, message string) managementResponse {
	return jsonResponse(status, map[string]any{"error": map[string]string{"message": message}})
}
