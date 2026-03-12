package cli

import (
	"errors"
	"io"
	"regexp"
)

// errInterrupt is returned by readLine when the user presses Ctrl+C.
var errInterrupt = errors.New("interrupted")

const maxHistoryEntries = 500

type pastedChunk struct {
	end     int
	rawLen  int
	dispLen int
}

type stdioReadWriter struct {
	r io.Reader
	w io.Writer
}

type readLineOptions struct {
	slashCommands []string
}

type tabCompletionState struct {
	lastPrefix         string
	lastWasMultiple    bool
	completionRendered bool
}

type textRange struct {
	start int
	end   int
}

const sanitizeTriggerKey = byte(0x18)

var (
	bracketPasteStart = []byte{0x1b, '[', '2', '0', '0', '~'}
	bracketPasteEnd   = []byte{0x1b, '[', '2', '0', '1', '~'}

	wellFormedPasteBlockPattern = regexp.MustCompile(`^\[pasted \d+ lines/\d+ chars\]$`)
	unbracketedPastePattern     = regexp.MustCompile(`pasted \d+ line\w*/\d+ ch\w*\]?`)
)
