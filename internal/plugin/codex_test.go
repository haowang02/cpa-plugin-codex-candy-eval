package plugin

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestCodexPrefixMatchesCapturedThread(t *testing.T) {
	var thread uuid
	raw, _ := hex.DecodeString(strings.ReplaceAll("01a1062a-f13e-7d92-8bbf-14512276dc3e", "-", ""))
	copy(thread[:], raw)
	prefix := codexPrefix(thread)
	if prefix[0].ID != "at_d7b9f801-e7d7-5396-a0de-11a7ae0dc033" || prefix[1].ID != "msg_fbcd7b80-1e9d-55e5-ad1e-992e8accac69" {
		t.Fatalf("prefix IDs = %s, %s", prefix[0].ID, prefix[1].ID)
	}
}

func TestCodexTurn(t *testing.T) {
	turn := func(authID string) (request codexRequest, metadata codexTurnMetadata, headers http.Header) {
		body, headers, err := codexTurn(authID, "gpt-5.5", "low", "q")
		if err != nil || !bytes.Contains(body, []byte("<environment_context>")) {
			t.Fatalf("body = %.200s, err = %v", body, err)
		}
		_ = json.Unmarshal(body, &request)
		_ = json.Unmarshal([]byte(headers.Get("X-Codex-Turn-Metadata")), &metadata)
		return request, metadata, headers
	}
	request, metadata, headers := turn("auth")
	thread, client := request.PromptCacheKey, request.ClientMetadata
	last := request.Input[len(request.Input)-1]
	if headers.Get("Session-Id") != thread || headers.Get("Thread-Id") != thread || headers.Get("X-Client-Request-Id") != thread ||
		headers.Get("X-Codex-Window-Id") != thread+":0" || client.ThreadID != thread || metadata.ThreadID != thread ||
		client.TurnMetadata != headers.Get("X-Codex-Turn-Metadata") || client.InstallationID != metadata.InstallationID ||
		metadata.ReasoningEffort != "low" || last.Content[0].Text != "q" || last.Metadata.TurnID != metadata.TurnID {
		t.Fatalf("inconsistent turn: headers = %v, client metadata = %+v", headers, client)
	}
	again, againMetadata, _ := turn("auth")
	_, otherMetadata, _ := turn("other")
	if again.PromptCacheKey == thread || againMetadata.InstallationID != metadata.InstallationID || otherMetadata.InstallationID == metadata.InstallationID {
		t.Fatal("each turn needs a new thread on its credential's stable installation")
	}
}
