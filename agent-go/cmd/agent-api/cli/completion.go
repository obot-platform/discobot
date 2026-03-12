package cli

import (
	"fmt"
	"io"
	"strings"
)

func completeSlashCommand(line string, pos int, commands []string) (string, int, []string, bool) {
	if pos < 0 {
		pos = 0
	}
	if pos > len(line) {
		pos = len(line)
	}

	cmdStart := 0
	for cmdStart < len(line) && line[cmdStart] == ' ' {
		cmdStart++
	}
	if cmdStart >= len(line) || line[cmdStart] != '/' {
		return "", 0, nil, false
	}

	cmdEnd := cmdStart
	for cmdEnd < len(line) && line[cmdEnd] != ' ' && line[cmdEnd] != '\t' {
		cmdEnd++
	}
	if pos < cmdStart || pos > cmdEnd {
		return "", 0, nil, false
	}

	prefix := line[cmdStart:pos]
	if !strings.HasPrefix(prefix, "/") {
		return "", 0, nil, false
	}

	matches := make([]string, 0, len(commands))
	for _, cmd := range commands {
		if strings.HasPrefix(cmd, prefix) {
			matches = append(matches, cmd)
		}
	}
	if len(matches) == 0 {
		return "", 0, nil, false
	}

	replacement := prefix
	if len(matches) == 1 {
		replacement = matches[0]
		if cmdEnd == len(line) {
			replacement += " "
		}
	} else {
		common := longestCommonPrefix(matches)
		if len(common) > len(prefix) {
			replacement = common
		}
	}

	completed := line[:cmdStart] + replacement + line[cmdEnd:]
	newPos := cmdStart + len(replacement)
	return completed, newPos, matches, true
}

func longestCommonPrefix(values []string) string {
	if len(values) == 0 {
		return ""
	}
	prefix := values[0]
	for _, v := range values[1:] {
		for !strings.HasPrefix(v, prefix) {
			if prefix == "" {
				return ""
			}
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

func slashPrefixAtCursor(line string, pos int) (string, bool) {
	if pos < 0 {
		pos = 0
	}
	if pos > len(line) {
		pos = len(line)
	}

	cmdStart := 0
	for cmdStart < len(line) && line[cmdStart] == ' ' {
		cmdStart++
	}
	if cmdStart >= len(line) || line[cmdStart] != '/' {
		return "", false
	}

	cmdEnd := cmdStart
	for cmdEnd < len(line) && line[cmdEnd] != ' ' && line[cmdEnd] != '\t' {
		cmdEnd++
	}
	if pos < cmdStart || pos > cmdEnd {
		return "", false
	}

	prefix := line[cmdStart:pos]
	if !strings.HasPrefix(prefix, "/") {
		return "", false
	}
	return prefix, true
}

func printCompletionMatches(stdout io.Writer, matches []string, maxWidth int, prompt string, buffer string) {
	line := strings.Join(matches, "  ")
	line = truncateCompletionLine(line, maxWidth)
	displayPrompt := strings.TrimLeft(prompt, "\n")
	fmt.Fprintf(stdout, "\r\n%s\r\n%s%s", line, displayPrompt, buffer)
}

func truncateCompletionLine(line string, maxWidth int) string {
	if maxWidth <= 0 {
		return line
	}
	runes := []rune(line)
	if len(runes) <= maxWidth {
		return line
	}
	suffix := []rune(" ...")
	if maxWidth <= len(suffix) {
		return string(suffix[:maxWidth])
	}
	cut := maxWidth - len(suffix)
	return strings.TrimRight(string(runes[:cut]), " ") + string(suffix)
}
