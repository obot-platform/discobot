package agentimpl

import (
	"testing"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
)

func TestResolveCurrentLeaf_PrefersActiveLeafFromConfig(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	agent := &DefaultAgent{store: store}
	threadID := "thread1"

	if err := store.SaveMessage(threadID, thread.StoredMessage{
		ID:      "root",
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "root"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessage(threadID, thread.StoredMessage{
		ID:       "leaf-a",
		ParentID: "root",
		Message:  message.Message{Role: "assistant", Parts: []message.Part{message.TextPart{Text: "a"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessage(threadID, thread.StoredMessage{
		ID:       "leaf-b",
		ParentID: "root",
		Message:  message.Message{Role: "assistant", Parts: []message.Part{message.TextPart{Text: "b"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveConfig(threadID, thread.Config{ActiveLeafID: "leaf-a"}); err != nil {
		t.Fatal(err)
	}

	leaf, err := agent.resolveCurrentLeaf(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if leaf != "leaf-a" {
		t.Fatalf("expected active leaf leaf-a, got %q", leaf)
	}
}

func TestResolveCurrentLeaf_PrefersInterruptedTurnLeaf(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	agent := &DefaultAgent{store: store}
	threadID := "thread1"

	if err := store.SaveConfig(threadID, thread.Config{ActiveLeafID: "leaf-a"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTurnState(threadID, thread.TurnState{
		ID:        "turn1",
		ThreadID:  threadID,
		Phase:     thread.PhaseWaitingForAnswer,
		LeafMsgID: "leaf-turn",
	}); err != nil {
		t.Fatal(err)
	}

	leaf, err := agent.resolveCurrentLeaf(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if leaf != "leaf-turn" {
		t.Fatalf("expected interrupted turn leaf leaf-turn, got %q", leaf)
	}
}
