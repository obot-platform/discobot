package main

import (
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
	"sort"
	"strings"
	"time"
)

const providerID = "anthropic"

type overlayFile map[string]map[string]map[string]any

type capabilitySupport struct {
	Supported bool `json:"supported"`
}

type thinkingTypes struct {
	Enabled  capabilitySupport `json:"enabled"`
	Adaptive capabilitySupport `json:"adaptive"`
}

type thinkingCapability struct {
	Supported bool          `json:"supported"`
	Types     thinkingTypes `json:"types"`
}

type effortCapability struct {
	Supported bool              `json:"supported"`
	Low       capabilitySupport `json:"low"`
	Medium    capabilitySupport `json:"medium"`
	High      capabilitySupport `json:"high"`
	Max       capabilitySupport `json:"max"`
}

type anthropicCapabilities struct {
	Thinking thinkingCapability `json:"thinking"`
	Effort   effortCapability   `json:"effort"`
}

type anthropicModel struct {
	ID           string                `json:"id"`
	DisplayName  string                `json:"display_name"`
	Capabilities anthropicCapabilities `json:"capabilities"`
}

type modelsResponse struct {
	Data []anthropicModel `json:"data"`
}

type apiErrorResponse struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type syncResult struct {
	Model              string                `json:"model"`
	Listed             bool                  `json:"listed"`
	DisplayName        string                `json:"displayName,omitempty"`
	Reasoning          bool                  `json:"reasoning"`
	ReasoningLevels    []string              `json:"reasoningLevels"`
	DefaultReasonLevel string                `json:"defaultReasonLevel,omitempty"`
	Capabilities       anthropicCapabilities `json:"capabilities"`
}

type config struct {
	OverlayPath string
	Mode        string
	RawReport   string
	BaseURL     string
	APIKey      string
}

func main() {
	cfg := config{
		OverlayPath: defaultOverlayPath(),
		Mode:        "missing",
		BaseURL:     strings.TrimRight(envOr("ANTHROPIC_API_BASE", "https://api.anthropic.com/v1"), "/"),
		APIKey:      firstEnv("ANTHROPIC_API_KEY"),
	}

	flag.StringVar(&cfg.OverlayPath, "overlay", cfg.OverlayPath, "Path to model-overlay.json")
	flag.StringVar(&cfg.Mode, "mode", cfg.Mode, "Sync mode: missing or refresh")
	flag.StringVar(&cfg.RawReport, "raw-report", "", "Optional path for a detailed raw capability report")
	flag.StringVar(&cfg.BaseURL, "base-url", cfg.BaseURL, "Anthropic API base URL")
	flag.StringVar(&cfg.APIKey, "api-key", cfg.APIKey, "Anthropic API key (defaults to ANTHROPIC_API_KEY)")
	flag.Parse()

	if cfg.APIKey == "" {
		fatalf("ANTHROPIC_API_KEY is required")
	}
	if cfg.Mode != "missing" && cfg.Mode != "refresh" {
		fatalf("invalid -mode %q, expected missing or refresh", cfg.Mode)
	}
	if cfg.RawReport == "" {
		cfg.RawReport = filepath.Join(filepath.Dir(cfg.OverlayPath), "model-overlay.anthropic-probed.raw.json")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client, err := newHTTPClient()
	if err != nil {
		fatalf("create HTTP client: %v", err)
	}
	overlay, err := readOverlay(cfg.OverlayPath)
	if err != nil {
		fatalf("read overlay: %v", err)
	}
	models, err := fetchModels(ctx, client, cfg)
	if err != nil {
		fatalf("fetch models: %v", err)
	}
	results := collectResults(cfg.Mode, overlay[providerID], models)
	applyResults(overlay, cfg.Mode, results)
	if cfg.Mode == "refresh" {
		pruneStaleModels(overlay[providerID], models)
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
	candidates := []string{filepath.Join("modelsdev", "model-overlay.json"), "model-overlay.json"}
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

func fetchModels(ctx context.Context, client *http.Client, cfg config) (map[string]anthropicModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", cfg.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s/models: status %d: %s", cfg.BaseURL, resp.StatusCode, apiErrorMessage(body))
	}
	var parsed modelsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}
	models := make(map[string]anthropicModel, len(parsed.Data))
	for _, model := range parsed.Data {
		models[model.ID] = model
	}
	return models, nil
}

func collectResults(mode string, providerOverlay map[string]map[string]any, models map[string]anthropicModel) []syncResult {
	targets := targetModels(mode, providerOverlay, models)
	results := make([]syncResult, 0, len(targets))
	for _, modelID := range targets {
		model, ok := models[modelID]
		if !ok {
			continue
		}
		reasoning, levels, defaultLevel := normalizeCapabilities(model.Capabilities)
		results = append(results, syncResult{
			Model:              modelID,
			Listed:             true,
			DisplayName:        model.DisplayName,
			Reasoning:          reasoning,
			ReasoningLevels:    levels,
			DefaultReasonLevel: defaultLevel,
			Capabilities:       model.Capabilities,
		})
	}
	return results
}

func targetModels(mode string, providerOverlay map[string]map[string]any, models map[string]anthropicModel) []string {
	set := make(map[string]struct{})
	if mode == "refresh" {
		for modelID := range providerOverlay {
			if modelID == "$provider" {
				continue
			}
			if _, ok := models[modelID]; ok {
				set[modelID] = struct{}{}
			}
		}
	} else {
		for modelID := range models {
			if _, exists := providerOverlay[modelID]; exists {
				continue
			}
			set[modelID] = struct{}{}
		}
	}
	ids := make([]string, 0, len(set))
	for modelID := range set {
		ids = append(ids, modelID)
	}
	sort.Strings(ids)
	return ids
}

func normalizeCapabilities(capabilities anthropicCapabilities) (bool, []string, string) {
	if !capabilities.Thinking.Supported {
		return false, []string{}, ""
	}
	levels := []string{"auto"}
	if capabilities.Effort.Low.Supported {
		levels = append(levels, "low")
	}
	if capabilities.Effort.Medium.Supported {
		levels = append(levels, "medium")
	}
	if capabilities.Effort.High.Supported {
		levels = append(levels, "high")
	}
	if capabilities.Effort.Max.Supported {
		levels = append(levels, "xhigh")
	}
	levels = append(levels, "none")
	levels = unique(levels)
	return true, levels, "auto"
}

func unique(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func applyResults(overlay overlayFile, mode string, results []syncResult) {
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
		entry["reasoning"] = result.Reasoning
		entry["reasoningLevels"] = result.ReasoningLevels
		if result.DefaultReasonLevel != "" {
			entry["defaultReasonLevel"] = result.DefaultReasonLevel
		}
		providerOverlay[result.Model] = entry
	}
}

func pruneStaleModels(providerOverlay map[string]map[string]any, models map[string]anthropicModel) {
	for modelID := range providerOverlay {
		if modelID == "$provider" {
			continue
		}
		if _, ok := models[modelID]; ok {
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

func writeRawReport(path string, results []syncResult) error {
	wrapped := map[string]map[string]syncResult{providerID: {}}
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

func apiErrorMessage(body []byte) string {
	var response apiErrorResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return strings.TrimSpace(string(body))
	}
	if response.Error == nil || response.Error.Message == "" {
		return strings.TrimSpace(string(body))
	}
	return strings.ReplaceAll(response.Error.Message, "\n", " ")
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
