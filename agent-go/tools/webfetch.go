package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
)

type webFetchInput struct {
	URL    string `json:"url"`
	Prompt string `json:"prompt"`
}

const defaultWebFetchUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Discobot/1.0 Safari/537.36"

var tavilyExtractURL = "https://api.tavily.com/extract"

var discobotServicesURL = "https://api.discobot.ai"

var httpClient = &http.Client{
	Timeout: 30 * time.Second,
	CheckRedirect: func(_ *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	},
}

func (e *Executor) executeWebFetch(ctx context.Context, call message.ToolCallPart) (thread.ToolExecuteResult, error) {
	var input webFetchInput
	if err := unmarshalInput(call, &input); err != nil {
		return errResult(call, err.Error()), nil
	}
	if input.URL == "" {
		return errResult(call, "url is required"), nil
	}

	// Upgrade http to https.
	rawURL := input.URL
	if strings.HasPrefix(rawURL, "http://") {
		rawURL = "https://" + strings.TrimPrefix(rawURL, "http://")
	}

	// Use Discobot proxy if configured (managed/hosted environment).
	if token := e.getenv("DISCOBOT_TOKEN"); token != "" {
		baseURL := e.getenv("DISCOBOT_SERVICES_URL")
		if baseURL == "" {
			baseURL = discobotServicesURL
		}
		markdown, err := fetchWithDiscobot(ctx, rawURL, token, baseURL)
		if err == nil {
			return textResult(call, trimWebFetchContent(markdown)), nil
		}

		// Fall back to native fetching so WebFetch still works when the proxy cannot
		// extract a specific URL.
		fallbackMarkdown, fallbackErr := fetchWithNativeHTTP(ctx, rawURL)
		if fallbackErr == nil {
			return textResult(call, trimWebFetchContent(fallbackMarkdown)), nil
		}

		return errResult(call, fmt.Sprintf("discobot extract request failed: %v; native fallback failed: %v", err, fallbackErr)), nil
	}

	if apiKey := e.getenv("TAVILY_API_KEY"); apiKey != "" {
		markdown, err := fetchWithTavily(ctx, rawURL, apiKey)
		if err == nil {
			return textResult(call, trimWebFetchContent(markdown)), nil
		}

		// Fall back to native fetching so WebFetch still works when Tavily cannot
		// extract a specific URL.
		fallbackMarkdown, fallbackErr := fetchWithNativeHTTP(ctx, rawURL)
		if fallbackErr == nil {
			return textResult(call, trimWebFetchContent(fallbackMarkdown)), nil
		}

		return errResult(call, fmt.Sprintf("Tavily request failed: %v; native fallback failed: %v", err, fallbackErr)), nil
	}

	markdown, err := fetchWithNativeHTTP(ctx, rawURL)
	if err != nil {
		return errResult(call, err.Error()), nil
	}

	return textResult(call, trimWebFetchContent(markdown)), nil
}

type tavilyExtractRequest struct {
	APIKey       string   `json:"api_key"`
	URLs         []string `json:"urls"`
	ExtractDepth string   `json:"extract_depth,omitempty"`
}

type tavilyExtractResponse struct {
	Results []struct {
		URL        string `json:"url"`
		Markdown   string `json:"markdown"`
		Content    string `json:"content"`
		RawContent string `json:"raw_content"`
	} `json:"results"`
}

func fetchWithNativeHTTP(ctx context.Context, rawURL string) (string, error) {
	pageURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %v", err)
	}
	req.Header.Set("User-Agent", defaultWebFetchUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.9,*/*;q=0.8")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	const maxBody = 5 * 1024 * 1024 // 5 MB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %v", err)
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") || contentType == "" {
		markdown, err := htmlToMarkdown(body, pageURL)
		if err != nil {
			return "", fmt.Errorf("failed to convert page: %v", err)
		}
		return markdown, nil
	}

	return string(body), nil
}

func fetchWithDiscobot(ctx context.Context, rawURL, token, baseURL string) (string, error) {
	reqBody := tavilyExtractRequest{
		URLs:         []string{rawURL},
		ExtractDepth: "basic",
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal discobot extract request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(baseURL, "/")+"/v1/tavily/extract", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Discobot-Id", token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return "", fmt.Errorf("read discobot extract response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed tavilyExtractResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("parse discobot extract response: %w", err)
	}
	if len(parsed.Results) == 0 {
		return "", fmt.Errorf("no content returned for URL")
	}

	content := parsed.Results[0].Markdown
	if content == "" {
		content = parsed.Results[0].Content
	}
	if content == "" {
		content = parsed.Results[0].RawContent
	}
	if content == "" {
		return "", fmt.Errorf("no extractable content in discobot extract response")
	}
	return content, nil
}

func fetchWithTavily(ctx context.Context, rawURL, apiKey string) (string, error) {
	reqBody := tavilyExtractRequest{
		APIKey:       apiKey,
		URLs:         []string{rawURL},
		ExtractDepth: "basic",
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal Tavily request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tavilyExtractURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return "", fmt.Errorf("read Tavily response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed tavilyExtractResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("parse Tavily response: %w", err)
	}
	if len(parsed.Results) == 0 {
		return "", fmt.Errorf("no content returned for URL")
	}

	content := parsed.Results[0].Markdown
	if content == "" {
		content = parsed.Results[0].Content
	}
	if content == "" {
		content = parsed.Results[0].RawContent
	}
	if content == "" {
		return "", fmt.Errorf("no extractable content in Tavily response")
	}
	return content, nil
}

func trimWebFetchContent(markdown string) string {
	const maxContent = 100_000
	if len(markdown) > maxContent {
		return markdown[:maxContent] + "\n\n[Content truncated]"
	}
	return markdown
}

// htmlToMarkdown converts raw HTML to Markdown using a two-step pipeline:
// 1. go-readability extracts the main article content (strips nav, ads, scripts).
// 2. html-to-markdown converts the cleaned HTML to Markdown.
func htmlToMarkdown(body []byte, pageURL *url.URL) (string, error) {
	// Step 1: Extract readable content.
	article, err := readability.FromReader(bytes.NewReader(body), pageURL)
	if err != nil {
		// If readability fails, fall back to converting the raw HTML.
		return convertHTMLToMarkdown(body)
	}

	// Build a clean HTML fragment from the article.
	var extracted bytes.Buffer
	if title := article.Title(); title != "" {
		fmt.Fprintf(&extracted, "<h1>%s</h1>\n", title)
	}
	if err := article.RenderHTML(&extracted); err != nil {
		return convertHTMLToMarkdown(body)
	}

	// Step 2: Convert the clean HTML to Markdown.
	return convertHTMLToMarkdown(extracted.Bytes())
}

// convertHTMLToMarkdown converts raw HTML bytes to Markdown using
// JohannesKaufmann/html-to-markdown/v2.
func convertHTMLToMarkdown(html []byte) (string, error) {
	md, err := htmltomarkdown.ConvertReader(bytes.NewReader(html))
	if err != nil {
		return "", err
	}
	return string(md), nil
}
