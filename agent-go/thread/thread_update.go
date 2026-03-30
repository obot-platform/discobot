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
				ID:          threadID,
				Name:        cfg.Name,
				LastMessage: cfg.LastMessage,
				Model:       cfg.Model,
				Reasoning:   string(cfg.Reasoning),
				Mode:        mode,
				State:       string(cfg.LastTurnState),
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
