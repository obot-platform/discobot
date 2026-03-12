//go:build !windows

package cli

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestEscWatchTermios_PreservesOutputProcessing(t *testing.T) {
	old := &unix.Termios{}
	old.Oflag = unix.OPOST | unix.ONLCR
	old.Lflag = unix.ICANON | unix.ECHO | unix.ISIG

	state := escWatchTermios(old)

	if state.Oflag != old.Oflag {
		t.Fatalf("output flags changed: got %#x want %#x", state.Oflag, old.Oflag)
	}
	if state.Lflag&unix.ICANON != 0 {
		t.Fatal("expected ICANON to be cleared")
	}
	if state.Lflag&unix.ECHO != 0 {
		t.Fatal("expected ECHO to be cleared")
	}
	if state.Lflag&unix.ISIG == 0 {
		t.Fatal("expected ISIG to remain enabled for Ctrl+C")
	}
	if state.Cc[unix.VMIN] != 1 {
		t.Fatalf("VMIN = %d, want 1", state.Cc[unix.VMIN])
	}
	if state.Cc[unix.VTIME] != 0 {
		t.Fatalf("VTIME = %d, want 0", state.Cc[unix.VTIME])
	}
}
