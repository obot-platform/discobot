package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/obot-platform/discobot/agent-go/message"
)

func runWebFetch(t *testing.T, e *Executor, input map[string]string) message.ToolResultOutput {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	result, err := e.Execute(context.Background(), nil, message.ToolCallPart{
		ToolCallID: t.Name(),
		ToolName:   "WebFetch",
		Input:      string(raw),
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	return result.Result.Output
}

func TestWebFetch_UsesBrowserLikeUserAgent(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "")

	oldClient := httpClient
	defer func() { httpClient = oldClient }()

	var gotUserAgent string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello from server"))
	}))
	defer server.Close()

	httpClient = server.Client()
	e := New(t.TempDir(), t.TempDir(), t.Name())
	out := runWebFetch(t, e, map[string]string{
		"url":    server.URL,
		"prompt": "summarize",
	})

	textOut, ok := out.(message.TextOutput)
	if !ok {
		t.Fatalf("expected TextOutput, got %T", out)
	}
	if !strings.Contains(textOut.Value, "hello from server") {
		t.Fatalf("expected server response in output, got: %q", textOut.Value)
	}
	if !strings.Contains(gotUserAgent, "Mozilla/5.0") {
		t.Errorf("expected browser-like User-Agent, got %q", gotUserAgent)
	}
	if !strings.Contains(gotUserAgent, "Discobot/1.0") {
		t.Errorf("expected Discobot identifier in User-Agent, got %q", gotUserAgent)
	}
}

func TestWebFetch_UsesTavilyWhenApiKeySet(t *testing.T) {
	t.Setenv("TAVILY_API_KEY", "test-key")

	oldTavilyURL := tavilyExtractURL
	defer func() { tavilyExtractURL = oldTavilyURL }()

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}

		var req tavilyExtractRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if req.APIKey != "test-key" {
			t.Fatalf("expected api_key test-key, got %q", req.APIKey)
		}
		if len(req.URLs) != 1 || req.URLs[0] != "https://example.com/article" {
			t.Fatalf("unexpected urls payload: %+v", req.URLs)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"url":"https://example.com/article","raw_content":"from tavily extract"}]}`))
	}))
	defer server.Close()
	tavilyExtractURL = server.URL

	e := New(t.TempDir(), t.TempDir(), t.Name())
	out := runWebFetch(t, e, map[string]string{
		"url":    "https://example.com/article",
		"prompt": "extract",
	})

	if !called {
		t.Fatal("expected Tavily extract endpoint to be called")
	}
	textOut, ok := out.(message.TextOutput)
	if !ok {
		t.Fatalf("expected TextOutput, got %T", out)
	}
	if !strings.Contains(textOut.Value, "from tavily extract") {
		t.Fatalf("expected Tavily content in output, got: %q", textOut.Value)
	}
}

func TestWebFetch_UsesDiscobotProxyWhenTokenSet(t *testing.T) {
	t.Setenv("DISCOBOT_TOKEN", "discobot-token")
	t.Setenv("TAVILY_API_KEY", "test-key")

	calledProxy := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledProxy = true
		if r.URL.Path != "/v1/tavily/extract" {
			t.Fatalf("expected /v1/tavily/extract, got %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Discobot-Id"); got != "discobot-token" {
			t.Fatalf("expected X-Discobot-Id header, got %q", got)
		}

		var req tavilyExtractRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if req.APIKey != "" {
			t.Fatalf("expected no api_key in proxy request, got %q", req.APIKey)
		}
		if len(req.URLs) != 1 || req.URLs[0] != "https://example.com/proxy" {
			t.Fatalf("unexpected urls payload: %+v", req.URLs)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"url":"https://example.com/proxy","raw_content":"from discobot proxy"}]}`))
	}))
	defer proxy.Close()
	t.Setenv("DISCOBOT_SERVICES_URL", proxy.URL)

	e := New(t.TempDir(), t.TempDir(), t.Name())
	out := runWebFetch(t, e, map[string]string{
		"url":    "https://example.com/proxy",
		"prompt": "extract",
	})

	if !calledProxy {
		t.Fatal("expected Discobot proxy endpoint to be called")
	}
	textOut, ok := out.(message.TextOutput)
	if !ok {
		t.Fatalf("expected TextOutput, got %T", out)
	}
	if !strings.Contains(textOut.Value, "from discobot proxy") {
		t.Fatalf("expected Discobot proxy content in output, got: %q", textOut.Value)
	}
}
