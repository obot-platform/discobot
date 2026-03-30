package thread

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/obot-platform/discobot/agent-go/message"
)

func TestRunTurn_PersistsUserMessageMetadata(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread-metadata"

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "Hello!"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	meta := json.RawMessage(`{"discobot":{"kind":"hook-failure","hookName":"Go Check","exitCode":2}}`)
	chunks := collectChunks(t, RunTurn(
		context.Background(), prov, &mockExecutor{}, store,
		threadID, "", TurnConfig{
			Model:     "test-model",
			UserParts: []message.Part{message.TextPart{Text: "### Hook failed: Go Check"}},
			Metadata:  meta,
		},
	))

	var userChunk message.UserMessageChunk
	foundUserChunk := false
	for _, chunk := range chunks {
		next, ok := chunk.(message.UserMessageChunk)
		if !ok {
			continue
		}
		userChunk = next
		foundUserChunk = true
		break
	}
	if !foundUserChunk {
		t.Fatal("expected a UserMessageChunk")
	}

	var gotChunkMeta map[string]any
	if err := json.Unmarshal(userChunk.Data.Message.Metadata, &gotChunkMeta); err != nil {
		t.Fatalf("unmarshal user chunk metadata: %v", err)
	}
	var wantMeta map[string]any
	if err := json.Unmarshal(meta, &wantMeta); err != nil {
		t.Fatalf("unmarshal expected metadata: %v", err)
	}
	if !reflect.DeepEqual(gotChunkMeta, wantMeta) {
		t.Fatalf("user chunk metadata = %#v, want %#v", gotChunkMeta, wantMeta)
	}

	stored, err := store.LoadMessage(threadID, userChunk.Data.Message.ID)
	if err != nil {
		t.Fatalf("LoadMessage() failed: %v", err)
	}
	var gotStoredMeta map[string]any
	if err := json.Unmarshal(stored.Message.Metadata, &gotStoredMeta); err != nil {
		t.Fatalf("unmarshal stored metadata: %v", err)
	}
	if !reflect.DeepEqual(gotStoredMeta, wantMeta) {
		t.Fatalf("stored message metadata = %#v, want %#v", gotStoredMeta, wantMeta)
	}
}
