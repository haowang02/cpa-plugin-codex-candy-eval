package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"time"
	"unicode"
)

type modelResponse struct {
	Answer          string
	InputTokens     int64
	OutputTokens    int64
	ReasoningTokens int64
}

func validModelName(model string, allowEffort bool) bool {
	return model != "" && len(model) <= 200 && strings.IndexFunc(model, unicode.IsControl) < 0 &&
		(allowEffort || !strings.ContainsAny(model, "()"))
}

func retryableModelStatus(status int) bool {
	return status == 0 || status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

// Collection probes retry the same request at most twice, after 2s and 4s.
// Invalid model answers are evaluated by the caller and do not trigger retries.
func executeProbe(ctx context.Context, auth credential, model string, params map[string]any, slots chan struct{}) (out modelResponse, attempts int, err error) {
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
			response, status, err = executeModel(auth, model, params)
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

// executeModel runs one Responses request on auth's provider; CPA translates it to the provider's own
// protocol. params holds the request fields besides model and stream. On failure, status is the upstream
// HTTP status, or 0 when no complete response arrived.
func executeModel(auth credential, model string, params map[string]any) (result modelResponse, status int, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("模型请求异常：%v", recovered)
		}
	}()
	if strings.TrimSpace(auth.ID) == "" || strings.TrimSpace(auth.Provider) == "" {
		return result, 0, fmt.Errorf("缺少凭证标识或提供商，无法固定路由")
	}
	request := map[string]any{"model": model, "stream": true}
	maps.Copy(request, params)
	body, err := json.Marshal(request)
	if err != nil {
		return result, 0, err
	}
	raw, err := hostCall("host.model.execute_stream", map[string]any{
		"entry_protocol": "openai-response", "exit_protocol": "openai-response",
		"model": model, "stream": true, "body": body,
		"forced_provider": auth.Provider, "auth_id": auth.ID,
	})
	if err != nil {
		var hostError *EnvelopeError
		if errors.As(err, &hostError) {
			return result, hostError.HTTPStatus, err
		}
		return result, 0, err
	}
	var stream struct {
		ID string `json:"stream_id"`
	}
	if err := json.Unmarshal(raw, &stream); err != nil || stream.ID == "" {
		return result, 0, fmt.Errorf("解析宿主响应失败：%s", raw)
	}
	// The host closes a stream by itself only once it is read to the end.
	defer hostCall("host.model.stream_close", map[string]any{"stream_id": stream.ID})
	out, err := readResponseStream(stream.ID)
	if err != nil {
		return result, 0, err
	}
	result.InputTokens = out.Usage.InputTokens
	result.OutputTokens = out.Usage.OutputTokens
	result.ReasoningTokens = out.Usage.OutputTokensDetails.ReasoningTokens
	if (out.Status != "" && out.Status != "completed") || (len(out.Error) > 0 && string(out.Error) != "null") {
		return result, http.StatusOK, fmt.Errorf("模型响应未完成（%s）：%s", out.Status, out.Error)
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
	if strings.TrimSpace(result.Answer) == "" {
		return result, http.StatusOK, fmt.Errorf("模型没有返回文本")
	}
	return result, http.StatusOK, nil
}

type responseItem struct {
	Type    string `json:"type"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

type responseBody struct {
	Status string          `json:"status"`
	Error  json.RawMessage `json:"error"`
	Output []responseItem  `json:"output"`
	Usage  struct {
		InputTokens         int64 `json:"input_tokens"`
		OutputTokens        int64 `json:"output_tokens"`
		OutputTokensDetails struct {
			ReasoningTokens int64 `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`
	} `json:"usage"`
}

// readResponseStream reads a Responses event stream to its end and returns the final response.
func readResponseStream(id string) (out responseBody, err error) {
	var events []byte
	for done := false; !done; {
		raw, err := hostCall("host.model.stream_read", map[string]any{"stream_id": id})
		if err != nil {
			return out, err
		}
		var chunk struct {
			Payload []byte `json:"payload"`
			Error   string `json:"error"`
			Done    bool   `json:"done"`
		}
		if err := json.Unmarshal(raw, &chunk); err != nil {
			return out, fmt.Errorf("解析模型流失败：%w", err)
		}
		if chunk.Error != "" {
			return out, fmt.Errorf("模型流中断：%s", chunk.Error)
		}
		// A chunk may end mid-line, or start the next field without a line break.
		if len(events) > 0 && !bytes.HasSuffix(events, []byte("\n")) && startsSSEField(chunk.Payload) {
			events = append(events, '\n')
		}
		events = append(events, chunk.Payload...)
		done = chunk.Done
	}
	var items []responseItem
	final := false
	for _, line := range bytes.Split(events, []byte("\n")) {
		data, ok := bytes.CutPrefix(bytes.TrimSpace(line), []byte("data:"))
		if !ok {
			continue
		}
		var event struct {
			Type     string          `json:"type"`
			Response json.RawMessage `json:"response"`
			Item     json.RawMessage `json:"item"`
		}
		if json.Unmarshal(data, &event) != nil {
			continue
		}
		switch event.Type {
		case "response.output_item.done":
			var item responseItem
			if json.Unmarshal(event.Item, &item) == nil {
				items = append(items, item)
			}
		case "response.completed", "response.incomplete", "response.failed":
			if err := json.Unmarshal(event.Response, &out); err != nil {
				return out, fmt.Errorf("解析模型响应失败：%w", err)
			}
			final = true
		case "error":
			return out, fmt.Errorf("模型返回错误：%s", bytes.TrimSpace(data))
		}
	}
	if !final {
		return out, fmt.Errorf("模型流在响应完成前结束")
	}
	// Codex streams the output items but may leave the final response's output empty.
	if len(out.Output) == 0 {
		out.Output = items
	}
	return out, nil
}

func startsSSEField(chunk []byte) bool {
	chunk = bytes.TrimLeft(chunk, " \t")
	for _, field := range []string{"data:", "event:", "id:", "retry:", ":"} {
		if bytes.HasPrefix(chunk, []byte(field)) {
			return true
		}
	}
	return false
}
