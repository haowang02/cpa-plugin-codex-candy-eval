package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// executeResponse sends a Responses request through CPA on the selected credential.
func executeResponse(ctx context.Context, auth credential, model string, body any, headers http.Header) (result responseBody, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("模型请求异常：%v", recovered)
		}
	}()
	if strings.TrimSpace(auth.ID) == "" || strings.TrimSpace(auth.Provider) == "" {
		return result, fmt.Errorf("缺少凭证 ID 或提供商，无法指定凭证")
	}
	// Keep <, > and & literal, as Codex sends them.
	var request bytes.Buffer
	encoder := json.NewEncoder(&request)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(body); err != nil {
		return result, err
	}
	raw, err := hostCall("host.model.execute_stream", map[string]any{
		"entry_protocol": "openai-response", "exit_protocol": "openai-response",
		"model": model, "stream": true, "body": bytes.TrimSuffix(request.Bytes(), []byte("\n")), "headers": headers,
		"forced_provider": auth.Provider, "auth_id": auth.ID,
	})
	if err != nil {
		var rejected *EnvelopeError
		if errors.As(err, &rejected) && (rejected.HTTPStatus == http.StatusRequestTimeout || rejected.HTTPStatus >= http.StatusInternalServerError) {
			err = retryableError{err}
		}
		return result, err
	}
	var stream struct {
		ID string `json:"stream_id"`
	}
	if err := json.Unmarshal(raw, &stream); err != nil || stream.ID == "" {
		return result, fmt.Errorf("解析 CPA 响应失败：%s", raw)
	}
	// The host closes a stream by itself only once it is read to the end. Closing it early ends the pending
	// read and cancels the upstream request.
	closeStream := sync.OnceFunc(func() { hostCall("host.model.stream_close", map[string]any{"stream_id": stream.ID}) })
	defer closeStream()
	stop := context.AfterFunc(ctx, closeStream)
	defer stop()
	result, err = readResponseStream(stream.ID)
	if err != nil && ctx.Err() != nil {
		err = context.Cause(ctx)
	}
	if err != nil {
		err = retryableError{err}
	}
	return result, err
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

func (r responseBody) completed() bool {
	return r.Status == "completed" && (len(r.Error) == 0 || string(r.Error) == "null")
}

func (r responseBody) tokenUsage() usage {
	return usage{r.Usage.InputTokens, r.Usage.OutputTokens, r.Usage.OutputTokensDetails.ReasoningTokens}
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
			if raw := bytes.TrimSpace(event.Response); len(raw) == 0 || raw[0] != '{' {
				return out, fmt.Errorf("模型返回了无效的完成事件")
			}
			if err := json.Unmarshal(event.Response, &out); err != nil {
				return out, fmt.Errorf("解析模型响应失败：%w", err)
			}
			status := strings.TrimPrefix(event.Type, "response.")
			if out.Status != "" && out.Status != status {
				return out, fmt.Errorf("模型完成事件与响应状态不一致")
			}
			out.Status = status
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
