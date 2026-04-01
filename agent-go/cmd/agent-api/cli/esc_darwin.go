//go:build darwin

package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// watchEscDuringTurn monitors stdin for ESC while an agent turn is streaming.
// This is the Darwin implementation; it uses TIOCGETA/TIOCSETA instead of
// the Linux-specific TCGETS/TCSETS ioctl constants.
func watchEscDuringTurn(ctx context.Context, cancel context.CancelFunc) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return
	}

	oldState, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return
	}
	state := escWatchTermios(oldState)
	if err := unix.IoctlSetTermios(fd, unix.TIOCSETA, &state); err != nil {
		return
	}
	defer unix.IoctlSetTermios(fd, unix.TIOCSETA, oldState) //nolint:errcheck

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
