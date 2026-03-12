package cli

import (
	"os"
	"path/filepath"
	"strings"
)

// cmdHistory stores per-session command history and persists it across restarts.
type cmdHistory struct {
	entries []string
	path    string
}

// historyView adapts cmdHistory to x/term's History interface.
// Index 0 is most-recent, while cmdHistory stores oldest→newest.
type historyView struct {
	h *cmdHistory
}

func (h historyView) Add(string) {}

func (h historyView) Len() int {
	if h.h == nil {
		return 0
	}
	return len(h.h.entries)
}

func (h historyView) At(idx int) string {
	if h.h == nil {
		panic("history unavailable")
	}
	return h.h.entries[len(h.h.entries)-1-idx]
}

// loadCmdHistory reads history from path. Missing file is not an error.
func loadCmdHistory(path string) *cmdHistory {
	h := &cmdHistory{path: path}
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			if line != "" {
				h.entries = append(h.entries, line)
			}
		}
		if len(h.entries) > maxHistoryEntries {
			h.entries = h.entries[len(h.entries)-maxHistoryEntries:]
		}
	}
	return h
}

// push appends line to the history and saves it to disk.
// Adjacent duplicates are skipped.
func (h *cmdHistory) push(line string) {
	if line == "" {
		return
	}
	if len(h.entries) > 0 && h.entries[len(h.entries)-1] == line {
		return
	}
	h.entries = append(h.entries, line)
	if len(h.entries) > maxHistoryEntries {
		h.entries = h.entries[1:]
	}
	_ = h.save()
}

func (h *cmdHistory) save() error {
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(h.path, []byte(strings.Join(h.entries, "\n")+"\n"), 0o600)
}
