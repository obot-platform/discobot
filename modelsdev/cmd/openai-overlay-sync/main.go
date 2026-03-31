package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	providerID                  = "openai"
	unsupportedReasoningMessage = "Unsupported parameter: 'reasoning.effort' is not supported with this model."
)

var (
	supportedValuesPattern = regexp.MustCompile(`Supported values are:\s*(.+?)\.?$`)
	quotedValuePattern     = regexp.MustCompile(`'([^']+)'`)
	reasoningLevelOrder    = map[string]int{"none": 0, "minimal": 1, "low": 2, "medium": 3, "high": 4, "xhigh": 5}
	candidateLevels        = []string{"minimal", "none", "low", "medium", "high", "xhigh"}
)

type overlayFile map[string]map[string]map[string]any

type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

type apiErrorResponse struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type reasoningProbe struct {
	Level  string `json:"level"`
	Status int    `json:"status"`
	Error  string `json:"error,omitempty"`
}

type supportProbe struct {
	Status int    `json:"status"`
	Error  string `json:"error,omitempty"`
}

type reasoningInference struct {
	Known  bool
	Value  bool
	Levels []string
}

type modelProbeResult struct {
	Model               string           `json:"model"`
	Listed              bool             `json:"listed"`
	ReasoningKnown      bool             `json:"reasoningKnown"`
	Reasoning           bool             `json:"reasoning"`
	ReasoningLevels     []string         `json:"reasoningLevels"`
	FunctionTool        supportProbe     `json:"functionTool"`
	CustomTool          supportProbe     `json:"customTool"`
	ReasoningProbeTrace []reasoningProbe `json:"reasoningProbes"`
}

type config struct {
	OverlayPath string
	Mode        string
	RawReport   string
	BaseURL     string
	APIKey      string
	ProbeTools  bool
	Concurrency int
}

func main() {
	cfg := config{
		OverlayPath: defaultOverlayPath(),
		Mode:        "missing",
		BaseURL:     strings.TrimRight(envOr("OPENAI_API_BASE", "https://api.openai.com/v1"), "/"),
		APIKey:      firstEnv("OPENAI_API_KEY", "OPENAI_APIKEY"),
	}

	flag.StringVar(&cfg.OverlayPath, "overlay", cfg.OverlayPath, "Path to model-overlay.json")
	flag.StringVar(&cfg.Mode, "mode", cfg.Mode, "Sync mode: missing or refresh")
	flag.StringVar(&cfg.RawReport, "raw-report", "", "Optional path for a detailed raw probe report")
	flag.StringVar(&cfg.BaseURL, "base-url", cfg.BaseURL, "OpenAI API base URL")
	flag.StringVar(&cfg.APIKey, "api-key", cfg.APIKey, "OpenAI API key (defaults to OPENAI_API_KEY)")
	flag.BoolVar(&cfg.ProbeTools, "probe-tools", false, "Also probe function and custom tool support")
	flag.IntVar(&cfg.Concurrency, "concurrency", 10, "Maximum number of models to probe in parallel")
	flag.Parse()

	if cfg.APIKey == "" {
		fatalf("OPENAI_API_KEY is required")
	}
	if cfg.Mode != "missing" && cfg.Mode != "refresh" {
		fatalf("invalid -mode %q, expected missing or refresh", cfg.Mode)
	}
	if cfg.Concurrency < 1 {
		fatalf("invalid -concurrency %d, expected >= 1", cfg.Concurrency)
	}
	if cfg.RawReport == "" {
		cfg.RawReport = filepath.Join(filepath.Dir(cfg.OverlayPath), "model-overlay.openai-probed.raw.json")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client, err := newHTTPClient()
	if err != nil {
		fatalf("create HTTP client: %v", err)
	}

	overlay, err := readOverlay(cfg.OverlayPath)
	if err != nil {
		fatalf("read overlay: %v", err)
	}

	apiModels, err := fetchModelIDs(ctx, client, cfg)
	if err != nil {
		fatalf("fetch models: %v", err)
	}

	results, err := probeModels(ctx, client, cfg, overlay, apiModels)
	if err != nil {
		fatalf("probe models: %v", err)
	}

	applyResults(overlay, cfg.Mode, results)
	if cfg.Mode == "refresh" {
		pruneStaleModels(overlay[providerID], apiModels)
	}

	if err := writeOverlay(cfg.OverlayPath, overlay); err != nil {
		fatalf("write overlay: %v", err)
	}
	if err := writeRawReport(cfg.RawReport, results); err != nil {
		fatalf("write raw report: %v", err)
	}

	fmt.Printf("Updated %s\n", cfg.OverlayPath)
	fmt.Printf("Wrote %s\n", cfg.RawReport)
}

func defaultOverlayPath() string {
	candidates := []string{
		filepath.Join("modelsdev", "model-overlay.json"),
		"model-overlay.json",
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return filepath.Join("modelsdev", "model-overlay.json")
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func newHTTPClient() (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if certPath := strings.TrimSpace(os.Getenv("NODE_EXTRA_CA_CERTS")); certPath != "" {
		pool, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system cert pool: %w", err)
		}
		if pool == nil {
			pool = x509.NewCertPool()
		}
		pemData, err := os.ReadFile(certPath)
		if err != nil {
			return nil, fmt.Errorf("read NODE_EXTRA_CA_CERTS %q: %w", certPath, err)
		}
		if !pool.AppendCertsFromPEM(pemData) {
			return nil, fmt.Errorf("append NODE_EXTRA_CA_CERTS %q", certPath)
		}
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		}
		transport.TLSClientConfig.RootCAs = pool
	}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second}, nil
}

func readOverlay(path string) (overlayFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var overlay overlayFile
	if err := json.Unmarshal(data, &overlay); err != nil {
		return nil, err
	}
	if overlay == nil {
		overlay = make(overlayFile)
	}
	return overlay, nil
}

func fetchModelIDs(ctx context.Context, client *http.Client, cfg config) (map[string]struct{}, error) {
	var response modelsResponse
	if err := doJSON(ctx, client, cfg, http.MethodGet, cfg.BaseURL+"/models", nil, &response); err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(response.Data))
	for _, item := range response.Data {
		if item.ID != "" {
			ids[item.ID] = struct{}{}
		}
	}
	return ids, nil
}

func probeModels(ctx context.Context, client *http.Client, cfg config, overlay overlayFile, apiModels map[string]struct{}) ([]modelProbeResult, error) {
	targets := targetModels(cfg.Mode, overlay[providerID], apiModels)
	results := make([]modelProbeResult, len(targets))
	workerCount := cfg.Concurrency
	if workerCount > len(targets) {
		workerCount = len(targets)
	}
	if workerCount == 0 {
		return results, nil
	}

	type job struct {
		index   int
		modelID string
	}

	jobs := make(chan job)
	errCh := make(chan error, workerCount)
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				result, err := probeModel(ctx, client, cfg, job.modelID, apiModels)
				if err != nil {
					select {
					case errCh <- fmt.Errorf("probe %s: %w", job.modelID, err):
					default:
					}
					continue
				}
				results[job.index] = result
			}
		}()
	}

	for index, modelID := range targets {
		jobs <- job{index: index, modelID: modelID}
	}
	close(jobs)
	wg.Wait()
	close(errCh)
	if err := <-errCh; err != nil {
		return nil, err
	}
	return results, nil
}

func targetModels(mode string, providerOverlay map[string]map[string]any, apiModels map[string]struct{}) []string {
	set := make(map[string]struct{})
	if mode == "refresh" {
		for modelID := range providerOverlay {
			if modelID == "$provider" {
				continue
			}
			set[modelID] = struct{}{}
		}
	} else {
		for modelID := range apiModels {
			if _, exists := providerOverlay[modelID]; exists {
				continue
			}
			set[modelID] = struct{}{}
		}
	}
	models := make([]string, 0, len(set))
	for modelID := range set {
		models = append(models, modelID)
	}
	sort.Strings(models)
	return models
}

func probeModel(ctx context.Context, client *http.Client, cfg config, modelID string, apiModels map[string]struct{}) (modelProbeResult, error) {
	_, listed := apiModels[modelID]
	result := modelProbeResult{Model: modelID, Listed: listed}

	for _, level := range candidateLevels {
		payload := map[string]any{
			"model":             modelID,
			"input":             "Reply with ok.",
			"max_output_tokens": 16,
			"store":             false,
			"reasoning": map[string]any{
				"effort": level,
			},
		}
		status, body, err := doRequestWithRetry(ctx, client, cfg, http.MethodPost, cfg.BaseURL+"/responses", payload)
		probe := reasoningProbe{Level: level}
		if err != nil {
			probe.Error = err.Error()
		} else {
			probe.Status = status
			probe.Error = apiErrorMessage(body)
		}
		result.ReasoningProbeTrace = append(result.ReasoningProbeTrace, probe)
	}

	if cfg.ProbeTools {
		functionPayload := map[string]any{
			"model":             modelID,
			"input":             "You may use the tool if useful.",
			"max_output_tokens": 16,
			"store":             false,
			"tools": []map[string]any{{
				"type":        "function",
				"name":        "ping",
				"description": "Returns pong.",
				"parameters": map[string]any{
					"type":                 "object",
					"properties":           map[string]any{},
					"additionalProperties": false,
				},
			}},
		}
		status, body, err := doRequestWithRetry(ctx, client, cfg, http.MethodPost, cfg.BaseURL+"/responses", functionPayload)
		if err != nil {
			result.FunctionTool = supportProbe{Error: err.Error()}
		} else {
			result.FunctionTool = supportProbe{Status: status, Error: apiErrorMessage(body)}
		}

		customPayload := map[string]any{
			"model":             modelID,
			"input":             "You may use the custom tool if useful.",
			"max_output_tokens": 16,
			"store":             false,
			"tools": []map[string]any{{
				"type":        "custom",
				"name":        "apply_patch",
				"description": "Applies a patch.",
				"format": map[string]any{
					"type":       "grammar",
					"syntax":     "lark",
					"definition": "start: /[\\s\\S]+/",
				},
			}},
		}
		status, body, err = doRequestWithRetry(ctx, client, cfg, http.MethodPost, cfg.BaseURL+"/responses", customPayload)
		if err != nil {
			result.CustomTool = supportProbe{Error: err.Error()}
		} else {
			result.CustomTool = supportProbe{Status: status, Error: apiErrorMessage(body)}
		}
	}

	inference := inferReasoning(listed, result.ReasoningProbeTrace)
	result.ReasoningKnown = inference.Known
	result.Reasoning = inference.Value
	result.ReasoningLevels = inference.Levels
	return result, nil
}

func doRequestWithRetry(ctx context.Context, client *http.Client, cfg config, method, url string, payload any) (int, []byte, error) {
	status, body, err := doJSONRequest(ctx, client, cfg, method, url, payload)
	if err != nil {
		return 0, nil, err
	}
	if status >= 500 && status <= 599 {
		return doJSONRequest(ctx, client, cfg, method, url, payload)
	}
	return status, body, nil
}

func doJSONRequest(ctx context.Context, client *http.Client, cfg config, method, url string, payload any) (int, []byte, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, data, nil
}

func doJSON(ctx context.Context, client *http.Client, cfg config, method, url string, payload any, out any) error {
	status, body, err := doJSONRequest(ctx, client, cfg, method, url, payload)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("%s %s: status %d: %s", method, url, status, apiErrorMessage(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return err
	}
	return nil
}

func apiErrorMessage(body []byte) string {
	var response apiErrorResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return ""
	}
	if response.Error == nil {
		return ""
	}
	return strings.ReplaceAll(response.Error.Message, "\n", " ")
}

func inferReasoning(listed bool, probes []reasoningProbe) reasoningInference {
	if !listed {
		return reasoningInference{Known: true, Value: false, Levels: []string{}}
	}
	levels := make([]string, 0)
	seen := make(map[string]struct{})
	unsupported := false
	for _, probe := range probes {
		if probe.Status == http.StatusOK {
			levels = appendIfMissing(levels, seen, probe.Level)
			continue
		}
		if strings.Contains(probe.Error, "Supported values are:") {
			unsupported = true
			for _, level := range parseSupportedValues(probe.Error) {
				levels = appendIfMissing(levels, seen, level)
			}
			continue
		}
		if strings.Contains(probe.Error, unsupportedReasoningMessage) {
			unsupported = true
		}
	}
	if len(levels) > 0 {
		sortReasoningLevels(levels)
		return reasoningInference{Known: true, Value: true, Levels: levels}
	}
	if unsupported {
		return reasoningInference{Known: true, Value: false, Levels: []string{}}
	}
	return reasoningInference{}
}

func parseSupportedValues(message string) []string {
	match := supportedValuesPattern.FindStringSubmatch(message)
	if len(match) != 2 {
		return nil
	}
	matches := quotedValuePattern.FindAllStringSubmatch(match[1], -1)
	levels := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, parts := range matches {
		if len(parts) != 2 {
			continue
		}
		levels = appendIfMissing(levels, seen, parts[1])
	}
	sortReasoningLevels(levels)
	return levels
}

func appendIfMissing(levels []string, seen map[string]struct{}, level string) []string {
	if level == "" {
		return levels
	}
	if _, ok := seen[level]; ok {
		return levels
	}
	seen[level] = struct{}{}
	return append(levels, level)
}

func sortReasoningLevels(levels []string) {
	sort.Slice(levels, func(i, j int) bool {
		left, right := levels[i], levels[j]
		li, lok := reasoningLevelOrder[left]
		rj, rok := reasoningLevelOrder[right]
		if lok && rok && li != rj {
			return li < rj
		}
		if lok != rok {
			return lok
		}
		return left < right
	})
}

func applyResults(overlay overlayFile, mode string, results []modelProbeResult) {
	if overlay[providerID] == nil {
		overlay[providerID] = make(map[string]map[string]any)
	}
	providerOverlay := overlay[providerID]
	for _, result := range results {
		entry, exists := providerOverlay[result.Model]
		if !exists {
			entry = make(map[string]any)
		}
		if mode == "missing" && exists {
			continue
		}
		if !result.ReasoningKnown {
			if !exists {
				continue
			}
			providerOverlay[result.Model] = entry
			continue
		}
		entry["reasoning"] = result.Reasoning
		entry["reasoningLevels"] = result.ReasoningLevels
		providerOverlay[result.Model] = entry
	}
}

func pruneStaleModels(providerOverlay map[string]map[string]any, apiModels map[string]struct{}) {
	for modelID := range providerOverlay {
		if modelID == "$provider" {
			continue
		}
		if _, ok := apiModels[modelID]; ok {
			continue
		}
		delete(providerOverlay, modelID)
	}
}

func writeOverlay(path string, overlay overlayFile) error {
	data, err := json.MarshalIndent(overlay, "", "\t")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func writeRawReport(path string, results []modelProbeResult) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	wrapped := map[string]map[string]modelProbeResult{providerID: {}}
	for _, result := range results {
		wrapped[providerID][result.Model] = result
	}
	data, err := json.MarshalIndent(wrapped, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
