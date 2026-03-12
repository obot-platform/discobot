package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

func (s stdioReadWriter) Read(p []byte) (int, error) {
	return s.r.Read(p)
}

func (s stdioReadWriter) Write(p []byte) (int, error) {
	return s.w.Write(p)
}

// readLine reads one interactive line using the readline-style editor. History
// navigation is enabled when hist is non-nil.
//
// Returns:
//   - (line, nil)         — normal input
//   - ("", errInterrupt)  — Ctrl+C
//   - ("", io.EOF)        — Ctrl+D on empty buffer, or stdin closed
func readLine(prompt string, hist *cmdHistory) (string, error) {
	return readLineWithOptions(prompt, hist, nil)
}

func readLineWithOptions(prompt string, hist *cmdHistory, opts *readLineOptions) (string, error) {
	return readLineReadlineWithOptions(prompt, hist, opts)
}

func readLineReadlineWithOptions(prompt string, hist *cmdHistory, opts *readLineOptions) (string, error) {
	// Non-interactive fallback (piped stdin).
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, prompt)
		return readLineSimple()
	}

	width, height := 0, 0
	if w, h, err := term.GetSize(int(os.Stderr.Fd())); err == nil {
		width, height = w, h
	} else if w, h, err := term.GetSize(int(os.Stdin.Fd())); err == nil {
		width, height = w, h
	}

	return readLineReadlineTTYWithOptions(
		prompt,
		hist,
		os.Stdin,
		os.Stderr,
		width,
		height,
		opts,
		func() (*term.State, error) { return term.MakeRaw(int(os.Stdin.Fd())) },
		func(state *term.State) error { return term.Restore(int(os.Stdin.Fd()), state) },
	)
}

func readLineReadlineTTY(
	prompt string,
	hist *cmdHistory,
	stdin io.Reader,
	stdout io.Writer,
	width int,
	height int,
	makeRaw func() (*term.State, error),
	restore func(*term.State) error,
) (string, error) {
	return readLineReadlineTTYWithOptions(prompt, hist, stdin, stdout, width, height, nil, makeRaw, restore)
}

func readLineReadlineTTYWithOptions(
	prompt string,
	hist *cmdHistory,
	stdin io.Reader,
	stdout io.Writer,
	width int,
	height int,
	opts *readLineOptions,
	makeRaw func() (*term.State, error),
	restore func(*term.State) error,
) (string, error) {
	oldState, err := makeRaw()
	if err != nil {
		return "", err
	}
	defer restore(oldState) //nolint:errcheck

	var sawCtrlC bool
	reader := &inputTrackingReader{r: stdin, sawC: &sawCtrlC}
	rw := stdioReadWriter{r: reader, w: stdout}
	t := term.NewTerminal(rw, prompt)
	if width > 0 {
		if height < 1 {
			height = 1
		}
		_ = t.SetSize(width, height)
	}
	t.SetBracketedPasteMode(true)
	defer t.SetBracketedPasteMode(false)
	if hist != nil {
		t.History = historyView{h: hist}
	} else {
		t.History = historyView{h: &cmdHistory{}}
	}
	tabCompletion := &tabCompletionState{}
	t.AutoCompleteCallback = func(line string, pos int, key rune) (string, int, bool) {
		if key == rune(sanitizeTriggerKey) {
			cleaned, newPos, changed := sanitizeMalformedPasteBlocksWithCursor(line, pos)
			if !changed {
				return "", 0, false
			}
			return cleaned, newPos, true
		}

		if key != '\t' {
			tabCompletion.lastPrefix = ""
			tabCompletion.lastWasMultiple = false
			tabCompletion.completionRendered = false
			return "", 0, false
		}
		if opts == nil || len(opts.slashCommands) == 0 {
			tabCompletion.lastPrefix = ""
			tabCompletion.lastWasMultiple = false
			tabCompletion.completionRendered = false
			return "", 0, false
		}

		prefix, ok := slashPrefixAtCursor(line, pos)
		if !ok {
			tabCompletion.lastPrefix = ""
			tabCompletion.lastWasMultiple = false
			return "", 0, false
		}

		completed, newPos, matches, ok := completeSlashCommand(line, pos, opts.slashCommands)
		if !ok {
			tabCompletion.lastPrefix = ""
			tabCompletion.lastWasMultiple = false
			return "", 0, false
		}

		if len(matches) <= 1 {
			tabCompletion.lastPrefix = ""
			tabCompletion.lastWasMultiple = false
			tabCompletion.completionRendered = false
			return completed, newPos, true
		}

		nextPrefix := prefix
		if updatedPrefix, ok := slashPrefixAtCursor(completed, newPos); ok {
			nextPrefix = updatedPrefix
		}
		showMatches := tabCompletion.lastWasMultiple && tabCompletion.lastPrefix == prefix
		tabCompletion.lastPrefix = nextPrefix
		tabCompletion.lastWasMultiple = true

		if showMatches && !tabCompletion.completionRendered {
			displayWidth := width
			if displayWidth <= 0 {
				displayWidth = 80
			}
			printCompletionMatches(stdout, matches, displayWidth, prompt, completed)
			tabCompletion.completionRendered = true
		}
		return completed, newPos, true
	}

	line, err := t.ReadLine()
	line = reader.expandPastes(line)
	if errors.Is(err, term.ErrPasteIndicator) {
		return line, nil
	}
	if err == nil {
		return line, nil
	}
	if errors.Is(err, io.EOF) {
		if sawCtrlC {
			fmt.Fprint(stdout, "^C\r\n")
			return "", errInterrupt
		}
		return "", io.EOF
	}
	return line, err
}

// readLineSimple reads a line from os.Stdin byte-by-byte without raw mode.
// Used as fallback when stdin is not a terminal or MakeRaw fails.
func readLineSimple() (string, error) {
	var buf []byte
	b := make([]byte, 1)
	for {
		_, err := os.Stdin.Read(b)
		if err != nil {
			if len(buf) > 0 {
				return strings.TrimRight(string(buf), "\r\n"), nil
			}
			return "", err
		}
		if b[0] == '\n' {
			return strings.TrimRight(string(buf), "\r"), nil
		}
		buf = append(buf, b[0])
	}
}
