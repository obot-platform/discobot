package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/agentimpl"
	"github.com/obot-platform/discobot/agent-go/internal/config"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
)

// threadSummary holds display metadata for a single thread.
type threadSummary struct {
	id      string
	modTime time.Time
	preview string // last user message text, truncated
	pending bool   // has a pending AskUserQuestion
}

func normalizeCWD(path string) string {
	if path == "" {
		return ""
	}
	path = filepath.Clean(path)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return path
}

func threadExists(threadsDir, threadID string) bool {
	if strings.TrimSpace(threadID) == "" {
		return false
	}
	fi, err := os.Stat(filepath.Join(threadsDir, threadID))
	return err == nil && fi.IsDir()
}

func startupCommandHints(store *thread.Store, cfg *config.Config, threadID string) (showResume bool, showHistory bool) {
	if threadExists(cfg.ThreadsDir, threadID) {
		showHistory = true
	}

	threadIDs, err := store.ListThreads()
	if err != nil || len(threadIDs) == 0 {
		return false, showHistory
	}

	targetCWD := normalizeCWD(cfg.AgentCwd)
	matchingCWD := 0
	currentMatchesCWD := false
	for _, id := range threadIDs {
		threadCfg, err := store.LoadConfig(id)
		if err != nil || strings.TrimSpace(threadCfg.CWD) == "" {
			continue
		}
		if normalizeCWD(threadCfg.CWD) != targetCWD {
			continue
		}
		matchingCWD++
		if id == threadID {
			currentMatchesCWD = true
		}
	}

	if currentMatchesCWD {
		showResume = matchingCWD > 1
	} else {
		showResume = matchingCWD > 0
	}
	return showResume, showHistory
}

func selectInitialThreadID(_ *thread.Store, cfg *config.Config, forceNew bool, resumeID string) string {
	if forceNew {
		return "thread-" + agent.GenerateID()
	}

	if strings.TrimSpace(resumeID) != "" {
		return resumeID
	}

	// Respect explicit SESSION_ID so advanced workflows remain deterministic.
	if cfg.SessionID != "" && cfg.SessionID != "default" {
		return cfg.SessionID
	}

	// CLI starts in a fresh thread by default. Existing threads are still
	// available via /resume.
	return "thread-" + agent.GenerateID()
}

// handleResumeCommand lists available threads and lets the user select one.
// Returns the selected thread ID, or currentThreadID if the user cancels.
func handleResumeCommand(_ context.Context, a *agentimpl.DefaultAgent, store *thread.Store, cfg *config.Config, currentThreadID string) string {
	threadIDs, err := a.ListThreads()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing threads: %v\n", err)
		return currentThreadID
	}
	if len(threadIDs) == 0 {
		fmt.Fprintln(os.Stderr, "No threads found.")
		return currentThreadID
	}

	targetCWD := normalizeCWD(cfg.AgentCwd)
	summaries := make([]threadSummary, 0, len(threadIDs))
	otherDirCounts := map[string]int{}
	otherUnknown := 0

	for _, id := range threadIDs {
		threadCfg, cfgErr := store.LoadConfig(id)
		threadCWD := normalizeCWD(threadCfg.CWD)
		if cfgErr == nil && threadCWD != "" && threadCWD != targetCWD {
			otherDirCounts[threadCWD]++
			continue
		}
		if cfgErr == nil && threadCWD == "" {
			otherUnknown++
		}

		s := threadSummary{id: id}

		// Modification time: use the thread directory's mtime as a proxy.
		if fi, err := os.Stat(filepath.Join(cfg.ThreadsDir, id)); err == nil {
			s.modTime = fi.ModTime()
		}

		// Preview: walk from leaf looking for the most recent user message.
		if leafID, err := store.FindLeaf(id); err == nil && leafID != "" {
			s.preview = lastUserPreview(store, id, leafID)
		}

		// Pending question check.
		if turnState, _ := store.LoadTurnState(id); turnState != nil && turnState.Phase == thread.PhaseWaitingForAnswer {
			s.pending = true
		}

		summaries = append(summaries, s)
	}

	// Sort newest-first.
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].modTime.After(summaries[j].modTime)
	})

	if len(summaries) == 0 {
		fmt.Fprintf(os.Stderr, "No threads found for current directory: %s\n", targetCWD)
	} else {
		fmt.Fprintf(os.Stderr, "\nAvailable threads for %s:\n", targetCWD)
		for i, s := range summaries {
			marker := ""
			if s.id == currentThreadID {
				marker = " (current)"
			}
			if s.pending {
				marker += " [pending approval]"
			}
			age := formatAge(time.Since(s.modTime))
			fmt.Fprintf(os.Stderr, "  %d. %s  %s%s\n", i+1, s.id, age, marker)
			if s.preview != "" {
				fmt.Fprintf(os.Stderr, "     \"%s\"\n", s.preview)
			}
		}
	}

	if len(otherDirCounts) > 0 {
		fmt.Fprintln(os.Stderr, "")
		totalOther := 0
		for _, n := range otherDirCounts {
			totalOther += n
		}
		fmt.Fprintf(os.Stderr, "%d thread(s) belong to other directories.\n", totalOther)
		dirs := make([]string, 0, len(otherDirCounts))
		for dir := range otherDirCounts {
			dirs = append(dirs, dir)
		}
		sort.Strings(dirs)
		for _, dir := range dirs {
			fmt.Fprintf(os.Stderr, "  - %s (%d)\n", dir, otherDirCounts[dir])
		}
		fmt.Fprintln(os.Stderr, "To resume those threads, cd into that directory and run /resume.")
	}
	if otherUnknown > 0 {
		fmt.Fprintf(os.Stderr, "\nIncluding %d legacy thread(s) with unknown cwd.\n", otherUnknown)
	}

	if len(summaries) == 0 {
		return currentThreadID
	}

	for {
		input, err := readLine("\nSelect thread (number, or Enter to cancel): ", nil)
		if err != nil {
			return currentThreadID
		}
		input = strings.TrimSpace(input)
		if input == "" {
			fmt.Fprintln(os.Stderr, "Cancelled.")
			return currentThreadID
		}
		n, err := strconv.Atoi(input)
		if err != nil || n < 1 || n > len(summaries) {
			fmt.Fprintf(os.Stderr, "Please enter a number between 1 and %d.\n", len(summaries))
			continue
		}
		return summaries[n-1].id
	}
}

// lastUserPreview walks the message chain from leafID upward (up to 20 hops)
// looking for the most recent human-typed user message, and returns its text
// preview. Auto-injected setup messages (system prompts, <system-reminder>
// blocks, skills reminders) are skipped.
func lastUserPreview(store *thread.Store, threadID, leafID string) string {
	currentID := leafID
	for i := 0; i < 20 && currentID != ""; i++ {
		msg, err := store.LoadMessage(threadID, currentID)
		if err != nil {
			break
		}
		if msg.Message.Role == "user" && !isInjectedMessageID(msg.ID) {
			if text := extractMessageText(msg.Message.Parts); text != "" && !isInjectedText(text) {
				return abbreviate(text, 80)
			}
		}
		currentID = msg.ParentID
	}
	return ""
}

// isInjectedMessageID reports whether a stored message ID belongs to an
// auto-injected setup message (system prompt, instructions, skills reminder).
func isInjectedMessageID(id string) bool {
	return strings.HasPrefix(id, "system-") ||
		strings.HasPrefix(id, "instructions-") ||
		strings.HasPrefix(id, "skills-")
}

// isInjectedText reports whether message text is auto-injected content
// (system reminders wrapped in XML tags) rather than human-typed input.
func isInjectedText(text string) bool {
	return strings.HasPrefix(text, "<system-reminder>") ||
		strings.HasPrefix(text, "<skills-reminder>")
}

// extractMessageText returns the first non-empty text from a list of message parts.
func extractMessageText(parts []message.Part) string {
	for _, p := range parts {
		if tp, ok := p.(message.TextPart); ok && tp.Text != "" {
			return tp.Text
		}
	}
	return ""
}

func extractAllText(parts []message.Part) string {
	var b strings.Builder
	for _, p := range parts {
		if tp, ok := p.(message.TextPart); ok {
			b.WriteString(tp.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

func printThreadHistory(store *thread.Store, threadID string) bool {
	leafID, err := store.FindLeaf(threadID)
	if err != nil || leafID == "" {
		return false
	}

	history, err := store.BuildHistoryWithIDs(threadID, leafID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading thread history: %v\n", err)
		return false
	}

	printed := false
	for _, entry := range history {
		if isInjectedMessageID(entry.ID) {
			continue
		}
		if entry.Message.Role != "user" && entry.Message.Role != "assistant" {
			continue
		}

		text := extractAllText(entry.Message.Parts)
		if text == "" {
			continue
		}

		fmt.Fprintln(os.Stdout)
		if entry.Message.Role == "user" {
			if !noColor && term.IsTerminal(int(os.Stdout.Fd())) {
				fmt.Fprint(os.Stdout, "\033[1;36m>\033[0m ")
			} else {
				fmt.Fprint(os.Stdout, "> ")
			}
		}

		md := newMarkdownRenderer(os.Stdout, term.IsTerminal(int(os.Stdout.Fd())), !noColor)
		md.WriteText(text)
		md.Finish()
		fmt.Fprintln(os.Stdout)
		printed = true
	}

	if printed {
		fmt.Fprintln(os.Stdout)
	}
	return printed
}

// formatAge returns a human-readable "X ago" string for a duration.
func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}
