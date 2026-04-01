//go:build !windows

package cli

import "golang.org/x/sys/unix"

func escWatchTermios(oldState *unix.Termios) unix.Termios {
	state := *oldState
	state.Lflag &^= unix.ICANON | unix.ECHO
	state.Cc[unix.VMIN] = 1
	state.Cc[unix.VTIME] = 0
	return state
}
