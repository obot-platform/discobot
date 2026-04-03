// Package transport provides a shared http.RoundTripper for all providers
// that adds two cross-cutting behaviours:
//
//  1. Retry — transparently retries requests on 429 and 5xx responses using
//     exponential back-off with jitter. The Retry-After response header is
//     respected for 429s.
//
//  2. Logging — non-blocking, fire-and-forget writes of the raw HTTP request
//     body (JSON) and the raw HTTP response body (JSONL for SSE streams) to
//     caller-supplied file paths. Paths are injected via context using
//     WithLogFiles so that turn.go can write directly into the per-step turn
//     directory:
//
//     threads/{threadID}/turns/{turnID}/step-NNN-req.json
//     threads/{threadID}/turns/{turnID}/step-NNN-resp.jsonl
//
// If logging paths are not present in the context (e.g. CountTokens calls
// outside a turn), logging is silently skipped.  All logging failures are
// silently ignored — the transport never fails a request due to a log error.
package transport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// --- Context keys ---

type contextKey struct{ name string }

var (
	reqPathKey       = &contextKey{"transport-req-path"}
	respPathKey      = &contextKey{"transport-resp-path"}
	retryObserverKey = &contextKey{"transport-retry-observer"}
)

// WithLogFiles returns a derived context that tells the transport to write the
// HTTP request body to reqPath and the raw response body to respPath.
// Both writes are non-blocking fire-and-forget; any failure is silently ignored.
func WithLogFiles(ctx context.Context, reqPath, respPath string) context.Context {
	ctx = context.WithValue(ctx, reqPathKey, reqPath)
	ctx = context.WithValue(ctx, respPathKey, respPath)
	return ctx
}

// RetryEvent describes an upcoming transport retry attempt.
type RetryEvent struct {
	// Attempt is 1-based (1 = first retry after initial failure).
	Attempt int
	// MaxRetries is the configured retry count after the initial attempt.
	MaxRetries int
	// Delay is how long the transport will wait before retrying.
	Delay time.Duration
	// StatusCode is the failed HTTP status code for HTTP-level retries.
	// It is zero for network-level errors.
	StatusCode int
	// Err is the network-level error for transport failures.
	// It is nil for HTTP-status retries.
	Err error
}

// RetryObserver receives retry events before the transport sleeps and retries.
type RetryObserver func(RetryEvent)

// WithRetryObserver returns a derived context that lets callers observe
// transport retry/backoff decisions.
func WithRetryObserver(ctx context.Context, observer RetryObserver) context.Context {
	return context.WithValue(ctx, retryObserverKey, observer)
}

// ObserveRetry emits a retry event to any observer attached to ctx.
func ObserveRetry(ctx context.Context, event RetryEvent) {
	observer, _ := ctx.Value(retryObserverKey).(RetryObserver)
	notifyRetry(observer, event)
}

// --- Client constructor ---

// NewClient returns an *http.Client that uses the logging+retry Transport.
//
// timeout is applied by the base http.Transport while waiting for response
// headers, rather than via http.Client.Timeout. This keeps long-lived response
// bodies (such as SSE streams) from being cancelled mid-read while still
// allowing pre-response timeouts to be retried by the Transport.
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: &Transport{Base: newBaseTransport(timeout)},
	}
}

func newBaseTransport(timeout time.Duration) http.RoundTripper {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return http.DefaultTransport
	}
	tr := base.Clone()
	if timeout > 0 {
		tr.ResponseHeaderTimeout = timeout
	}
	return tr
}

// --- Transport ---

const (
	defaultMaxRetries = 3
	defaultBaseDelay  = time.Second
	maxBackoffDelay   = 30 * time.Second
	maxRetryAfter     = 60 * time.Second
)

var retryableStatus = map[int]bool{
	http.StatusTooManyRequests:     true, // 429
	http.StatusInternalServerError: true, // 500
	http.StatusBadGateway:          true, // 502
	http.StatusServiceUnavailable:  true, // 503
	http.StatusGatewayTimeout:      true, // 504
}

// Transport implements http.RoundTripper with retry and async request/response
// logging. Zero value is ready to use (uses http.DefaultTransport as the base).
type Transport struct {
	// Base is the underlying RoundTripper. Nil means http.DefaultTransport.
	Base http.RoundTripper
	// MaxRetries is the number of retry attempts after the initial request.
	// Zero means defaultMaxRetries (3).
	MaxRetries int
	// BaseDelay is the initial back-off duration before the first retry.
	// Zero means defaultBaseDelay (1 s).
	BaseDelay time.Duration
}

func (t *Transport) base() http.RoundTripper {
	if t.Base != nil {
		return t.Base
	}
	return http.DefaultTransport
}

func (t *Transport) maxRetries() int {
	if t.MaxRetries > 0 {
		return t.MaxRetries
	}
	return defaultMaxRetries
}

func (t *Transport) baseDelay() time.Duration {
	if t.BaseDelay > 0 {
		return t.BaseDelay
	}
	return defaultBaseDelay
}

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Buffer the body so it can be replayed on retry and logged.
	var reqBody []byte
	if req.Body != nil {
		var err error
		reqBody, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
	}

	ctx := req.Context()
	reqPath, _ := ctx.Value(reqPathKey).(string)
	respPath, _ := ctx.Value(respPathKey).(string)
	retryObserver, _ := ctx.Value(retryObserverKey).(RetryObserver)

	// Write the request body once (fire-and-forget).
	if reqPath != "" && len(reqBody) > 0 {
		fireWriteFile(reqPath, reqBody)
	}

	maxRetries := t.maxRetries()
	baseDelay := t.baseDelay()

	var (
		resp *http.Response
		err  error
	)

	for attempt := 0; ; attempt++ {
		// Restore body for each attempt.
		if len(reqBody) > 0 {
			req.Body = io.NopCloser(bytes.NewReader(reqBody))
			req.ContentLength = int64(len(reqBody))
		}

		resp, err = t.base().RoundTrip(req)

		// Stop after max retries regardless of outcome.
		if attempt >= maxRetries {
			break
		}

		if err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() == context.Canceled {
				break
			}
			// Network-level error — wait then retry.
			delay := backoff(baseDelay, attempt, "")
			notifyRetry(retryObserver, RetryEvent{
				Attempt:    attempt + 1,
				MaxRetries: maxRetries,
				Delay:      delay,
				Err:        err,
			})
			if !sleepCtx(ctx, delay) {
				return nil, ctx.Err()
			}
			continue
		}

		if !retryableStatus[resp.StatusCode] {
			break // success or non-retryable error (4xx etc.)
		}

		// Retryable HTTP status: drain + close the failed response body,
		// then sleep before the next attempt.
		retryAfter := resp.Header.Get("Retry-After")
		delay := backoff(baseDelay, attempt, retryAfter)
		resp.Body.Close()
		notifyRetry(retryObserver, RetryEvent{
			Attempt:    attempt + 1,
			MaxRetries: maxRetries,
			Delay:      delay,
			StatusCode: resp.StatusCode,
		})

		if !sleepCtx(ctx, delay) {
			return nil, ctx.Err()
		}
	}

	// Wrap the final response body with the async logger.
	if err == nil && respPath != "" {
		resp.Body = newLoggingBody(resp.Body, respPath)
	}

	return resp, err
}

// --- Back-off helpers ---

// backoff returns an exponential delay with ±10 % random jitter.
// If retryAfter is a valid integer (seconds from Retry-After header), that
// value is used instead (capped at maxRetryAfter).
func backoff(base time.Duration, attempt int, retryAfter string) time.Duration {
	if retryAfter != "" {
		if secs, err := strconv.Atoi(retryAfter); err == nil && secs > 0 {
			d := time.Duration(secs) * time.Second
			if d > maxRetryAfter {
				d = maxRetryAfter
			}
			return d
		}
	}

	exp := math.Pow(2, float64(attempt))
	d := time.Duration(float64(base) * exp)
	// ±10 % jitter
	d += time.Duration((rand.Float64()*2 - 1) * float64(d) * 0.1)
	if d > maxBackoffDelay {
		d = maxBackoffDelay
	}
	return d
}

// sleepCtx waits for d to elapse or for ctx to be cancelled.
// Returns false if the context was cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

func notifyRetry(observer RetryObserver, event RetryEvent) {
	if observer == nil {
		return
	}
	defer func() {
		_ = recover()
	}()
	observer(event)
}

// --- Async fire-and-forget logging ---

// fireWriteFile writes data to path in a background goroutine, ignoring errors.
func fireWriteFile(path string, data []byte) {
	go func() { _ = os.WriteFile(path, data, 0o644) }()
}

// loggingBody wraps an http.Response body and tees every byte read through an
// asyncFileWriter. The write is non-blocking: data is silently dropped if the
// writer's buffer is full.  The file is closed when the body is closed.
type loggingBody struct {
	rc  io.ReadCloser
	fw  *asyncFileWriter
	tee io.Reader
}

func newLoggingBody(rc io.ReadCloser, path string) *loggingBody {
	fw := newAsyncFileWriter(path)
	return &loggingBody{
		rc:  rc,
		fw:  fw,
		tee: io.TeeReader(rc, fw),
	}
}

func (b *loggingBody) Read(p []byte) (int, error) { return b.tee.Read(p) }

func (b *loggingBody) Close() error {
	b.fw.close()
	return b.rc.Close()
}

// asyncFileWriter is an io.Writer that fans incoming data to a file via a
// buffered channel. If the channel is full, writes are silently dropped,
// preserving fire-and-forget semantics. The file is opened lazily in a
// background goroutine; errors opening the file are silently ignored.
type asyncFileWriter struct {
	ch   chan []byte
	once sync.Once
}

const asyncWriterChanSize = 512

func newAsyncFileWriter(path string) *asyncFileWriter {
	w := &asyncFileWriter{ch: make(chan []byte, asyncWriterChanSize)}
	go func() {
		f, err := os.Create(path)
		if err != nil {
			for range w.ch {
				continue // drain so goroutine exits and channel can be GC'd
			}
			return
		}
		defer f.Close()
		for data := range w.ch {
			_, _ = f.Write(data)
		}
	}()
	return w
}

// Write implements io.Writer. It never blocks: if the channel is full the
// write is dropped. It always reports success to the caller.
func (w *asyncFileWriter) Write(p []byte) (int, error) {
	cp := make([]byte, len(p))
	copy(cp, p)
	select {
	case w.ch <- cp:
	default: // drop — fire-and-forget
	}
	return len(p), nil
}

func (w *asyncFileWriter) close() {
	w.once.Do(func() { close(w.ch) })
}
