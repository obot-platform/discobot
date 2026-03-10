package transport

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestTransportRetryObserver_HTTP429(t *testing.T) {
	calls := 0
	tr := &Transport{
		Base: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Body:       io.NopCloser(strings.NewReader(`{"error":"rate limited"}`)),
					Header:     make(http.Header),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("ok")),
				Header:     make(http.Header),
			}, nil
		}),
		MaxRetries: 1,
		BaseDelay:  time.Millisecond,
	}

	var events []RetryEvent
	ctx := WithRetryObserver(context.Background(), func(event RetryEvent) {
		events = append(events, event)
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 retry event, got %d", len(events))
	}
	if events[0].Attempt != 1 || events[0].MaxRetries != 1 {
		t.Fatalf("unexpected attempt metadata: %+v", events[0])
	}
	if events[0].StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected status 429 in event, got %d", events[0].StatusCode)
	}
	if events[0].Err != nil {
		t.Fatalf("expected nil transport error for HTTP retry, got %v", events[0].Err)
	}
}

func TestTransportRetryObserver_NetworkError(t *testing.T) {
	calls := 0
	tr := &Transport{
		Base: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return nil, errors.New("dial failed")
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("ok")),
				Header:     make(http.Header),
			}, nil
		}),
		MaxRetries: 1,
		BaseDelay:  time.Millisecond,
	}

	var events []RetryEvent
	ctx := WithRetryObserver(context.Background(), func(event RetryEvent) {
		events = append(events, event)
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 retry event, got %d", len(events))
	}
	if events[0].StatusCode != 0 {
		t.Fatalf("expected status 0 for network error, got %d", events[0].StatusCode)
	}
	if events[0].Err == nil {
		t.Fatal("expected transport error in retry event")
	}
}

func TestTransportDoesNotRetryContextCanceled(t *testing.T) {
	calls := 0
	tr := &Transport{
		Base: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			calls++
			return nil, context.Canceled
		}),
		MaxRetries: 1,
		BaseDelay:  time.Millisecond,
	}

	var events []RetryEvent
	ctx := WithRetryObserver(context.Background(), func(event RetryEvent) {
		events = append(events, event)
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := tr.RoundTrip(req)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error, got %v", err)
	}
	if resp != nil {
		t.Fatalf("expected nil response, got %#v", resp)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 retry events, got %d", len(events))
	}
}

func TestTransportRetriesContextDeadlineExceeded(t *testing.T) {
	calls := 0
	tr := &Transport{
		Base: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return nil, context.DeadlineExceeded
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("ok")),
				Header:     make(http.Header),
			}, nil
		}),
		MaxRetries: 1,
		BaseDelay:  time.Millisecond,
	}

	var events []RetryEvent
	ctx := WithRetryObserver(context.Background(), func(event RetryEvent) {
		events = append(events, event)
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 retry event, got %d", len(events))
	}
	if !errors.Is(events[0].Err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded retry event, got %v", events[0].Err)
	}
}

func TestNewClientUsesResponseHeaderTimeoutInsteadOfClientTimeout(t *testing.T) {
	client := NewClient(5 * time.Second)
	if client.Timeout != 0 {
		t.Fatalf("expected client timeout to be unset, got %v", client.Timeout)
	}

	tr, ok := client.Transport.(*Transport)
	if !ok {
		t.Fatalf("expected *Transport, got %T", client.Transport)
	}

	base, ok := tr.Base.(*http.Transport)
	if !ok {
		t.Fatalf("expected base *http.Transport, got %T", tr.Base)
	}

	if base.ResponseHeaderTimeout != 5*time.Second {
		t.Fatalf("expected response header timeout 5s, got %v", base.ResponseHeaderTimeout)
	}
}
