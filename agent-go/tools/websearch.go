package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
)

type webSearchInput struct {
	Query          string   `json:"query"`
	AllowedDomains []string `json:"allowed_domains"`
	BlockedDomains []string `json:"blocked_domains"`
}

type searchResult struct {
	Title   string
	URL     string
	Content string
}

func (e *Executor) executeWebSearch(ctx context.Context, call message.ToolCallPart) (thread.ToolExecuteResult, error) {
	var input webSearchInput
	if err := unmarshalInput(call, &input); err != nil {
		return errResult(call, err.Error()), nil
	}
	if input.Query == "" {
		return errResult(call, "query is required"), nil
	}

	// Try Discobot proxy if configured (managed/hosted environment).
	if token := e.getenv("DISCOBOT_TOKEN"); token != "" {
		baseURL := e.getenv("DISCOBOT_SERVICES_URL")
		if baseURL == "" {
			baseURL = discobotServicesURL
		}
		return e.searchDiscobot(ctx, call, input, token, baseURL)
	}

	// Try Tavily API if configured.
	if apiKey := e.getenv("TAVILY_API_KEY"); apiKey != "" {
		return e.searchTavily(ctx, call, input, apiKey)
	}

	// Try Brave Search API if configured.
	if apiKey := e.getenv("BRAVE_SEARCH_API_KEY"); apiKey != "" {
		return e.searchBrave(ctx, call, input, apiKey)
	}

	return errResult(call, "WebSearch requires a search provider. Set DISCOBOT_TOKEN, or set TAVILY_API_KEY or BRAVE_SEARCH_API_KEY to enable web search."), nil
}

func formatSearchResults(results []searchResult) string {
	if len(results) == 0 {
		return "No results found."
	}
	var sb strings.Builder
	for i, r := range results {
		fmt.Fprintf(&sb, "## Result %d: %s\n", i+1, r.Title)
		fmt.Fprintf(&sb, "**URL:** %s\n\n", r.URL)
		fmt.Fprintf(&sb, "%s\n\n", r.Content)
		sb.WriteString("---\n\n")
	}
	return strings.TrimRight(sb.String(), "\n-")
}

// --- Tavily ---

type tavilyRequest struct {
	APIKey         string   `json:"api_key"`
	Query          string   `json:"query"`
	SearchDepth    string   `json:"search_depth"`
	MaxResults     int      `json:"max_results"`
	IncludeDomains []string `json:"include_domains,omitempty"`
	ExcludeDomains []string `json:"exclude_domains,omitempty"`
}

type tavilyResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
}

func (e *Executor) searchTavily(ctx context.Context, call message.ToolCallPart, input webSearchInput, apiKey string) (thread.ToolExecuteResult, error) {
	reqBody := tavilyRequest{
		APIKey:         apiKey,
		Query:          input.Query,
		SearchDepth:    "basic",
		MaxResults:     5,
		IncludeDomains: input.AllowedDomains,
		ExcludeDomains: input.BlockedDomains,
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.tavily.com/search",
		strings.NewReader(string(bodyBytes)))
	if err != nil {
		return errResult(call, err.Error()), nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return errResult(call, fmt.Sprintf("Tavily request failed: %v", err)), nil
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return errResult(call, fmt.Sprintf("Tavily API error %d: %s", resp.StatusCode, string(data))), nil
	}

	var result tavilyResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return errResult(call, fmt.Sprintf("failed to parse Tavily response: %v", err)), nil
	}

	var results []searchResult
	for _, r := range result.Results {
		results = append(results, searchResult{Title: r.Title, URL: r.URL, Content: r.Content})
	}
	return textResult(call, formatSearchResults(results)), nil
}

// --- Brave Search ---

type braveSearchResponse struct {
	Web struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"results"`
	} `json:"web"`
}

func (e *Executor) searchBrave(ctx context.Context, call message.ToolCallPart, input webSearchInput, apiKey string) (thread.ToolExecuteResult, error) {
	params := url.Values{}
	params.Set("q", input.Query)
	params.Set("count", "5")

	reqURL := "https://api.search.brave.com/res/v1/web/search?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return errResult(call, err.Error()), nil
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return errResult(call, fmt.Sprintf("Brave Search request failed: %v", err)), nil
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return errResult(call, fmt.Sprintf("Brave Search API error %d: %s", resp.StatusCode, string(data))), nil
	}

	var raw braveSearchResponse
	if err := json.Unmarshal(data, &raw); err != nil {
		return errResult(call, fmt.Sprintf("failed to parse Brave response: %v", err)), nil
	}

	var results []searchResult
	for _, r := range raw.Web.Results {
		if len(input.AllowedDomains) > 0 && !domainAllowed(r.URL, input.AllowedDomains) {
			continue
		}
		if domainBlocked(r.URL, input.BlockedDomains) {
			continue
		}
		results = append(results, searchResult{Title: r.Title, URL: r.URL, Content: r.Description})
	}

	return textResult(call, formatSearchResults(results)), nil
}

// --- Discobot proxy ---

func (e *Executor) searchDiscobot(ctx context.Context, call message.ToolCallPart, input webSearchInput, token, baseURL string) (thread.ToolExecuteResult, error) {
	reqBody := struct {
		Query          string   `json:"query"`
		SearchDepth    string   `json:"search_depth"`
		MaxResults     int      `json:"max_results"`
		IncludeDomains []string `json:"include_domains,omitempty"`
		ExcludeDomains []string `json:"exclude_domains,omitempty"`
	}{
		Query:          input.Query,
		SearchDepth:    "basic",
		MaxResults:     5,
		IncludeDomains: input.AllowedDomains,
		ExcludeDomains: input.BlockedDomains,
	}

	bodyBytes, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(baseURL, "/")+"/v1/tavily/search",
		strings.NewReader(string(bodyBytes)))
	if err != nil {
		return errResult(call, err.Error()), nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Discobot-Id", token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return errResult(call, fmt.Sprintf("discobot search request failed: %v", err)), nil
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return errResult(call, fmt.Sprintf("discobot search API error %d: %s", resp.StatusCode, string(data))), nil
	}

	var result tavilyResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return errResult(call, fmt.Sprintf("failed to parse search response: %v", err)), nil
	}

	var results []searchResult
	for _, r := range result.Results {
		results = append(results, searchResult{Title: r.Title, URL: r.URL, Content: r.Content})
	}
	return textResult(call, formatSearchResults(results)), nil
}

func domainAllowed(rawURL string, allowed []string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.TrimPrefix(u.Hostname(), "www.")
	for _, d := range allowed {
		if host == strings.TrimPrefix(d, "www.") {
			return true
		}
	}
	return false
}

func domainBlocked(rawURL string, blocked []string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.TrimPrefix(u.Hostname(), "www.")
	for _, d := range blocked {
		if host == strings.TrimPrefix(d, "www.") {
			return true
		}
	}
	return false
}
