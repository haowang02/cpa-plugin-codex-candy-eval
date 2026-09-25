// Command cpa-codex-candy-eval is a CLIProxyAPI plugin that asks Codex auth
// files a candy counting problem (answer: 21) to spot degraded accounts.
package main

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	pluginID       = "cpa-codex-candy-eval"
	pluginVersion  = "0.1.4"
	abiVersion     = 1
	schemaVersion  = 6
	managementBase = "/v0/management/plugins/" + pluginID
	uiPath         = "/v0/resource/plugins/" + pluginID + "/ui"
	historyLimit   = 20
	maxRuns        = 10
)

// Relative to the CLIProxyAPI working directory, next to the plugin files.
var statePath = "plugins/" + pluginID + "-state.json"

// hostCall is installed by the C ABI bridge.
var hostCall func(method string, payload any) (json.RawMessage, error)

//go:embed ui.html
var uiTemplate []byte

// uiHTML is the web UI with the prompt injected, so the page shows and copies exactly what models receive.
var uiHTML = func() []byte {
	prompt, _ := json.Marshal(candyPrompt)
	return bytes.Replace(uiTemplate, []byte(`/*CANDY_PROMPT*/""`), prompt, 1)
}()

const candyPrompt = `不使用任何外部工具回答以下问题：

在一个黑色的袋子里放有三种口味的糖果，每种糖果有两种不同的形状（圆形和五角星形，不同的形状靠手感可以分辨）。现已知不同口味的糖和不同形状的数量统计如下表。参赛者需要在活动前决定摸出的糖果数目，那么，最少取出多少个糖果才能保证手中同时拥有不同形状的苹果味和桃子味的糖？（同时手中有圆形苹果味匹配五角星桃子味糖果，或者有圆形桃子味匹配五角星苹果味糖果都满足要求）

        苹果味  桃子味  西瓜味
圆形       7      9      8
五角星形   7      6      4
`

// Lucide "candy" icon (https://lucide.dev, ISC license); hosts render it through an img element.
const logoSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#72787c" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><style>@media (prefers-color-scheme: dark) { :root { stroke: #9c9d9b; } }</style><path d="M10 7v10.9"/><path d="M14 6.1V17"/><path d="M16 7V3a1 1 0 0 1 1.707-.707 2.5 2.5 0 0 0 2.152.717 1 1 0 0 1 1.131 1.131 2.5 2.5 0 0 0 .717 2.152A1 1 0 0 1 21 8h-4"/><path d="M16.536 7.465a5 5 0 0 0-7.072 0l-2 2a5 5 0 0 0 0 7.07 5 5 0 0 0 7.072 0l2-2a5 5 0 0 0 0-7.07"/><path d="M8 17v4a1 1 0 0 1-1.707.707 2.5 2.5 0 0 0-2.152-.717 1 1 0 0 1-1.131-1.131 2.5 2.5 0 0 0-.717-2.152A1 1 0 0 1 3 16h4"/></svg>`

func main() {}

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

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
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Provider string `json:"provider"`
	Email    string `json:"email"`
	Disabled bool   `json:"disabled"`
}

type result struct {
	Time            time.Time `json:"time"`
	Model           string    `json:"model"`
	Effort          string    `json:"effort"`
	OK              bool      `json:"ok"`
	Answer          string    `json:"answer,omitempty"`
	Error           string    `json:"error,omitempty"`
	InputTokens     int64     `json:"input_tokens"`
	OutputTokens    int64     `json:"output_tokens"`
	ReasoningTokens int64     `json:"reasoning_tokens"`
	DurationMS      int64     `json:"duration_ms"`
}

type progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

type authView struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Email    string    `json:"email,omitempty"`
	Disabled bool      `json:"disabled"`
	Running  *progress `json:"running,omitempty"`
	Results  []result  `json:"results"`
}

type runRequest struct {
	AuthIDs []string `json:"auth_ids"`
	All     bool     `json:"all"`
	Model   string   `json:"model"`
	Effort  string   `json:"effort"`
	Runs    int      `json:"runs"`
}

var (
	mu      sync.Mutex
	loaded  bool
	results = map[string][]result{}
	running = map[string]*progress{}
)

// handleMethod answers one host RPC call with an encoded envelope.
func handleMethod(method string, request []byte) (response []byte) {
	defer func() {
		if recovered := recover(); recovered != nil {
			response = errorEnvelope("plugin_panic", fmt.Sprint(recovered), http.StatusInternalServerError)
		}
	}()
	switch method {
	case "plugin.register", "plugin.reconfigure":
		loadState()
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
				{"Method": http.MethodGet, "Path": managementBase + "/state", "Description": "View Codex candy test results"},
				{"Method": http.MethodPost, "Path": managementBase + "/run", "Description": "Run the candy test on Codex auth files"},
				{"Method": http.MethodDelete, "Path": managementBase + "/results", "Description": "Clear Codex candy test results"},
			},
			"resources": []map[string]string{
				{"Path": uiPath, "Menu": "Codex 糖果测试", "Description": "用糖果题测试 Codex 认证文件是否降智"},
			},
		})
	case "management.handle":
		var req managementRequest
		if err := json.Unmarshal(request, &req); err != nil {
			return errorEnvelope("invalid_request", err.Error(), http.StatusBadRequest)
		}
		return okEnvelope(handleManagement(req))
	case "plugin.quiesce", "plugin.shutdown":
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
		return runResponse(req.Body)
	case req.Method == http.MethodDelete && path == managementBase+"/results":
		mu.Lock()
		defer mu.Unlock()
		results = map[string][]result{}
		saveStateLocked()
		return jsonResponse(http.StatusOK, map[string]bool{"cleared": true})
	default:
		return jsonError(http.StatusNotFound, "Route not found: "+req.Method+" "+req.Path)
	}
}

func stateResponse() managementResponse {
	auths, err := codexAuths()
	if err != nil {
		return jsonError(http.StatusBadGateway, err.Error())
	}
	mu.Lock()
	defer mu.Unlock()
	views := make([]authView, 0, len(auths))
	for _, auth := range auths {
		view := authView{ID: auth.ID, Name: auth.Name, Email: auth.Email, Disabled: auth.Disabled, Results: results[auth.ID]}
		if view.Results == nil {
			view.Results = []result{}
		}
		if p := running[auth.ID]; p != nil {
			copied := *p
			view.Running = &copied
		}
		views = append(views, view)
	}
	return jsonResponse(http.StatusOK, map[string]any{"auths": views})
}

func runResponse(body []byte) managementResponse {
	var req runRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return jsonError(http.StatusBadRequest, "请求格式错误："+err.Error())
	}
	req.Model = strings.TrimSpace(req.Model)
	req.Effort = strings.TrimSpace(req.Effort)
	if req.Model == "" {
		return jsonError(http.StatusBadRequest, "请选择模型")
	}
	req.Runs = min(max(req.Runs, 1), maxRuns)
	auths, err := codexAuths()
	if err != nil {
		return jsonError(http.StatusBadGateway, err.Error())
	}
	wanted := map[string]bool{}
	for _, id := range req.AuthIDs {
		wanted[id] = true
	}
	var targets []string
	for _, auth := range auths {
		if (req.All && !auth.Disabled) || wanted[auth.ID] {
			targets = append(targets, auth.ID)
		}
	}
	if len(targets) == 0 {
		return jsonError(http.StatusBadRequest, "没有可测试的 Codex 认证文件")
	}
	mu.Lock()
	defer mu.Unlock()
	started := 0
	for _, id := range targets {
		if running[id] != nil {
			continue
		}
		running[id] = &progress{Total: req.Runs}
		started++
		go runAuth(id, req.Model, req.Effort, req.Runs)
	}
	return jsonResponse(http.StatusOK, map[string]int{"started": started})
}

// runAuth tests one auth file serially; different auth files run in parallel.
func runAuth(id, model, effort string, runs int) {
	defer func() {
		mu.Lock()
		delete(running, id)
		mu.Unlock()
	}()
	for range runs {
		r := evaluate(id, model, effort)
		mu.Lock()
		history := append(results[id], r)
		results[id] = history[max(len(history)-historyLimit, 0):]
		running[id].Done++
		saveStateLocked()
		mu.Unlock()
	}
}

func evaluate(authID, model, effort string) result {
	r := result{Time: time.Now().UTC(), Model: model, Effort: effort}
	payload := map[string]any{"model": model, "input": candyPrompt, "stream": false}
	if effort != "" {
		payload["reasoning"] = map[string]string{"effort": effort}
	}
	body, _ := json.Marshal(payload)
	start := time.Now()
	raw, err := hostCall("host.model.execute", map[string]any{
		"entry_protocol":  "openai-response",
		"exit_protocol":   "openai-response",
		"model":           model,
		"stream":          false,
		"body":            body,
		"forced_provider": "codex",
		"auth_id":         authID,
	})
	r.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		r.Error = truncate(err.Error(), 500)
		return r
	}
	var resp struct {
		StatusCode int    `json:"status_code"`
		Body       []byte `json:"body"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		r.Error = "解析宿主响应失败：" + err.Error()
		return r
	}
	if resp.StatusCode >= 300 {
		r.Error = truncate(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, resp.Body), 500)
		return r
	}
	var out struct {
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			InputTokens         int64 `json:"input_tokens"`
			OutputTokens        int64 `json:"output_tokens"`
			OutputTokensDetails struct {
				ReasoningTokens int64 `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		r.Error = truncate("解析模型响应失败："+string(resp.Body), 500)
		return r
	}
	var answer strings.Builder
	for _, item := range out.Output {
		for _, part := range item.Content {
			if item.Type == "message" && part.Type == "output_text" {
				answer.WriteString(part.Text)
			}
		}
	}
	r.Answer = truncate(answer.String(), 4000)
	r.OK = hasStandalone21(answer.String())
	r.InputTokens = out.Usage.InputTokens
	r.OutputTokens = out.Usage.OutputTokens
	r.ReasoningTokens = out.Usage.OutputTokensDetails.ReasoningTokens
	if r.Answer == "" {
		r.Error = "模型没有返回文本"
	}
	return r
}

// hasStandalone21 reports whether "21" appears without adjacent digits.
func hasStandalone21(text string) bool {
	isDigit := func(i int) bool { return i >= 0 && i < len(text) && text[i] >= '0' && text[i] <= '9' }
	for i := 0; i+1 < len(text); i++ {
		if text[i] == '2' && text[i+1] == '1' && !isDigit(i-1) && !isDigit(i+2) {
			return true
		}
	}
	return false
}

func codexAuths() ([]authFile, error) {
	if hostCall == nil {
		return nil, fmt.Errorf("宿主回调不可用")
	}
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
		if strings.EqualFold(file.Provider, "codex") || strings.EqualFold(file.Type, "codex") {
			auths = append(auths, file)
		}
	}
	sort.Slice(auths, func(i, j int) bool { return auths[i].Name < auths[j].Name })
	return auths, nil
}

func loadState() {
	mu.Lock()
	defer mu.Unlock()
	if loaded {
		return
	}
	loaded = true
	data, err := os.ReadFile(statePath)
	if err != nil {
		return
	}
	var state struct {
		Results map[string][]result `json:"results"`
	}
	if json.Unmarshal(data, &state) == nil && state.Results != nil {
		results = state.Results
	}
}

func saveStateLocked() {
	data, _ := json.Marshal(map[string]any{"results": results})
	tmp := statePath + ".tmp"
	err := os.WriteFile(tmp, data, 0o600)
	if err == nil {
		err = os.Rename(tmp, statePath)
	}
	if err != nil && hostCall != nil {
		_, _ = hostCall("host.log", map[string]any{"level": "warn", "message": pluginID + ": save state failed: " + err.Error()})
	}
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
	data, _ := json.Marshal(envelope{OK: true, Result: raw})
	return data
}

func errorEnvelope(code, message string, status int) []byte {
	data, _ := json.Marshal(envelope{Error: &envelopeError{Code: code, Message: message, HTTPStatus: status}})
	return data
}

func jsonResponse(status int, v any) managementResponse {
	body, _ := json.Marshal(v)
	return managementResponse{StatusCode: status, Headers: http.Header{"Content-Type": {"application/json; charset=utf-8"}}, Body: body}
}

func jsonError(status int, message string) managementResponse {
	return jsonResponse(status, map[string]any{"error": map[string]string{"message": message}})
}
