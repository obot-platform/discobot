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

func runWebSearch(t *testing.T, e *Executor, input map[string]any) message.ToolResultOutput {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	result, err := e.Execute(context.Background(), nil, message.ToolCallPart{
		ToolCallID: t.Name(),
		ToolName:   "WebSearch",
		Input:      string(raw),
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	return result.Result.Output
}

func TestWebSearch_UsesDefaultDiscobotProxyURLWhenTokenSet(t *testing.T) {
	t.Setenv("DISCOBOT_TOKEN", "discobot-token")
	oldBaseURL := discobotServicesURL
	defer func() { discobotServicesURL = oldBaseURL }()

	calledProxy := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledProxy = true
		if got := r.Header.Get("X-Discobot-Id"); got != "discobot-token" {
			t.Fatalf("expected X-Discobot-Id header, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"Example","url":"https://example.com","content":"via default base url"}]}`))
	}))
	defer proxy.Close()
	discobotServicesURL = proxy.URL

	e := New(t.TempDir(), t.TempDir(), t.Name())
	out := runWebSearch(t, e, map[string]any{"query": "golang"})

	if !calledProxy {
		t.Fatal("expected Discobot proxy endpoint to be called via default base URL")
	}
	textOut, ok := out.(message.TextOutput)
	if !ok {
		t.Fatalf("expected TextOutput, got %T", out)
	}
	if !strings.Contains(textOut.Value, "via default base url") {
		t.Fatalf("expected default proxy content in output, got %q", textOut.Value)
	}
}

func TestWebSearch_UsesDiscobotProxyWhenTokenSet(t *testing.T) {
	t.Setenv("DISCOBOT_TOKEN", "discobot-token")
	t.Setenv("TAVILY_API_KEY", "should-not-be-used")

	calledProxy := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calledProxy = true
		if r.URL.Path != "/v1/tavily/search" {
			t.Fatalf("expected /v1/tavily/search, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("X-Discobot-Id"); got != "discobot-token" {
			t.Fatalf("expected X-Discobot-Id header, got %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("expected Accept application/json, got %q", got)
		}

		var req struct {
			APIKey         string   `json:"api_key"`
			Query          string   `json:"query"`
			SearchDepth    string   `json:"search_depth"`
			MaxResults     int      `json:"max_results"`
			IncludeDomains []string `json:"include_domains"`
			ExcludeDomains []string `json:"exclude_domains"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if req.APIKey != "" {
			t.Fatalf("expected no api_key in proxy request, got %q", req.APIKey)
		}
		if req.Query != "golang" {
			t.Fatalf("expected query golang, got %q", req.Query)
		}
		if req.SearchDepth != "basic" {
			t.Fatalf("expected search_depth basic, got %q", req.SearchDepth)
		}
		if req.MaxResults != 5 {
			t.Fatalf("expected max_results 5, got %d", req.MaxResults)
		}
		if len(req.IncludeDomains) != 1 || req.IncludeDomains[0] != "go.dev" {
			t.Fatalf("unexpected include_domains: %#v", req.IncludeDomains)
		}
		if len(req.ExcludeDomains) != 1 || req.ExcludeDomains[0] != "example.com" {
			t.Fatalf("unexpected exclude_domains: %#v", req.ExcludeDomains)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"Go","url":"https://go.dev","content":"The Go programming language"}]}`))
	}))
	defer proxy.Close()
	t.Setenv("DISCOBOT_SERVICES_URL", proxy.URL)

	e := New(t.TempDir(), t.TempDir(), t.Name())
	out := runWebSearch(t, e, map[string]any{
		"query":           "golang",
		"allowed_domains": []string{"go.dev"},
		"blocked_domains": []string{"example.com"},
	})

	if !calledProxy {
		t.Fatal("expected Discobot proxy endpoint to be called")
	}
	textOut, ok := out.(message.TextOutput)
	if !ok {
		t.Fatalf("expected TextOutput, got %T", out)
	}
	if !strings.Contains(textOut.Value, "https://go.dev") {
		t.Fatalf("expected search result URL in output, got %q", textOut.Value)
	}
	if !strings.Contains(textOut.Value, "The Go programming language") {
		t.Fatalf("expected search result content in output, got %q", textOut.Value)
	}
}
