package plugin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"
)

// usage counts the tokens of one or more model responses.
type usage struct {
	InputTokens     int64 `json:"input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ReasoningTokens int64 `json:"reasoning_tokens"`
}

func (u *usage) add(v usage) {
	u.InputTokens += v.InputTokens
	u.OutputTokens += v.OutputTokens
	u.ReasoningTokens += v.ReasoningTokens
}

type modelResponse struct {
	Answer string
	usage
}

func validModelName(model string, allowEffort bool) bool {
	return model != "" && len(model) <= 200 && strings.IndexFunc(model, unicode.IsControl) < 0 &&
		(allowEffort || !strings.ContainsAny(model, "()"))
}

// Time limits of an attempt, as a model stuck repeating itself streams until the upstream cuts it off.
// Fingerprint challenges normally answer within a minute.
const (
	challengeTimeout = 3 * time.Minute
	answerTimeout    = 10 * time.Minute
)

// A request is sent at most modelAttempts times. The first retry waits retryDelay, each later one twice as
// long.
const modelAttempts = 3

var retryDelay = 2 * time.Second

// retryableError marks a failure that sending the request again may avoid.
type retryableError struct{ error }

func (e retryableError) Unwrap() error { return e.error }

// askCodex retries transient failures and answers rejected by accept, summing usage across attempts.
// CPA handles retries before streaming starts; the plugin also retries interrupted or unusable streams.
func askCodex(ctx context.Context, auth credential, model, effort, instructions, prompt string, limit time.Duration, accept func(answer string) error) (out modelResponse, attempts int, err error) {
	if effort == "none" {
		effort = ""
	}
	body, headers := codexTurn(auth.ID, model, effort, instructions, prompt)
	timeout := fmt.Errorf("模型 %d 分钟内未完成回答，已中断请求", int(limit.Minutes()))
	for delay := retryDelay; ; delay *= 2 {
		if err = ctx.Err(); err != nil {
			return
		}
		attempts++
		requestCtx, cancel := context.WithTimeoutCause(ctx, limit, timeout)
		var response modelResponse
		response, err = executeModel(requestCtx, auth, model, body, headers)
		cancel()
		out.Answer = response.Answer
		out.add(response.usage)
		if err == nil && accept != nil {
			if err = accept(response.Answer); err != nil {
				err = retryableError{err}
			}
		}
		if err == nil || attempts == modelAttempts || !errors.As(err, new(retryableError)) {
			return
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return out, attempts, ctx.Err()
		}
	}
}

// executeModel requires a completed text answer; unusable answers may be retried.
func executeModel(ctx context.Context, auth credential, model string, body any, headers http.Header) (result modelResponse, err error) {
	out, err := executeResponse(ctx, auth, model, body, headers)
	if err != nil {
		return result, err
	}
	result, err = answerOf(out)
	if err != nil {
		err = retryableError{err}
	}
	return
}

// answerOf returns the answer text and usage of a final response. Any final response reports usage,
// complete or not.
func answerOf(out responseBody) (result modelResponse, err error) {
	result.usage = out.tokenUsage()
	if !out.completed() {
		return result, fmt.Errorf("模型响应未完成（%s）：%s", out.Status, out.Error)
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
	if strings.TrimSpace(answer.String()) == "" {
		// Codex turns list tools, and items such as function_call or custom_tool_call request one.
		if slices.ContainsFunc(out.Output, func(item responseItem) bool { return strings.HasSuffix(item.Type, "_call") }) {
			return result, fmt.Errorf("模型调用了工具，没有直接回答")
		}
		return result, fmt.Errorf("模型未返回文本")
	}
	result.Answer = answer.String()
	return result, nil
}
