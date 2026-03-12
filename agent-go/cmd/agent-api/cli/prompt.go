package cli

import (
	"os"
	"strings"

	"golang.org/x/term"
)

// noColor is true when the NO_COLOR environment variable is set (any value),
// per the convention at https://no-color.org/. When true, ANSI cursor-control
// and spinner output are suppressed.
var noColor = func() bool {
	_, set := os.LookupEnv("NO_COLOR")
	return set
}()

func promptColorsEnabled() bool {
	return !noColor && term.IsTerminal(int(os.Stderr.Fd()))
}

func formatPrompt(model string, planMode bool) string {
	planTag := ""
	if planMode {
		planTag = "[plan]"
	}

	if model == "" {
		if planTag != "" {
			if promptColorsEnabled() {
				return "\n\033[36m" + planTag + "\033[0m \033[1;36m>\033[0m "
			}
			return "\n" + planTag + " > "
		}
		if promptColorsEnabled() {
			return "\n\033[1;36m>\033[0m "
		}
		return "\n> "
	}

	if promptColorsEnabled() {
		return "\n\033[36m[" + model + "]" + planTag + "\033[0m \033[1;36m>\033[0m "
	}
	return "\n[" + model + "]" + planTag + " > "
}

func formatPromptHint() string {
	if promptColorsEnabled() {
		return "\033[1;36m>\033[0m "
	}
	return "> "
}

func startupMessage(showResume, showHistory bool) string {
	msg := "Discobot agent ready. Type your message"
	var commands []string
	if showResume {
		commands = append(commands, "/resume to switch threads")
	}
	if showHistory {
		commands = append(commands, "/history to view thread messages")
	}
	if len(commands) > 0 {
		msg += ", " + strings.Join(commands, ", ")
	}
	msg += ", or 'exit' to quit."
	return msg
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func resumeThreadCommand(threadID, argv0 string) string {
	if threadID == "" || argv0 == "" {
		return ""
	}
	return shellQuote(argv0) + " --resume " + shellQuote(threadID)
}
