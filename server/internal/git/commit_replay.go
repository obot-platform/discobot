package git

import (
	"encoding/json"
	"fmt"
	"time"
)

type commitReplayBundle struct {
	Version int                 `json:"version"`
	Commits []commitReplayEntry `json:"commits"`
}

type commitReplayEntry struct {
	SHA            string             `json:"sha,omitempty"`
	Message        string             `json:"message"`
	AuthorName     string             `json:"authorName"`
	AuthorEmail    string             `json:"authorEmail"`
	AuthorDate     time.Time          `json:"authorDate"`
	CommitterName  string             `json:"committerName,omitempty"`
	CommitterEmail string             `json:"committerEmail,omitempty"`
	CommitterDate  *time.Time         `json:"committerDate,omitempty"`
	Changes        []commitFileChange `json:"changes"`
}

type commitFileChange struct {
	Path            string `json:"path"`
	OldPath         string `json:"oldPath,omitempty"`
	Status          string `json:"status"`
	Binary          bool   `json:"binary,omitempty"`
	PreviousContent []byte `json:"previousContent,omitempty"`
	Content         []byte `json:"content,omitempty"`
}

func decodeCommitReplayBundle(payload []byte) (*commitReplayBundle, error) {
	var bundle commitReplayBundle
	if err := json.Unmarshal(payload, &bundle); err != nil {
		return nil, fmt.Errorf("failed to decode commit replay bundle: %w", err)
	}

	if len(bundle.Commits) == 0 {
		return nil, fmt.Errorf("commit replay bundle is empty")
	}

	for i, commit := range bundle.Commits {
		if commit.Message == "" {
			return nil, fmt.Errorf("commit replay bundle entry %d is missing a message", i)
		}
		if commit.AuthorName == "" || commit.AuthorEmail == "" {
			return nil, fmt.Errorf("commit replay bundle entry %d is missing author metadata", i)
		}
		if len(commit.Changes) == 0 {
			return nil, fmt.Errorf("commit replay bundle entry %d has no changes", i)
		}
		for j, change := range commit.Changes {
			switch change.Status {
			case "added":
				if change.Path == "" {
					return nil, fmt.Errorf("commit replay bundle entry %d change %d is missing path", i, j)
				}
			case "modified", "deleted":
				if change.Path == "" {
					return nil, fmt.Errorf("commit replay bundle entry %d change %d is missing path", i, j)
				}
			case "renamed":
				if change.Path == "" || change.OldPath == "" {
					return nil, fmt.Errorf("commit replay bundle entry %d change %d is missing rename paths", i, j)
				}
			default:
				return nil, fmt.Errorf("commit replay bundle entry %d change %d has unsupported status %q", i, j, change.Status)
			}
		}
	}

	return &bundle, nil
}
