//go:build !windows

package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func escWatchTermios(oldState *unix.Termios) unix.Termios {
	state := *oldState
	state.Lflag &^= unix.ICANON | unix.ECHO
	state.Cc[unix.VMIN] = 1
	state.Cc[unix.VTIME] = 0
	return state
}

// watchEscDuringTurn monitors stdin for ESC while an agent turn is streaming.
// It switches stdin into a non-canonical, no-echo mode so ESC is delivered as
// soon as it is pressed, while preserving output processing so rendered newlines
// still return the cursor to column 0.
//
// Ctrl+C continues to flow through the normal SIGINT path, and ESC cancels the
// turn directly here so both keys end up interrupting the active turn.
//
// Any byte other than ESC that arrives while the watcher is running is
// discarded. This means keystrokes typed during a turn will not appear at the
// next prompt.
//
// The function returns as soon as ctx is cancelled (either by an ESC press or
// because the turn ended normally).
func watchEscDuringTurn(ctx context.Context, cancel context.CancelFunc) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return
	}

	oldState, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return
	}
	state := escWatchTermios(oldState)
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &state); err != nil {
		return
	}
	defer unix.IoctlSetTermios(fd, unix.TCSETS, oldState) //nolint:errcheck

	if err := unix.SetNonblock(fd, true); err != nil {
		return
	}
	defer unix.SetNonblock(fd, false) //nolint:errcheck

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	buf := make([]byte, 1)
	for {
		n, err := unix.Read(fd, buf)
		if err == unix.EAGAIN {
			// No data available; wait for the next poll tick or context done.
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			continue
		}
		if err != nil || n == 0 {
			return
		}

		if buf[0] == 0x1b { // ESC
			fmt.Fprint(os.Stderr, "\n^C\n")
			cancel()
			return
		}
	}
}
