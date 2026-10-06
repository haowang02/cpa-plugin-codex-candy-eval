// Package plugin implements model evaluation and attribution tests for CPA credentials.
package plugin

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

const (
	pluginID       = "cpa-codex-candy-eval"
	pluginVersion  = "0.3.11"
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
var uiTemplate string

//go:embed web/style.css
var uiStyles string

//go:embed web/theme.css
var uiThemes string

//go:embed web/vendor/markdown-it.min.js
var markdownScript string

//go:embed web/vendor/markdown-it-cjk-friendly.min.js
var cjkFriendlyScript string

//go:embed web/vendor/texmath.js
var texmathScript string

//go:embed web/vendor/temml.min.js
var temmlScript string

//go:embed web/vendor/temml.css
var temmlStyles string

//go:embed web/credentials.js
var credentialScript string

//go:embed web/app.js
var appScript string

//go:embed web/catalog.js
var catalogScript string

//go:embed web/components.js
var componentsScript string

//go:embed web/candy.js
var candyScript string

//go:embed web/fingerprint.js
var fingerprintScript string

//go:embed web/modeltrace.js
var modelTraceScript string

var uiHTML = func() []byte {
	config, _ := json.Marshal(map[string]any{"modes": fingerprintModes, "default_concurrency": fingerprintDefaultConcurrency, "max_concurrency": fingerprintMaxConcurrency})
	fpScript := strings.Replace(fingerprintScript, `/*FINGERPRINT_CONFIG*/{}`, string(config), 1)
	traceConfig, _ := json.Marshal(map[string]any{"requests": traceTarget, "default_concurrency": traceDefaultConcurrency, "max_concurrency": traceMaxConcurrency})
	traceScript := strings.Replace(modelTraceScript, `/*MODELTRACE_CONFIG*/{}`, string(traceConfig), 1)
	return []byte(strings.NewReplacer(
		"/*APP_STYLES*/", temmlStyles+"\n"+uiThemes+"\n"+uiStyles,
		"<!--PLUGIN_VERSION-->", pluginVersion,
		"<!--CANDY_PROMPT-->", html.EscapeString(candyPrompt),
		"<!--MODELTRACE_LICENSE-->", "<!-- ModelTrace\n"+modelTraceLicense+"-->",
		"/*MARKDOWN_SCRIPT*/", markdownScript,
		"/*CJK_FRIENDLY_SCRIPT*/", cjkFriendlyScript,
		"/*TEXMATH_SCRIPT*/", texmathScript,
		"/*TEMML_SCRIPT*/", temmlScript,
		"/*CREDENTIALS_SCRIPT*/", credentialScript,
		"/*CATALOG_SCRIPT*/", catalogScript,
		"/*COMPONENTS_SCRIPT*/", componentsScript,
		"/*CANDY_SCRIPT*/", candyScript,
		"/*FINGERPRINT_SCRIPT*/", fpScript,
		"/*MODELTRACE_SCRIPT*/", traceScript,
		"/*APP_SCRIPT*/", appScript,
	).Replace(uiTemplate))
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
	Method string     `json:"Method"`
	Path   string     `json:"Path"`
	Query  url.Values `json:"Query"`
	Body   []byte     `json:"Body"`
}

type managementResponse struct {
	StatusCode int         `json:"StatusCode"`
	Headers    http.Header `json:"Headers,omitempty"`
	Body       []byte      `json:"Body,omitempty"`
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
				{"Method": http.MethodPost, "Path": managementBase + "/credentials/sync", "Description": "Sync configured credential identities"},
				{"Method": http.MethodGet, "Path": managementBase + "/state", "Description": "View credential test results"},
				{"Method": http.MethodPost, "Path": managementBase + "/run", "Description": "Run the candy test on credentials"},
				{"Method": http.MethodDelete, "Path": managementBase + "/results", "Description": "Clear candy test results"},
				{"Method": http.MethodPost, "Path": managementBase + "/fingerprint/run", "Description": "Collect and compare model fingerprints"},
				{"Method": http.MethodPost, "Path": managementBase + "/fingerprint/cancel", "Description": "Stop fingerprint collection"},
				{"Method": http.MethodGet, "Path": managementBase + "/fingerprint/record", "Description": "View a fingerprint record"},
				{"Method": http.MethodDelete, "Path": managementBase + "/fingerprint/results", "Description": "Clear fingerprint history"},
				{"Method": http.MethodPost, "Path": managementBase + "/modeltrace/run", "Description": "Run ModelTrace on credentials"},
				{"Method": http.MethodPost, "Path": managementBase + "/modeltrace/cancel", "Description": "Stop ModelTrace collection"},
				{"Method": http.MethodGet, "Path": managementBase + "/modeltrace/record", "Description": "View a ModelTrace record"},
				{"Method": http.MethodDelete, "Path": managementBase + "/modeltrace/results", "Description": "Clear ModelTrace history"},
				{"Method": http.MethodDelete, "Path": managementBase + "/all/results", "Description": "Clear all test history"},
			},
			"resources": []map[string]string{
				{"Path": uiPath, "Menu": "Codex 降智测试", "Description": "通过糖果测试、指纹测试和 ModelTrace 检测 CPA 凭证"},
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
	case req.Method == http.MethodPost && path == managementBase+"/credentials/sync":
		return syncCredentialsResponse(req.Body)
	case req.Method == http.MethodGet && path == managementBase+"/state":
		return stateResponse()
	case req.Method == http.MethodPost && path == managementBase+"/run":
		return candyRunResponse(req.Body)
	case req.Method == http.MethodPost && path == managementBase+"/fingerprint/run":
		return fingerprintRunResponse(req.Body)
	case req.Method == http.MethodPost && path == managementBase+"/fingerprint/cancel":
		return cancelCollectionResponse("fingerprint", req.Body)
	case req.Method == http.MethodGet && path == managementBase+"/fingerprint/record":
		return recordResponse("fingerprint", req.Query)
	case req.Method == http.MethodDelete && path == managementBase+"/fingerprint/results":
		return clearHistoryResponse("fingerprint")
	case req.Method == http.MethodDelete && path == managementBase+"/results":
		return clearHistoryResponse("candy")
	case req.Method == http.MethodPost && path == managementBase+"/modeltrace/run":
		return traceRunResponse(req.Body)
	case req.Method == http.MethodPost && path == managementBase+"/modeltrace/cancel":
		return cancelCollectionResponse("modeltrace", req.Body)
	case req.Method == http.MethodGet && path == managementBase+"/modeltrace/record":
		return recordResponse("modeltrace", req.Query)
	case req.Method == http.MethodDelete && path == managementBase+"/modeltrace/results":
		return clearHistoryResponse("modeltrace")
	case req.Method == http.MethodDelete && path == managementBase+"/all/results":
		return clearHistoryResponse("all")
	default:
		return jsonError(http.StatusNotFound, "Route not found: "+req.Method+" "+req.Path)
	}
}

func stateResponse() managementResponse {
	auths, err := credentials()
	if err != nil {
		return jsonError(http.StatusBadGateway, err.Error())
	}
	// Host calls must stay outside the results lock.
	for i := range auths {
		if auths[i].Source == credentialSourceFile && auths[i].Provider == "codex" {
			auths[i].PlanType = authPlanType(auths[i])
		}
	}
	mu.Lock()
	defer mu.Unlock()
	views := make([]credentialView, 0, len(auths))
	for _, auth := range auths {
		view := credentialView{credential: auth, Results: candyResults[auth.ID]}
		if view.Results == nil {
			view.Results = []candyResult{}
		}
		view.Running = candyRunning[auth.ID]
		view.FingerprintRunning = fingerprintRunning[auth.ID]
		view.Fingerprints = summaries(fingerprintResults[auth.ID])
		view.ModelTraceRunning = traceRunning[auth.ID]
		view.ModelTraces = summaries(traceResults[auth.ID])
		views = append(views, view)
	}
	return jsonResponse(http.StatusOK, map[string]any{"auths": views, "storage_error": storageError})
}

func summaries[T interface{ summary() T }](records []T) []T {
	out := make([]T, len(records))
	for i, record := range records {
		out[i] = record.summary()
	}
	return out
}

func Quiesce() {
	mu.Lock()
	quiescing = true
	for _, p := range fingerprintRunning {
		p.Phase = "cancelling"
		p.cancel()
	}
	for _, p := range traceRunning {
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
