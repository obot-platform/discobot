package startup

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestWaitForTCPBindSucceedsImmediately(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close initial listener: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := waitForTCPBind(ctx, addr, 200*time.Millisecond, 10*time.Millisecond); err != nil {
		t.Fatalf("waitForTCPBind: %v", err)
	}

	rebound, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("rebind after waitForTCPBind: %v", err)
	}
	_ = rebound.Close()
}

func TestWaitForTCPBindRetriesUntilPortIsReleased(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	defer func() { _ = listener.Close() }()

	time.AfterFunc(35*time.Millisecond, func() {
		_ = listener.Close()
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	start := time.Now()
	if err := waitForTCPBind(ctx, addr, 300*time.Millisecond, 10*time.Millisecond); err != nil {
		t.Fatalf("waitForTCPBind: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Fatalf("expected at least one retry, got %s", elapsed)
	}
}

func TestWaitForTCPBindTimesOutWhenPortStaysBusy(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := listener.Addr().String()
	defer func() { _ = listener.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err = waitForTCPBind(ctx, addr, 50*time.Millisecond, 10*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}
