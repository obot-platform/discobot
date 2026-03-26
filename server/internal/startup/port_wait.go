package startup

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"
)

const (
	defaultPortBindRetryInterval = 10 * time.Second
	defaultPortBindTimeout       = 2 * time.Minute
)

// WaitForTCPBind blocks until addr can be bound or the timeout expires.
// It immediately releases the listener on success so regular startup can continue.
func WaitForTCPBind(ctx context.Context, addr string) error {
	return waitForTCPBind(ctx, addr, defaultPortBindTimeout, defaultPortBindRetryInterval)
}

func waitForTCPBind(ctx context.Context, addr string, timeout, retryInterval time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
		if err == nil {
			if closeErr := listener.Close(); closeErr != nil {
				return fmt.Errorf("close temporary listener for %s: %w", addr, closeErr)
			}
			return nil
		}

		if ctx.Err() != nil {
			return fmt.Errorf("api port %s did not become available within %s (last error: %v): %w", addr, timeout, err, ctx.Err())
		}

		log.Printf("API port %s is still unavailable; retrying in %s: %v", addr, retryInterval, err)

		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return fmt.Errorf("api port %s did not become available within %s: %w", addr, timeout, ctx.Err())
		case <-timer.C:
		}
	}
}
