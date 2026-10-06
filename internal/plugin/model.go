package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
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

// Time limits of a model request, as a model stuck repeating itself streams until the upstream cuts it off.
// Probes normally answer within a minute.
const (
	probeTimeout  = 3 * time.Minute
	answerTimeout = 10 * time.Minute
)

func withModelTimeout(ctx context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeoutCause(ctx, limit, fmt.Errorf("模型 %d 分钟内未完成回答，已中断请求", int(limit.Minutes())))
}

// Collection probes retry the same request at most twice, after 2s and 4s.
// Invalid model answers are evaluated by the caller and do not trigger retries.
func executeProbe(ctx context.Context, auth credential, model string, body any, headers http.Header, slots chan struct{}) (out modelResponse, attempts int, err error) {
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
			requestCtx, cancel := withModelTimeout(ctx, probeTimeout)
			defer cancel()
			var response modelResponse
			response, status, err = executeModel(requestCtx, auth, model, body, headers)
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

// executeModel sends a streaming Responses request body with headers on auth's provider; CPA translates
// it to the provider's own protocol. When ctx ends first, the request is abandoned with ctx's cause. On
// failure, status is the upstream HTTP status, or 0 when no complete response arrived.
func executeModel(ctx context.Context, auth credential, model string, body any, headers http.Header) (result modelResponse, status int, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("模型请求异常：%v", recovered)
		}
	}()
	if strings.TrimSpace(auth.ID) == "" || strings.TrimSpace(auth.Provider) == "" {
		return result, 0, fmt.Errorf("缺少凭证 ID 或提供商，无法指定凭证")
	}
	// Keep <, > and & literal, as Codex sends them.
	var request bytes.Buffer
	encoder := json.NewEncoder(&request)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(body); err != nil {
		return result, 0, err
	}
	raw, err := hostCall("host.model.execute_stream", map[string]any{
		"entry_protocol": "openai-response", "exit_protocol": "openai-response",
		"model": model, "stream": true, "body": bytes.TrimSuffix(request.Bytes(), []byte("\n")), "headers": headers,
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
		return result, 0, fmt.Errorf("解析 CPA 响应失败：%s", raw)
	}
	// The host closes a stream by itself only once it is read to the end. Closing it early ends the pending
	// read and cancels the upstream request.
	closeStream := sync.OnceFunc(func() { hostCall("host.model.stream_close", map[string]any{"stream_id": stream.ID}) })
	defer closeStream()
	stop := context.AfterFunc(ctx, closeStream)
	defer stop()
	out, err := readResponseStream(stream.ID)
	if err != nil {
		if ctx.Err() != nil {
			err = context.Cause(ctx)
		}
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
		// Codex turns list tools, and items such as function_call or custom_tool_call request one.
		if slices.ContainsFunc(out.Output, func(item responseItem) bool { return strings.HasSuffix(item.Type, "_call") }) {
			return result, http.StatusOK, fmt.Errorf("模型调用了工具，没有直接回答")
		}
		return result, http.StatusOK, fmt.Errorf("模型未返回文本")
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
			return out, fmt.Errorf("解析模型响应流失败：%w", err)
		}
		if chunk.Error != "" {
			return out, fmt.Errorf("模型响应流中断：%s", chunk.Error)
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
		return out, fmt.Errorf("模型响应流在完成前中断")
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
