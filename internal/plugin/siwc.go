package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
)

const siwcTimeout = time.Minute
const siwcOffMessage = "请先开启「使用 SIWC」；关闭时 CPA 会使用原始 Codex 通道，无法进行 SIWC 检测"

type siwcRecord struct {
	record
	Code       string `json:"code,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
}

func siwcRunResponse(body []byte) managementResponse {
	var req runRequest
	if json.Unmarshal(body, &req) != nil {
		return jsonError(http.StatusBadRequest, "请求格式错误")
	}
	req.Model = strings.TrimSpace(req.Model)
	if !validModelName(req.Model, false) {
		return jsonError(http.StatusBadRequest, "请选择模型")
	}
	auths, err := selectedCredentials(req.CredentialIDs, req.All)
	if err != nil {
		return jsonError(http.StatusBadGateway, err.Error())
	}
	auths = slices.DeleteFunc(auths, func(auth credential) bool {
		return auth.Source != credentialSourceFile || auth.Provider != "codex"
	})
	return startCredentialRuns("siwc", req, auths, 1, func(id string) {
		records.SIWC.add(id, siwcRecord{record: skippedRecord(req.Model)})
	}, func(ctx context.Context, auth credential, p *progress) {
		r := evaluateSIWC(ctx, auth, req.Model)
		if ctx.Err() != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		records.SIWC.add(auth.ID, r)
		p.Done++
		_ = saveStateLocked()
	})
}

func evaluateSIWC(ctx context.Context, auth credential, model string) siwcRecord {
	started := time.Now()
	r := siwcRecord{record: newRecord(model, started)}
	// Check persisted credentials immediately before selecting the upstream route.
	metadata, err := readCodexMetadata(auth)
	if err != nil {
		r.Status, r.Error = "failed", err.Error()
		return r
	}
	bound, enabled := metadata.siwcState()
	if !bound {
		r.Status, r.Error = "skipped", "凭证尚未绑定 SIWC"
		return r
	}
	if !enabled {
		r.Status, r.Error = "skipped", siwcOffMessage
		return r
	}
	ctx, cancel := context.WithTimeout(ctx, siwcTimeout)
	defer cancel()
	// No Codex client markers or Responses Lite context: those select the outer OAuth partition.
	body := map[string]any{
		"model": model, "store": false, "stream": true,
		"input": []map[string]string{{"role": "user", "content": "Reply with OK."}},
	}
	out, err := executeResponse(ctx, auth, model, body, http.Header{
		"Content-Type": {"application/json"}, "Accept": {"text/event-stream"},
		"User-Agent": {"cpa-siwc-check/" + pluginVersion},
	})
	r.DurationMS = time.Since(started).Milliseconds()
	r.usage = out.tokenUsage()
	if err == nil && out.completed() {
		r.Status = "available"
		return r
	}
	message := string(out.Error)
	if err != nil {
		message = err.Error()
		var envelope *EnvelopeError
		if errors.As(err, &envelope) {
			message, r.HTTPStatus = envelope.Message, envelope.HTTPStatus
		}
	}
	r.Code, r.Error = siwcErrorDetails(message)
	r.Status = "failed"
	switch r.Code {
	case "subscription_sharing_user_not_eligible":
		r.Status = "restricted"
	case "subscription_sharing_usage_limit_exceeded":
		r.Status = "limited"
	case "subscription_sharing_invalid_user":
		r.Status = "auth_error"
	}
	if r.Status == "failed" {
		switch r.HTTPStatus {
		case 401:
			r.Status = "auth_error"
		case 429:
			r.Status = "limited"
		}
	}
	if r.Error == "" {
		r.Error = "请求未完成，无法判断 SIWC 是否可用"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		r.Error = "SIWC 检测超时，请稍后重试"
	}
	return r
}

// Only a structured code is evidence. A generic 403 or a message mentioning the code is not.
func siwcErrorDetails(message string) (code, detail string) {
	var data struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Detail  string          `json:"detail"`
		Error   json.RawMessage `json:"error"`
	}
	if i := strings.IndexByte(message, '{'); i >= 0 && json.Unmarshal([]byte(message[i:]), &data) == nil {
		if len(data.Error) > 0 && string(data.Error) != "null" {
			return siwcErrorDetails(string(data.Error))
		}
		if data.Message == "" {
			data.Message = data.Detail
		}
		return truncate(data.Code, 120), truncate(data.Message, 500)
	}
	if strings.TrimSpace(message) == "null" {
		return "", ""
	}
	return "", truncate(message, 500)
}
