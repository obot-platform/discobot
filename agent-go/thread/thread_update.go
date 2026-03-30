package thread

import (
	"fmt"

	"github.com/obot-platform/discobot/agent-go/message"
)

func UpdateChunkFromConfig(threadID string, cfg Config) message.ThreadUpdateChunk {
	mode := "build"
	if cfg.PlanMode {
		mode = "plan"
	}

	return message.ThreadUpdateChunk{
		Data: message.ThreadUpdateData{
			Thread: message.ThreadUpdateInfo{
				ID:          threadID,
				Name:        cfg.Name,
				LastMessage: cfg.LastMessage,
				Model:       cfg.Model,
				Reasoning:   string(cfg.Reasoning),
				Mode:        mode,
			},
		},
	}
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
