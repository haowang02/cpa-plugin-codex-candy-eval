package plugin

import (
	"encoding/hex"
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
	request, headers := codexTurn("auth", "gpt-5.5", "low", "q")
	thread, metadata := request.PromptCacheKey, request.ClientMetadata
	if headers.Get("Session-Id") != thread || headers.Get("Thread-Id") != thread || metadata.ThreadID != thread ||
		headers.Get("X-Codex-Turn-Metadata") != metadata.TurnMetadata || request.Input[len(request.Input)-1].Content[0].Text != "q" {
		t.Fatalf("inconsistent turn: headers = %v, metadata = %+v", headers, metadata)
	}
	again, _ := codexTurn("auth", "gpt-5.5", "low", "q")
	other, _ := codexTurn("other", "gpt-5.5", "low", "q")
	if again.PromptCacheKey == thread || again.ClientMetadata.InstallationID != metadata.InstallationID || other.ClientMetadata.InstallationID == metadata.InstallationID {
		t.Fatal("each turn needs a new thread on its credential's stable installation")
	}
}
