// Package plugin implements model evaluation and attribution tests for CPA credentials.
package plugin

import (
	"context"
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
	pluginID      = "cpa-codex-candy-eval"
	pluginVersion = "0.4.3"
	ABIVersion    = 1
	// rpcSchemaVersion is the version of CPA's plugin RPC contract, not of the state file.
	rpcSchemaVersion = 6
	managementBase   = "/v0/management/plugins/" + pluginID
	uiPath           = "/v0/resource/plugins/" + pluginID + "/ui"
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

//go:embed web/pelican.js
var pelicanScript string

var uiHTML = func() []byte {
	config, _ := json.Marshal(map[string]any{"requests": fingerprintTarget, "aliases": fingerprintAliases})
	fingerprint := strings.Replace(fingerprintScript, `/*FINGERPRINT_CONFIG*/{}`, string(config), 1)
	variants, _ := json.Marshal(pelicanVariants)
	pelican := strings.Replace(pelicanScript, `/*PELICAN_VARIANTS*/[]`, string(variants), 1)
	return []byte(strings.NewReplacer(
		"/*APP_STYLES*/", temmlStyles+"\n"+uiThemes+"\n"+uiStyles,
		"<!--PLUGIN_VERSION-->", pluginVersion,
		"<!--CANDY_PROMPT-->", html.EscapeString(candyPrompt),
		"<!--DETECTOR_LICENSE-->", "<!-- LM Fingerpoint Detector\n"+detectorLicense+"-->",
		"/*MARKDOWN_SCRIPT*/", markdownScript,
		"/*CJK_FRIENDLY_SCRIPT*/", cjkFriendlyScript,
		"/*TEXMATH_SCRIPT*/", texmathScript,
		"/*TEMML_SCRIPT*/", temmlScript,
		"/*CREDENTIALS_SCRIPT*/", credentialScript,
		"/*CATALOG_SCRIPT*/", catalogScript,
		"/*COMPONENTS_SCRIPT*/", componentsScript,
		"/*CANDY_SCRIPT*/", candyScript,
		"/*FINGERPRINT_SCRIPT*/", fingerprint,
		"/*PELICAN_SCRIPT*/", pelican,
		"/*APP_SCRIPT*/", appScript,
	).Replace(uiTemplate))
}()

// Lucide "candy" icon. Hosts render it in an img element, so the stroke color is fixed.
const logoSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#72787c" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><style>@media (prefers-color-scheme: dark) { :root { stroke: #9c9d9b; } }</style><path d="M10 7v10.9"/><path d="M14 6.1V17"/><path d="M16 7V3a1 1 0 0 1 1.707-.707 2.5 2.5 0 0 0 2.152.717 1 1 0 0 1 1.131 1.131 2.5 2.5 0 0 0 .717 2.152A1 1 0 0 1 21 8h-4"/><path d="M16.536 7.465a5 5 0 0 0-7.072 0l-2 2a5 5 0 0 0 0 7.07 5 5 0 0 0 7.072 0l2-2a5 5 0 0 0 0-7.07"/><path d="M8 17v4a1 1 0 0 1-1.707.707 2.5 2.5 0 0 0-2.152-.717 1 1 0 0 1-1.131-1.131 2.5 2.5 0 0 0-.717-2.152A1 1 0 0 1 3 16h4"/></svg>`

var (
	mu    sync.Mutex
	tasks sync.WaitGroup
	// runCtx carries every test run; Quiesce ends it and the next registration renews it.
	runCtx, stopRuns = context.WithCancel(context.Background())
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
		if runCtx.Err() != nil {
			runCtx, stopRuns = context.WithCancel(context.Background())
		}
		mu.Unlock()
		return okEnvelope(map[string]any{
			"schema_version": rpcSchemaVersion,
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
		registered := make([]map[string]string, len(routes))
		for i, r := range routes {
			registered[i] = map[string]string{"Method": r.method, "Path": managementBase + r.path, "Description": r.description}
		}
		return okEnvelope(map[string]any{
			"routes":    registered,
			"resources": []map[string]string{{"Path": uiPath, "Menu": "Codex 降智测试", "Description": "通过糖果题、指纹检测和画鹈鹕检测 CPA 凭证"}},
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

// route is a management endpoint below managementBase, registered with CPA and served by handleManagement.
type route struct {
	method, path, description string
	handle                    func(managementRequest) managementResponse
}

var routes = func() []route {
	withBody := func(handle func([]byte) managementResponse) func(managementRequest) managementResponse {
		return func(req managementRequest) managementResponse { return handle(req.Body) }
	}
	viewRecord := func(test string) func(managementRequest) managementResponse {
		return func(req managementRequest) managementResponse { return recordResponse(test, req.Query) }
	}
	clearRecords := func(test string) func(managementRequest) managementResponse {
		return func(managementRequest) managementResponse { return clearRecordsResponse(test) }
	}
	return []route{
		{http.MethodPost, "/credentials/sync", "Sync configured credential identities", withBody(syncCredentialsResponse)},
		{http.MethodGet, "/state", "View credentials with their test records", func(managementRequest) managementResponse { return stateResponse() }},
		{http.MethodPost, "/cancel", "Stop the tests running on credentials", withBody(cancelResponse)},
		{http.MethodDelete, "/records", "Clear the records of every test", clearRecords("all")},
		{http.MethodPost, "/candy/run", "Run the candy test on credentials", withBody(candyRunResponse)},
		{http.MethodDelete, "/candy/records", "Clear candy test records", clearRecords("candy")},
		{http.MethodPost, "/fingerprint/run", "Run fingerprint detection on credentials", withBody(fingerprintRunResponse)},
		{http.MethodGet, "/fingerprint/record", "View a fingerprint detection record", viewRecord("fingerprint")},
		{http.MethodDelete, "/fingerprint/records", "Clear fingerprint detection records", clearRecords("fingerprint")},
		{http.MethodPost, "/pelican/run", "Run the pelican test on credentials", withBody(pelicanRunResponse)},
		{http.MethodGet, "/pelican/record", "View a pelican test record", viewRecord("pelican")},
		{http.MethodDelete, "/pelican/records", "Clear pelican test records", clearRecords("pelican")},
		{http.MethodPost, "/inspection", "Inspect credentials with the candy test or fingerprint detection", withBody(inspectionRunResponse)},
		{http.MethodGet, "/inspection", "View the latest inspection of a test", func(req managementRequest) managementResponse { return inspectionResponse(req.Query.Get("test")) }},
	}
}()

func handleManagement(req managementRequest) managementResponse {
	path := strings.TrimRight(req.Path, "/")
	if req.Method == http.MethodGet && path == uiPath {
		return managementResponse{
			StatusCode: http.StatusOK,
			Headers: http.Header{
				"Content-Type":            {"text/html; charset=utf-8"},
				"Cache-Control":           {"no-store"},
				"X-Content-Type-Options":  {"nosniff"},
				"Content-Security-Policy": {"default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; img-src data:; font-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'self'"},
			},
			Body: uiHTML,
		}
	}
	for _, r := range routes {
		if req.Method == r.method && path == managementBase+r.path {
			return r.handle(req)
		}
	}
	return jsonError(http.StatusNotFound, "Route not found: "+req.Method+" "+req.Path)
}

// credentialView is a credential with the test it runs and the summaries of its records.
type credentialView struct {
	credential
	Running      *progress           `json:"running,omitempty"`
	Candies      []candyRecord       `json:"candies"`
	Fingerprints []fingerprintRecord `json:"fingerprints"`
	Pelicans     []pelicanRecord     `json:"pelicans"`
}

func stateResponse() managementResponse {
	auths, err := credentials()
	if err != nil {
		return jsonError(http.StatusBadGateway, err.Error())
	}
	// Host calls must stay outside the records lock.
	for i := range auths {
		if auths[i].Source == credentialSourceFile && auths[i].Provider == "codex" {
			auths[i].PlanType = authPlanType(auths[i])
		}
	}
	mu.Lock()
	defer mu.Unlock()
	views := make([]credentialView, len(auths))
	for i, auth := range auths {
		views[i] = credentialView{
			credential:   auth,
			Running:      running[auth.ID],
			Candies:      append([]candyRecord{}, records.Candies[auth.ID]...),
			Fingerprints: summaries(records.Fingerprints[auth.ID]),
			Pelicans:     summaries(records.Pelicans[auth.ID]),
		}
	}
	return jsonResponse(http.StatusOK, map[string]any{"credentials": views, "storage_error": storageError})
}

func summaries[T interface{ summary() T }](list []T) []T {
	out := make([]T, len(list))
	for i, r := range list {
		out[i] = r.summary()
	}
	return out
}

func Quiesce() {
	mu.Lock()
	stopRuns()
	for _, p := range running {
		p.Cancelling = true
	}
	mu.Unlock()
	tasks.Wait()
}

func mustDecode[T any](data []byte) T {
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		panic(err)
	}
	return value
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
