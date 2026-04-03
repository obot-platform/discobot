package thread

import (
	"fmt"
	"strings"

	"github.com/obot-platform/discobot/agent-go/message"
)

func UpdateChunkFromConfig(threadID string, cfg Config) message.ThreadUpdateChunk {
	// Prefer the canonical Mode.Value; default to build if empty.
	mode := strings.TrimSpace(cfg.Mode.Value)
	if mode == "" {
		mode = "build"
	}

	return message.ThreadUpdateChunk{
		Data: message.ThreadUpdateData{
			Thread: message.ThreadUpdateInfo{
				ID:           threadID,
				Name:         cfg.Name,
				LastMessage:  cfg.LastMessage,
				ErrorMessage: cfg.ErrorMessage,
				Model:        cfg.Model,
				Reasoning:    string(cfg.Reasoning),
				Mode:         mode,
				State:        string(cfg.LastTurnState),
				PromptQueue:  promptQueueToThreadUpdateInfo(cfg.PromptQueue),
			},
		},
	}
}

func promptQueueToThreadUpdateInfo(queue []QueuedPrompt) []message.ThreadQueuedPromptInfo {
	if len(queue) == 0 {
		return nil
	}
	items := make([]message.ThreadQueuedPromptInfo, 0, len(queue))
	for _, prompt := range queue {
		items = append(items, message.ThreadQueuedPromptInfo{
			ID:        prompt.ID,
			CreatedAt: prompt.CreatedAt,
			Message:   prompt.Message,
			Model:     prompt.Model,
			Reasoning: prompt.Reasoning,
			Mode:      prompt.Mode,
		})
	}
	return items
}

func YieldThreadUpdate(
	yield func(message.MessageChunk, error) bool,
	store *Store,
	threadID string,
) bool {
	cfg, err := store.LoadConfig(threadID)
	if err != nil {
		return yield(nil, fmt.Errorf("load thread config: %w", err))
	}
	return yield(UpdateChunkFromConfig(threadID, cfg), nil)
}
