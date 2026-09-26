package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type modelResponse struct {
	Answer          string
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
}

func executeModel(authID, model string, payload map[string]any) (result modelResponse, status int, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("模型请求异常：%v", recovered)
		}
	}()
	body, err := json.Marshal(payload)
	if err != nil {
		return result, 0, err
	}
	raw, err := hostCall("host.model.execute", map[string]any{
		"entry_protocol": "openai-response", "exit_protocol": "openai-response",
		"model": model, "stream": false, "body": body,
		"forced_provider": "codex", "auth_id": authID,
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
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return result, response.StatusCode, fmt.Errorf("HTTP %d: %s", response.StatusCode, response.Body)
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
	if err := json.Unmarshal(response.Body, &out); err != nil {
		return result, 0, fmt.Errorf("解析模型响应失败：%w", err)
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
	result.InputTokens = out.Usage.InputTokens
	result.OutputTokens = out.Usage.OutputTokens
	result.ReasoningTokens = out.Usage.OutputTokensDetails.ReasoningTokens
	return result, response.StatusCode, nil
}
