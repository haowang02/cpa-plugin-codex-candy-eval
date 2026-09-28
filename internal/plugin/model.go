package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type modelResponse struct {
	Answer          string
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
}

func retryableModelStatus(status int) bool {
	return status == 0 || status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

// Collection probes retry the same request at most twice, after 2s and 4s.
// Invalid model answers are evaluated by the caller and do not trigger retries.
func executeProbe(ctx context.Context, auth credential, model string, payload map[string]any, slots chan struct{}) (out modelResponse, attempts int, err error) {
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return out, attempts, ctx.Err()
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return out, attempts, ctx.Err()
		}
		var status int
		func() {
			defer func() { <-slots }()
			if ctx.Err() != nil {
				err = ctx.Err()
				return
			}
			attempts++
			var response modelResponse
			response, status, err = executeModel(auth, model, payload)
			out.Answer = response.Answer
			out.InputTokens += response.InputTokens
			out.OutputTokens += response.OutputTokens
			out.ReasoningTokens += response.ReasoningTokens
		}()
		if err == nil || !retryableModelStatus(status) || attempt == 2 {
			return
		}
		timer := time.NewTimer(time.Duration(2<<attempt) * time.Second)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return out, attempts, ctx.Err()
		}
	}
	return
}

func executeModel(auth credential, model string, payload map[string]any) (result modelResponse, status int, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("模型请求异常：%v", recovered)
		}
	}()
	if strings.TrimSpace(auth.ID) == "" || strings.TrimSpace(auth.Provider) == "" {
		return result, 0, fmt.Errorf("缺少凭证标识或提供商，无法固定路由")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return result, 0, err
	}
	raw, err := hostCall("host.model.execute", map[string]any{
		"entry_protocol": "openai-response", "exit_protocol": "openai-response",
		"model": model, "stream": false, "body": body,
		"forced_provider": auth.Provider, "auth_id": auth.ID,
	})
	if err != nil {
		var hostError *EnvelopeError
		if errors.As(err, &hostError) {
			return result, hostError.HTTPStatus, err
		}
		return result, 0, err
	}
	var response struct {
		StatusCode int    `json:"status_code"`
		Body       []byte `json:"body"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return result, 0, fmt.Errorf("解析宿主响应失败：%w", err)
	}
	var out struct {
		Status string          `json:"status"`
		Error  json.RawMessage `json:"error"`
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
	parseErr := json.Unmarshal(response.Body, &out)
	if parseErr == nil {
		result.InputTokens = out.Usage.InputTokens
		result.OutputTokens = out.Usage.OutputTokens
		result.ReasoningTokens = out.Usage.OutputTokensDetails.ReasoningTokens
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, response.StatusCode, fmt.Errorf("HTTP %d: %s", response.StatusCode, response.Body)
	}
	if parseErr != nil {
		return result, 0, fmt.Errorf("解析模型响应失败：%w", parseErr)
	}
	if (out.Status != "" && out.Status != "completed") || (len(out.Error) > 0 && string(out.Error) != "null") {
		return result, response.StatusCode, fmt.Errorf("模型响应未完成（%s）：%s", out.Status, out.Error)
	}
	var answer strings.Builder
	for _, item := range out.Output {
		if item.Type != "message" {
			continue
		}
		for _, part := range item.Content {
			if part.Type == "output_text" || part.Type == "text" {
				answer.WriteString(part.Text)
			}
		}
	}
	result.Answer = answer.String()
	return result, response.StatusCode, nil
}
