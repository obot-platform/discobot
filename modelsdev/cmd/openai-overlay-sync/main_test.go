package main

import (
	"reflect"
	"testing"
)

func TestParseSupportedValues(t *testing.T) {
	message := "Unsupported value: 'xhigh' is not supported with the 'gpt-5' model. Supported values are: 'minimal', 'low', 'medium', and 'high'."
	got := parseSupportedValues(message)
	want := []string{"minimal", "low", "medium", "high"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseSupportedValues() = %v, want %v", got, want)
	}
}

func TestInferReasoningEnabled(t *testing.T) {
	probes := []reasoningProbe{{
		Level:  "minimal",
		Status: 400,
		Error:  "Unsupported value: 'minimal' is not supported with the 'gpt-5.1-codex-max' model. Supported values are: 'low', 'medium', 'high', and 'xhigh'.",
	}}
	got := inferReasoning(true, probes)
	want := reasoningInference{Known: true, Value: true, Levels: []string{"low", "medium", "high", "xhigh"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inferReasoning() = %#v, want %#v", got, want)
	}
}

func TestInferReasoningDisabled(t *testing.T) {
	probes := []reasoningProbe{{
		Level:  "low",
		Status: 400,
		Error:  unsupportedReasoningMessage,
	}}
	got := inferReasoning(true, probes)
	want := reasoningInference{Known: true, Value: false, Levels: []string{}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inferReasoning() = %#v, want %#v", got, want)
	}
}

func TestInferReasoningUnknown(t *testing.T) {
	probes := []reasoningProbe{{
		Level:  "low",
		Status: 500,
		Error:  "An error occurred while processing your request.",
	}}
	got := inferReasoning(true, probes)
	if got.Known {
		t.Fatalf("inferReasoning() = %#v, want unknown result", got)
	}
}

func TestApplyResultsRefresh(t *testing.T) {
	overlay := overlayFile{
		providerID: {
			"gpt-5": {
				"defaultReasonLevel": "medium",
				"reasoning":          false,
				"reasoningLevels":    []any{"low"},
			},
		},
	}
	applyResults(overlay, "refresh", []modelProbeResult{{
		Model:           "gpt-5",
		ReasoningKnown:  true,
		Reasoning:       true,
		ReasoningLevels: []string{"minimal", "low", "medium", "high"},
	}})

	got := overlay[providerID]["gpt-5"]
	if got["defaultReasonLevel"] != "medium" {
		t.Fatalf("defaultReasonLevel was not preserved: %#v", got)
	}
	if got["reasoning"] != true {
		t.Fatalf("reasoning = %#v, want true", got["reasoning"])
	}
	levels, ok := got["reasoningLevels"].([]string)
	if !ok {
		t.Fatalf("reasoningLevels type = %T, want []string", got["reasoningLevels"])
	}
	wantLevels := []string{"minimal", "low", "medium", "high"}
	if !reflect.DeepEqual(levels, wantLevels) {
		t.Fatalf("reasoningLevels = %v, want %v", levels, wantLevels)
	}
}

func TestPruneStaleModels(t *testing.T) {
	providerOverlay := map[string]map[string]any{
		"$provider": {"name": "OpenAI"},
		"gpt-5":     {"reasoning": true},
		"old-model": {"reasoning": false},
	}
	pruneStaleModels(providerOverlay, map[string]struct{}{"gpt-5": {}})
	if _, ok := providerOverlay["old-model"]; ok {
		t.Fatal("expected stale model to be deleted")
	}
	if _, ok := providerOverlay["gpt-5"]; !ok {
		t.Fatal("expected live model to remain")
	}
	if _, ok := providerOverlay["$provider"]; !ok {
		t.Fatal("expected $provider metadata to remain")
	}
}

func TestMissingModeDoesNotPruneStaleModels(t *testing.T) {
	overlay := overlayFile{
		providerID: {
			"gpt-5":     {"reasoning": true},
			"old-model": {"reasoning": false},
		},
	}
	applyResults(overlay, "missing", nil)
	if _, ok := overlay[providerID]["old-model"]; !ok {
		t.Fatal("expected stale model to remain in missing mode")
	}
}

func TestApplyResultsMissingOnlyAddsNewModels(t *testing.T) {
	overlay := overlayFile{
		providerID: {
			"gpt-5": {
				"reasoning":       false,
				"reasoningLevels": []any{"low"},
			},
		},
	}
	applyResults(overlay, "missing", []modelProbeResult{
		{
			Model:           "gpt-5",
			ReasoningKnown:  true,
			Reasoning:       true,
			ReasoningLevels: []string{"minimal", "low", "medium", "high"},
		},
		{
			Model:           "gpt-5.4-mini",
			ReasoningKnown:  true,
			Reasoning:       true,
			ReasoningLevels: []string{"minimal", "low", "medium", "high"},
		},
	})

	existing := overlay[providerID]["gpt-5"]
	if existing["reasoning"] != false {
		t.Fatalf("existing model was modified in missing mode: %#v", existing)
	}
	added := overlay[providerID]["gpt-5.4-mini"]
	if added["reasoning"] != true {
		t.Fatalf("new model was not added correctly: %#v", added)
	}
}
