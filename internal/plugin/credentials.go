package plugin

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	credentialSourceFile     = "auth_files"
	credentialSourceProvider = "ai_providers"
)

type credential struct {
	ID           string `json:"id"`
	Provider     string `json:"provider"`
	Source       string `json:"source"`
	Name         string `json:"name"`
	BaseURL      string `json:"base_url,omitempty"`
	ProviderName string `json:"provider_name,omitempty"`
	Email        string `json:"email,omitempty"`
	PlanType     string `json:"plan_type,omitempty"`
	Disabled     bool   `json:"disabled"`
	// CPA marks enabled credentials unavailable while they cool down, e.g. after exhausting quota.
	Unavailable    bool      `json:"unavailable,omitempty"`
	StatusMessage  string    `json:"status_message,omitempty"`
	NextRetryAfter time.Time `json:"next_retry_after,omitzero"`
	AuthIndex      string    `json:"-"`
}

type hostAuthFile struct {
	ID             string    `json:"id"`
	AuthIndex      string    `json:"auth_index"`
	Name           string    `json:"name"`
	Provider       string    `json:"provider"`
	Type           string    `json:"type"`
	Source         string    `json:"source"`
	Path           string    `json:"path"`
	RuntimeOnly    bool      `json:"runtime_only"`
	Account        string    `json:"account"`
	AccountType    string    `json:"account_type"`
	Email          string    `json:"email"`
	PlanType       string    `json:"plan_type"`
	Disabled       bool      `json:"disabled"`
	Unavailable    bool      `json:"unavailable"`
	StatusMessage  string    `json:"status_message"`
	NextRetryAfter time.Time `json:"next_retry_after"`
}

type credentialView struct {
	credential
	Running            *candyProgress       `json:"running,omitempty"`
	Results            []candyResult        `json:"results"`
	FingerprintRunning *fingerprintProgress `json:"fingerprint_running,omitempty"`
	Fingerprints       []fingerprintResult  `json:"fingerprints"`
	ModelTraceRunning  *traceProgress       `json:"modeltrace_running,omitempty"`
	ModelTraces        []traceResult        `json:"modeltraces"`
}

// host.auth.list omits some config-backed credentials. The UI syncs their CPA
// stable IDs, masked key previews, base URLs and group names; keys stay in CPA.
var configuredCredentials = map[string]credential{}

func syncCredentialsResponse(body []byte) managementResponse {
	var req struct {
		Credentials []credential `json:"credentials"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Credentials == nil || len(req.Credentials) > 10000 {
		return jsonError(http.StatusBadRequest, "AI 提供商凭证列表无效")
	}
	next := make(map[string]credential, len(req.Credentials))
	for _, auth := range req.Credentials {
		auth.ID = strings.TrimSpace(auth.ID)
		auth.Provider = strings.ToLower(strings.TrimSpace(auth.Provider))
		auth.Name = strings.TrimSpace(auth.Name)
		if auth.ID == "" || auth.Provider == "" || strings.ContainsAny(auth.ID+auth.Provider, "\x00\r\n") || len(auth.ID) > 512 || len(auth.Provider) > 128 ||
			len(auth.Name) > 512 || len(auth.BaseURL) > 4096 || len(auth.ProviderName) > 512 {
			return jsonError(http.StatusBadRequest, "AI 提供商凭证标识无效")
		}
		if _, exists := next[auth.ID]; exists {
			return jsonError(http.StatusBadRequest, "AI 提供商凭证标识重复")
		}
		if auth.Name == "" {
			auth.Name = auth.ID
		}
		next[auth.ID] = credential{
			ID: auth.ID, Name: auth.Name, Provider: auth.Provider, Source: credentialSourceProvider, Disabled: auth.Disabled,
			BaseURL: strings.TrimSpace(auth.BaseURL), ProviderName: strings.TrimSpace(auth.ProviderName),
		}
	}
	mu.Lock()
	configuredCredentials = next
	mu.Unlock()
	return jsonResponse(http.StatusOK, map[string]int{"synced": len(next)})
}

func credentials() ([]credential, error) {
	raw, err := hostCall("host.auth.list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("读取凭证失败：%w", err)
	}
	var list struct {
		Files []hostAuthFile `json:"files"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("解析凭证列表失败：%w", err)
	}
	if list.Files == nil {
		return nil, fmt.Errorf("CPA 返回的凭证列表无效")
	}
	byID, now := make(map[string]credential, len(list.Files)), time.Now()
	for _, file := range list.Files {
		provider := strings.ToLower(strings.TrimSpace(file.Provider))
		if provider == "" {
			provider = strings.ToLower(strings.TrimSpace(file.Type))
		}
		if strings.TrimSpace(file.ID) == "" || provider == "" {
			continue
		}
		auth := credential{ID: file.ID, AuthIndex: file.AuthIndex, Name: file.Name, Provider: provider, Email: file.Email, PlanType: file.PlanType, Disabled: file.Disabled, Source: credentialSourceFile}
		// Like CPA's scheduler, treat a cooldown as over once its retry time passes, even if the flag remains.
		if file.Unavailable && !file.Disabled && (file.NextRetryAfter.IsZero() || file.NextRetryAfter.After(now)) {
			auth.Unavailable, auth.StatusMessage, auth.NextRetryAfter = true, strings.TrimSpace(file.StatusMessage), file.NextRetryAfter
		}
		source := strings.ToLower(strings.TrimSpace(file.Source))
		if file.RuntimeOnly || source == "config" || strings.HasPrefix(source, "config:") || (source == "memory" && file.Path == "") {
			auth.Source, auth.Name, auth.Email, auth.AuthIndex, auth.PlanType = credentialSourceProvider, auth.ID, "", "", ""
			if strings.EqualFold(file.AccountType, "api_key") && strings.TrimSpace(file.Account) != "" {
				auth.Name = previewAPIKey(file.Account)
			}
		}
		if auth.Name == "" {
			auth.Name = auth.ID
		}
		byID[auth.ID] = auth
	}
	mu.Lock()
	for id, auth := range configuredCredentials {
		if runtime, exists := byID[id]; exists {
			if runtime.Source != credentialSourceProvider || runtime.Provider != auth.Provider {
				continue
			}
			auth.Disabled = auth.Disabled || runtime.Disabled
			if !auth.Disabled {
				auth.Unavailable, auth.StatusMessage, auth.NextRetryAfter = runtime.Unavailable, runtime.StatusMessage, runtime.NextRetryAfter
			}
		}
		byID[id] = auth
	}
	mu.Unlock()
	auths := make([]credential, 0, len(byID))
	for _, auth := range byID {
		auths = append(auths, auth)
	}
	sort.Slice(auths, func(i, j int) bool {
		if auths[i].Name == auths[j].Name {
			return auths[i].ID < auths[j].ID
		}
		return auths[i].Name < auths[j].Name
	})
	return auths, nil
}

func previewAPIKey(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		return strings.Repeat("*", len(value))
	}
	return value[:6] + "…" + value[len(value)-4:]
}

func selectedCredentials(ids []string, all bool) ([]credential, error) {
	auths, err := credentials()
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	selected := auths[:0]
	for _, auth := range auths {
		if !auth.Disabled && !auth.Unavailable && (all || wanted[auth.ID]) {
			selected = append(selected, auth)
		}
	}
	return selected, nil
}

const unsupportedModelMessage = "已跳过：此凭证不支持所选模型"

type runSummary struct {
	Started   int `json:"started"`
	Busy      int `json:"busy,omitempty"`
	Skipped   int `json:"skipped,omitempty"`
	Unchecked int `json:"unchecked,omitempty"`
}

// The UI supplies CPA's credential model catalogs. A missing/null catalog means
// support is unknown, not unsupported.
func credentialSupportsModel(catalog map[string][]string, id, model string) (known, supported bool) {
	models, ok := catalog[id]
	if !ok || models == nil {
		return false, false
	}
	base := model
	if strings.HasSuffix(base, ")") {
		if i := strings.LastIndex(base, "("); i >= 0 {
			base = base[:i]
		}
	}
	for _, candidate := range models {
		if candidate == model || candidate == base {
			return true, true
		}
	}
	return true, false
}

func authPlanType(auth credential) string {
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
