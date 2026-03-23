package proxy

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

type mockReadWriteFlusher struct {
	readBuf bytes.Reader
	writes  bytes.Buffer
	flushes int
}

func newMockReadWriteFlusher(data []byte) *mockReadWriteFlusher {
	return &mockReadWriteFlusher{readBuf: *bytes.NewReader(data)}
}

func (m *mockReadWriteFlusher) Read(p []byte) (int, error) {
	return m.readBuf.Read(p)
}

func (m *mockReadWriteFlusher) Write(p []byte) (int, error) {
	return m.writes.Write(p)
}

func (m *mockReadWriteFlusher) Close() error {
	return nil
}

func (m *mockReadWriteFlusher) Flush() {
	m.flushes++
}

func TestResponseStream_PreservesWriteAndFlush(t *testing.T) {
	body := newMockReadWriteFlusher([]byte("response"))
	stream := &responseStream{source: body}

	writer, ok := any(stream).(io.Writer)
	if !ok {
		t.Fatal("responseStream should implement io.Writer")
	}
	if _, err := writer.Write([]byte("ping")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if got := body.writes.String(); got != "ping" {
		t.Fatalf("underlying writer received %q, want %q", got, "ping")
	}

	flusher, ok := any(stream).(http.Flusher)
	if !ok {
		t.Fatal("responseStream should implement http.Flusher")
	}
	flusher.Flush()
	if body.flushes != 1 {
		t.Fatalf("flush count = %d, want 1", body.flushes)
	}
}
