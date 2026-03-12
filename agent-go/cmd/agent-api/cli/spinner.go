package cli

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// spinner animates a small rotating indicator on stderr while the agent is
// thinking. It is safe to call Stop multiple times.
type spinner struct {
	once   sync.Once
	stopCh chan struct{}
	doneCh chan struct{}
}

func newSpinner() *spinner {
	return &spinner{
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

// Start launches the spinner goroutine. Call Stop to clear it.
// When NO_COLOR is set the spinner is suppressed and the goroutine exits
// immediately so Stop() is always safe to call.
func (s *spinner) Start() {
	go func() {
		defer close(s.doneCh)
		if noColor {
			<-s.stopCh
			return
		}
		frames := []string{"|", "/", "-", "\\"}
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		i := 0
		for {
			select {
			case <-s.stopCh:
				fmt.Fprint(os.Stderr, "\r \r") // erase the spinner character
				return
			case <-ticker.C:
				fmt.Fprintf(os.Stderr, "\r%s", frames[i%len(frames)])
				i++
			}
		}
	}()
}

// Stop clears the spinner and blocks until the goroutine has exited.
// Safe to call multiple times or before Start.
func (s *spinner) Stop() {
	s.once.Do(func() { close(s.stopCh) })
	<-s.doneCh
}
