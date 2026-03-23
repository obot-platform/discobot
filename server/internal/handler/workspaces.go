package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	gitrepo "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/obot-platform/discobot/server/internal/jobs"
	"github.com/obot-platform/discobot/server/internal/middleware"
	"github.com/obot-platform/discobot/server/internal/service"
)

var githubShorthandPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})/[A-Za-z0-9._-]+$`)

const githubRepoListCacheTTL = 2 * time.Minute

type githubRepoListCacheEntry struct {
	Repos     []githubRepositoryListItem
	ExpiresAt time.Time
}

var githubRepoListCache = struct {
	sync.RWMutex
	Entries map[string]githubRepoListCacheEntry
}{Entries: make(map[string]githubRepoListCacheEntry)}

type validateWorkspaceRequest struct {
	Path       string `json:"path"`
	SourceType string `json:"sourceType"`
}

type validateWorkspaceResponse struct {
	Path           string       `json:"path"`
	SourceType     string       `json:"sourceType"`
	Valid          bool         `json:"valid"`
	Classification string       `json:"classification"`
	Error          string       `json:"error,omitempty"`
	Suggestions    []Suggestion `json:"suggestions"`
	AuthProvider   string       `json:"authProvider,omitempty"`
	AuthRequired   bool         `json:"authRequired,omitempty"`
	AuthMessage    string       `json:"authMessage,omitempty"`
}

// ValidateWorkspace validates a workspace path/repo and returns suggestions.
// POST /api/projects/{projectId}/workspaces/validate
func (h *Handler) ValidateWorkspace(w http.ResponseWriter, r *http.Request) {
	projectID := middleware.GetProjectID(r.Context())

	var req validateWorkspaceRequest
	if err := h.DecodeJSON(r, &req); err != nil {
		h.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	req.Path = strings.TrimSpace(req.Path)
	if req.SourceType == "" {
		req.SourceType = "local"
	}

	if req.SourceType != "local" && req.SourceType != "git" {
		h.Error(w, http.StatusBadRequest, "sourceType must be local or git")
		return
	}

	if req.Path == "" {
		h.JSON(w, http.StatusOK, validateWorkspaceResponse{
			Path:           "",
			SourceType:     req.SourceType,
			Valid:          false,
			Classification: service.LocalWorkspaceClassificationInvalid,
			Suggestions:    []Suggestion{},
		})
		return
	}

	if req.SourceType == "local" {
		normalizedPath, classification, err := h.workspaceService.ValidateLocalWorkspacePath(req.Path)
		response := validateWorkspaceResponse{
			Path:           normalizedPath,
			SourceType:     "local",
			Classification: classification,
			Suggestions:    getDirectorySuggestions(req.Path),
		}
		if err != nil {
			response.Error = err.Error()
		} else {
			response.Valid = classification == service.LocalWorkspaceClassificationNew ||
				classification == service.LocalWorkspaceClassificationEmpty ||
				classification == service.LocalWorkspaceClassificationExistingGit
		}

		h.JSON(w, http.StatusOK, response)
		return
	}

	normalizedPath := normalizeGitPath(req.Path)
	response := validateWorkspaceResponse{
		Path:           normalizedPath,
		SourceType:     "git",
		Classification: "invalid",
		Suggestions:    getRepoSuggestions(r.Context(), req.Path, ""),
	}

	if !looksLikeGitRepositoryInput(req.Path) {
		response.Error = "Enter a repository URL or org/repo."
		h.JSON(w, http.StatusOK, response)
		return
	}

	githubToken, hasGitHubToken, tokenErr := h.getGitHubToken(r.Context(), projectID)
	if tokenErr != nil {
		response.Error = fmt.Sprintf("failed to check GitHub credential: %v", tokenErr)
		h.JSON(w, http.StatusOK, response)
		return
	}

	response.Suggestions = getRepoSuggestions(r.Context(), req.Path, githubToken)

	if isGitHubRepositoryURL(normalizedPath) && !hasGitHubToken {
		response.AuthProvider = service.ProviderGitHub
		response.AuthRequired = true
		response.AuthMessage = "Sign in to GitHub to validate and clone private repositories."
	}

	if err := validateGitRemote(r.Context(), normalizedPath, githubToken); err != nil {
		response.Error = err.Error()

		if isGitHubRepositoryURL(normalizedPath) {
			if !hasGitHubToken || errors.Is(err, transport.ErrAuthenticationRequired) || errors.Is(err, transport.ErrAuthorizationFailed) || isGitHubRepositoryNotFound(err) {
				response.AuthProvider = service.ProviderGitHub
				response.AuthRequired = true
				if !hasGitHubToken {
					response.AuthMessage = "Sign in to GitHub to validate and clone private repositories."
				} else if isGitHubRepositoryNotFound(err) {
					response.AuthMessage = "Repository not found. If this repo is private, ensure your GitHub credential has repo access and org SSO authorization."
				} else {
					response.AuthMessage = "GitHub authentication failed. Reconnect your GitHub credential and try again."
				}
			}
		}

		h.JSON(w, http.StatusOK, response)
		return
	}

	response.Valid = true
	response.Classification = "cloneable"
	h.JSON(w, http.StatusOK, response)
}

func normalizeGitPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if githubShorthandPattern.MatchString(trimmed) {
		return fmt.Sprintf("https://github.com/%s", trimmed)
	}

	if strings.HasPrefix(trimmed, "github.com/") {
		return "https://" + trimmed
	}

	if strings.HasPrefix(trimmed, "www.github.com/") {
		return "https://" + strings.TrimPrefix(trimmed, "www.")
	}

	return trimmed
}

func looksLikeGitRepositoryInput(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return false
	}

	if githubShorthandPattern.MatchString(trimmed) {
		return true
	}

	if strings.HasPrefix(trimmed, "github.com/") || strings.HasPrefix(trimmed, "www.github.com/") {
		return true
	}

	if strings.HasPrefix(trimmed, "git@") {
		return strings.Contains(trimmed, ":")
	}

	if strings.Contains(trimmed, "://") {
		parsedURL, err := url.Parse(trimmed)
		if err != nil {
			return false
		}
		return parsedURL.Scheme != "" && parsedURL.Host != ""
	}

	return false
}

func getRepoSuggestions(ctx context.Context, query string, githubToken string) []Suggestion {
	normalizedGitHubQuery := normalizeGitHubRepoQuery(query)

	if githubToken != "" && normalizedGitHubQuery != "" {
		repoSuggestions, repoErr := getGitHubRepoSuggestions(ctx, query, githubToken)
		orgSuggestions := []Suggestion{}
		var orgErr error

		if !strings.Contains(normalizedGitHubQuery, "/") {
			orgSuggestions, orgErr = getGitHubOrgSuggestions(ctx, normalizedGitHubQuery, githubToken)
		}

		if repoErr == nil && orgErr == nil && (len(repoSuggestions) > 0 || len(orgSuggestions) > 0) {
			merged := make([]Suggestion, 0, len(repoSuggestions)+len(orgSuggestions))
			seen := make(map[string]struct{}, len(repoSuggestions)+len(orgSuggestions))

			appendSuggestion := func(suggestion Suggestion) {
				if suggestion.Value == "" {
					return
				}
				if _, exists := seen[suggestion.Value]; exists {
					return
				}
				seen[suggestion.Value] = struct{}{}
				merged = append(merged, suggestion)
			}

			for _, suggestion := range repoSuggestions {
				appendSuggestion(suggestion)
			}
			for _, suggestion := range orgSuggestions {
				appendSuggestion(suggestion)
			}

			if len(merged) > 10 {
				return merged[:10]
			}

			return merged
		}

		if strings.Contains(normalizedGitHubQuery, "/") {
			return []Suggestion{}
		}
	}

	staticSuggestions := getStaticRepoSuggestions(query)
	return staticSuggestions
}

func getStaticRepoSuggestions(query string) []Suggestion {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return []Suggestion{}
	}

	seen := map[string]struct{}{}
	suggestions := make([]Suggestion, 0, 3)
	appendSuggestion := func(value string) {
		if value == "" {
			return
		}
		if _, exists := seen[value]; exists {
			return
		}
		seen[value] = struct{}{}
		suggestions = append(suggestions, Suggestion{Value: value, Type: "repo", Valid: true})
	}

	if looksLikeGitRepositoryInput(trimmed) {
		if !githubShorthandPattern.MatchString(trimmed) {
			appendSuggestion(normalizeGitPath(trimmed))
		}
	}

	if strings.HasPrefix(trimmed, "https://github.com/") {
		short := strings.TrimPrefix(trimmed, "https://github.com/")
		short = strings.TrimSuffix(short, ".git")
		short = strings.Trim(short, "/")
		if githubShorthandPattern.MatchString(short) {
			appendSuggestion(short)
		}
	}

	if len(suggestions) > 10 {
		return suggestions[:10]
	}

	return suggestions
}

type githubRepositorySearchResponse struct {
	Items []struct {
		FullName string `json:"full_name"`
		Name     string `json:"name"`
	} `json:"items"`
}

type githubUserSearchResponse struct {
	Items []struct {
		Login string `json:"login"`
	} `json:"items"`
}

func getGitHubRepoSuggestions(ctx context.Context, query string, githubToken string) ([]Suggestion, error) {
	if githubToken == "" {
		return nil, nil
	}

	if owner, repoPrefix, ok := splitGitHubOwnerRepoPrefix(normalizeGitHubRepoQuery(query)); ok {
		ownerSuggestions, ownerErr := getGitHubOwnerRepoSuggestions(ctx, owner, repoPrefix, githubToken)
		if ownerErr == nil && len(ownerSuggestions) > 0 {
			return ownerSuggestions, nil
		}
	}

	searchQuery := buildGitHubSearchQuery(query)
	if searchQuery == "" {
		return nil, nil
	}

	requestContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	requestURL := "https://api.github.com/search/repositories?q=" + url.QueryEscape(searchQuery) + "&per_page=10"
	req, err := http.NewRequestWithContext(requestContext, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	setGitHubRequestHeaders(req, githubToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("github repository search failed with status %d", resp.StatusCode)
	}

	var payload githubRepositorySearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	suggestions := make([]Suggestion, 0, len(payload.Items))
	for _, item := range payload.Items {
		if item.FullName == "" {
			continue
		}
		suggestions = append(suggestions, Suggestion{
			Value: item.FullName,
			Type:  "repo",
			Valid: true,
		})
	}

	return suggestions, nil
}

func getGitHubOrgSuggestions(ctx context.Context, query string, githubToken string) ([]Suggestion, error) {
	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" {
		return nil, nil
	}

	requestContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	search := trimmedQuery + " in:login type:org"
	requestURL := "https://api.github.com/search/users?q=" + url.QueryEscape(search) + "&per_page=5"
	req, err := http.NewRequestWithContext(requestContext, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	setGitHubRequestHeaders(req, githubToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("github org search failed with status %d", resp.StatusCode)
	}

	var payload githubUserSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	suggestions := make([]Suggestion, 0, len(payload.Items))
	for _, item := range payload.Items {
		if item.Login == "" {
			continue
		}

		suggestions = append(suggestions, Suggestion{
			Value:          item.Login + "/",
			Type:           "repo",
			Valid:          false,
			Classification: "org",
		})
	}

	return suggestions, nil
}

type githubRepositoryListItem struct {
	Name     string `json:"name"`
	FullName string `json:"full_name"`
}

func getGitHubOwnerRepoSuggestions(ctx context.Context, owner, repoPrefix, githubToken string) ([]Suggestion, error) {
	if owner == "" {
		return nil, nil
	}

	if cachedRepos, ok := getCachedGitHubOwnerRepos(githubToken, owner); ok {
		return filterGitHubOwnerRepoSuggestions(cachedRepos, strings.ToLower(repoPrefix)), nil
	}

	requestContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	repos, err := fetchGitHubOwnerRepos(requestContext, owner, githubToken)
	if err != nil {
		return nil, err
	}

	setCachedGitHubOwnerRepos(githubToken, owner, repos)
	return filterGitHubOwnerRepoSuggestions(repos, strings.ToLower(repoPrefix)), nil
}

func fetchGitHubOwnerRepos(ctx context.Context, owner, githubToken string) ([]githubRepositoryListItem, error) {
	endpoints := []string{
		"https://api.github.com/orgs/" + url.PathEscape(owner) + "/repos?per_page=100&type=all&sort=updated",
		"https://api.github.com/users/" + url.PathEscape(owner) + "/repos?per_page=100&type=owner&sort=updated",
	}

	merged := make([]githubRepositoryListItem, 0, 100)
	seen := make(map[string]struct{}, 100)
	var lastErr error

	for _, endpoint := range endpoints {
		items, err := fetchGitHubRepositoryList(ctx, endpoint, githubToken)
		if err != nil {
			lastErr = err
			continue
		}

		for _, item := range items {
			if item.FullName == "" {
				continue
			}
			if _, exists := seen[item.FullName]; exists {
				continue
			}

			seen[item.FullName] = struct{}{}
			merged = append(merged, item)
		}
	}

	if len(merged) == 0 && lastErr != nil {
		return nil, lastErr
	}

	return merged, nil
}

func filterGitHubOwnerRepoSuggestions(repos []githubRepositoryListItem, lowerPrefix string) []Suggestion {
	suggestions := make([]Suggestion, 0, 10)
	for _, item := range repos {
		if item.FullName == "" {
			continue
		}
		if lowerPrefix != "" && !strings.HasPrefix(strings.ToLower(item.Name), lowerPrefix) {
			continue
		}

		suggestions = append(suggestions, Suggestion{
			Value: item.FullName,
			Type:  "repo",
			Valid: true,
		})

		if len(suggestions) >= 10 {
			break
		}
	}

	return suggestions
}

func getCachedGitHubOwnerRepos(githubToken string, owner string) ([]githubRepositoryListItem, bool) {
	cacheKey := buildGitHubRepoListCacheKey(githubToken, owner)
	now := time.Now()

	githubRepoListCache.RLock()
	entry, ok := githubRepoListCache.Entries[cacheKey]
	githubRepoListCache.RUnlock()

	if !ok || now.After(entry.ExpiresAt) {
		if ok {
			githubRepoListCache.Lock()
			delete(githubRepoListCache.Entries, cacheKey)
			githubRepoListCache.Unlock()
		}
		return nil, false
	}

	return append([]githubRepositoryListItem(nil), entry.Repos...), true
}

func setCachedGitHubOwnerRepos(githubToken string, owner string, repos []githubRepositoryListItem) {
	cacheKey := buildGitHubRepoListCacheKey(githubToken, owner)
	now := time.Now()

	githubRepoListCache.Lock()
	for key, entry := range githubRepoListCache.Entries {
		if now.After(entry.ExpiresAt) {
			delete(githubRepoListCache.Entries, key)
		}
	}

	githubRepoListCache.Entries[cacheKey] = githubRepoListCacheEntry{
		Repos:     append([]githubRepositoryListItem(nil), repos...),
		ExpiresAt: now.Add(githubRepoListCacheTTL),
	}
	githubRepoListCache.Unlock()
}

func buildGitHubRepoListCacheKey(githubToken string, owner string) string {
	sum := sha256.Sum256([]byte(githubToken))
	return strings.ToLower(strings.TrimSpace(owner)) + ":" + hex.EncodeToString(sum[:])
}

func fetchGitHubRepositoryList(ctx context.Context, requestURL string, githubToken string) ([]githubRepositoryListItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	setGitHubRequestHeaders(req, githubToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("github repo list failed with status %d", resp.StatusCode)
	}

	var payload []githubRepositoryListItem
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	return payload, nil
}

func splitGitHubOwnerRepoPrefix(query string) (owner string, repoPrefix string, ok bool) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" || !strings.Contains(trimmed, "/") {
		return "", "", false
	}

	owner, repoPrefix, _ = strings.Cut(trimmed, "/")
	owner = strings.TrimSpace(owner)
	repoPrefix = strings.TrimSpace(repoPrefix)
	if owner == "" {
		return "", "", false
	}

	return owner, repoPrefix, true
}

func setGitHubRequestHeaders(req *http.Request, githubToken string) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "discobot-server")
	if githubToken != "" {
		req.Header.Set("Authorization", "Bearer "+githubToken)
	}
}

func buildGitHubSearchQuery(input string) string {
	normalized := normalizeGitHubRepoQuery(input)
	if normalized == "" {
		return ""
	}

	if strings.Contains(normalized, "/") {
		owner, repo, _ := strings.Cut(normalized, "/")
		owner = strings.TrimSpace(owner)
		repo = strings.TrimSpace(repo)
		if owner == "" {
			return ""
		}
		if repo == "" {
			return "user:" + owner
		}
		return repo + " in:name user:" + owner
	}

	return normalized + " in:name"
}

func normalizeGitHubRepoQuery(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "git@github.com:") {
		path := strings.TrimPrefix(trimmed, "git@github.com:")
		return strings.Trim(strings.TrimSuffix(path, ".git"), "/")
	}

	if strings.HasPrefix(trimmed, "github.com/") {
		path := strings.TrimPrefix(trimmed, "github.com/")
		return strings.Trim(strings.TrimSuffix(path, ".git"), "/")
	}

	if strings.HasPrefix(trimmed, "www.github.com/") {
		path := strings.TrimPrefix(trimmed, "www.github.com/")
		return strings.Trim(strings.TrimSuffix(path, ".git"), "/")
	}

	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		parsedURL, err := url.Parse(trimmed)
		if err != nil {
			return trimmed
		}
		if parsedURL.Host == "github.com" || parsedURL.Host == "www.github.com" {
			return strings.Trim(strings.TrimSuffix(parsedURL.Path, ".git"), "/")
		}
		return trimmed
	}

	return strings.Trim(strings.TrimSuffix(trimmed, ".git"), "/")
}

func isGitHubRepositoryURL(raw string) bool {
	lowerValue := strings.ToLower(strings.TrimSpace(raw))
	if strings.HasPrefix(lowerValue, "git@github.com:") {
		return true
	}

	parsedURL, err := url.Parse(lowerValue)
	if err != nil {
		return false
	}

	return parsedURL.Host == "github.com" || parsedURL.Host == "www.github.com"
}

func isGitHubRepositoryNotFound(err error) bool {
	if err == nil {
		return false
	}

	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "repository not found")
}

func validateGitRemote(ctx context.Context, remoteURL string, githubToken string) error {
	remote := gitrepo.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{remoteURL},
	})

	listOptions := &gitrepo.ListOptions{}
	if isGitHubRepositoryURL(remoteURL) && githubToken != "" {
		listOptions.Auth = &githttp.BasicAuth{
			Username: "x-access-token",
			Password: githubToken,
		}
	}

	if _, err := remote.ListContext(ctx, listOptions); err != nil {
		return fmt.Errorf("repository is not cloneable: %w", err)
	}

	return nil
}

func (h *Handler) getGitHubToken(ctx context.Context, projectID string) (string, bool, error) {
	tokens, err := h.credentialService.GetOAuthTokens(ctx, projectID, service.ProviderGitHub)
	if err == nil && tokens != nil && tokens.AccessToken != "" {
		return tokens.AccessToken, true, nil
	}

	if err != nil && !errors.Is(err, service.ErrCredentialNotFound) {
		return "", false, err
	}

	apiKeyCredential, err := h.credentialService.GetAPIKey(ctx, projectID, service.ProviderGitHub)
	if err == nil && apiKeyCredential != nil && apiKeyCredential.APIKey != "" {
		return apiKeyCredential.APIKey, true, nil
	}

	if err != nil && !errors.Is(err, service.ErrCredentialNotFound) {
		return "", false, err
	}

	return "", false, nil
}

// ListWorkspaces returns all workspaces for a project
func (h *Handler) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	projectID := middleware.GetProjectID(r.Context())

	workspaces, err := h.workspaceService.ListWorkspaces(r.Context(), projectID)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "Failed to list workspaces")
		return
	}

	h.JSON(w, http.StatusOK, map[string]any{"workspaces": mapWorkspaceResponses(workspaces)})
}

// CreateWorkspace creates a new workspace
func (h *Handler) CreateWorkspace(w http.ResponseWriter, r *http.Request) {
	projectID := middleware.GetProjectID(r.Context())

	var req struct {
		Path        string  `json:"path"`
		DisplayName *string `json:"displayName"`
		SourceType  string  `json:"sourceType"`
		Provider    string  `json:"provider"`
	}
	if err := h.DecodeJSON(r, &req); err != nil {
		h.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.Path == "" {
		h.Error(w, http.StatusBadRequest, "Path is required")
		return
	}
	if req.SourceType == "" {
		req.SourceType = "local"
	}

	workspace, err := h.workspaceService.CreateWorkspace(r.Context(), projectID, req.Path, req.SourceType, req.Provider)
	if err != nil {
		// Pass through the detailed error message from the service
		h.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	// Update display name if provided
	if req.DisplayName != nil {
		// Get the model workspace and update it
		modelWorkspace, err := h.store.GetWorkspaceByID(r.Context(), workspace.ID)
		if err != nil {
			h.Error(w, http.StatusInternalServerError, "Failed to get workspace for update")
			return
		}
		modelWorkspace.DisplayName = req.DisplayName
		if err := h.store.UpdateWorkspace(r.Context(), modelWorkspace); err != nil {
			h.Error(w, http.StatusInternalServerError, "Failed to update workspace")
			return
		}
		// Update the response object
		workspace.DisplayName = req.DisplayName
	}

	// Enqueue workspace initialization job
	if err := h.jobQueue.Enqueue(r.Context(), jobs.WorkspaceInitPayload{ProjectID: projectID, WorkspaceID: workspace.ID}); err != nil {
		h.Error(w, http.StatusInternalServerError, "Failed to enqueue workspace initialization")
		return
	}

	h.JSON(w, http.StatusCreated, mapWorkspaceResponse(workspace))
}

// GetWorkspace returns a single workspace
func (h *Handler) GetWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceId")

	workspace, err := h.workspaceService.GetWorkspaceWithSessions(r.Context(), workspaceID)
	if err != nil {
		h.Error(w, http.StatusNotFound, "Workspace not found")
		return
	}

	h.JSON(w, http.StatusOK, mapWorkspaceResponse(workspace))
}

// UpdateWorkspace updates a workspace
func (h *Handler) UpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceId")

	// Parse raw JSON to detect which fields were sent
	var rawReq map[string]interface{}
	if err := h.DecodeJSON(r, &rawReq); err != nil {
		h.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Get the existing workspace
	workspace, err := h.store.GetWorkspaceByID(r.Context(), workspaceID)
	if err != nil {
		h.Error(w, http.StatusNotFound, "Workspace not found")
		return
	}

	modified := false

	// Update path if provided
	if path, ok := rawReq["path"].(string); ok {
		// UpdateWorkspace in service returns the full workspace, but we need to re-fetch
		// to ensure we have the latest model
		_, err = h.workspaceService.UpdateWorkspace(r.Context(), workspaceID, path)
		if err != nil {
			h.Error(w, http.StatusInternalServerError, "Failed to update workspace")
			return
		}
		// Re-fetch to get updated workspace
		workspace, err = h.store.GetWorkspaceByID(r.Context(), workspaceID)
		if err != nil {
			h.Error(w, http.StatusInternalServerError, "Failed to get updated workspace")
			return
		}
		modified = true
	}

	// Update display name if the field was sent (even if null to clear it)
	if displayName, ok := rawReq["displayName"]; ok {
		if displayName == nil {
			workspace.DisplayName = nil
		} else if str, ok := displayName.(string); ok {
			workspace.DisplayName = &str
		}
		modified = true
	}

	// Note: Provider cannot be updated after creation - it's set only on Create

	// Save if we modified the workspace
	if modified {
		if err := h.store.UpdateWorkspace(r.Context(), workspace); err != nil {
			h.Error(w, http.StatusInternalServerError, "Failed to update workspace")
			return
		}
	}

	// Map to service workspace for response
	serviceWorkspace, err := h.workspaceService.GetWorkspace(r.Context(), workspaceID)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "Failed to get updated workspace")
		return
	}
	h.JSON(w, http.StatusOK, mapWorkspaceResponse(serviceWorkspace))
}

// DeleteWorkspace deletes a workspace
// Query params:
//   - deleteFiles: if "true", also delete the workspace files from disk
func (h *Handler) DeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceId")
	deleteFiles := r.URL.Query().Get("deleteFiles") == "true"

	if err := h.workspaceService.DeleteWorkspace(r.Context(), workspaceID, deleteFiles); err != nil {
		h.Error(w, http.StatusInternalServerError, "Failed to delete workspace")
		return
	}

	h.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

// GetProviders returns all sandbox providers with their status.
// GET /api/projects/{projectId}/workspaces/providers
func (h *Handler) GetProviders(w http.ResponseWriter, _ *http.Request) {
	h.JSON(w, http.StatusOK, map[string]any{
		"providers": h.sandboxManager.ListProviderStatuses(),
		"default":   h.sandboxManager.DefaultProviderName(),
	})
}

// GetProvider returns the status of a specific sandbox provider.
// GET /api/projects/{projectId}/workspaces/providers/{provider}
func (h *Handler) GetProvider(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "provider")

	status, ok := h.sandboxManager.GetProviderStatus(name)
	if !ok {
		h.Error(w, http.StatusNotFound, "Provider not found")
		return
	}

	h.JSON(w, http.StatusOK, status)
}
