package main

import (
	"reflect"
	"testing"
)

func TestNormalizeCapabilitiesAdaptiveWithEffort(t *testing.T) {
	reasoning, levels, defaultLevel := normalizeCapabilities(anthropicCapabilities{
		Thinking: thinkingCapability{
			Supported: true,
			Types: thinkingTypes{
				Enabled:  capabilitySupport{Supported: true},
				Adaptive: capabilitySupport{Supported: true},
			},
		},
		Effort: effortCapability{
			Supported: true,
			Low:       capabilitySupport{Supported: true},
			Medium:    capabilitySupport{Supported: true},
			High:      capabilitySupport{Supported: true},
			Max:       capabilitySupport{Supported: true},
		},
	})
	if !reasoning {
		t.Fatal("expected reasoning=true")
	}
	wantLevels := []string{"auto", "low", "medium", "high", "xhigh", "none"}
	if !reflect.DeepEqual(levels, wantLevels) {
		t.Fatalf("levels = %v, want %v", levels, wantLevels)
	}
	if defaultLevel != "auto" {
		t.Fatalf("defaultReasonLevel = %q, want auto", defaultLevel)
	}
}

func TestNormalizeCapabilitiesThinkingWithoutEffort(t *testing.T) {
	reasoning, levels, defaultLevel := normalizeCapabilities(anthropicCapabilities{
		Thinking: thinkingCapability{
			Supported: true,
			Types: thinkingTypes{
				Enabled: capabilitySupport{Supported: true},
			},
		},
	})
	if !reasoning {
		t.Fatal("expected reasoning=true")
	}
	wantLevels := []string{"auto", "none"}
	if !reflect.DeepEqual(levels, wantLevels) {
		t.Fatalf("levels = %v, want %v", levels, wantLevels)
	}
	if defaultLevel != "auto" {
		t.Fatalf("defaultReasonLevel = %q, want auto", defaultLevel)
	}
}

func TestNormalizeCapabilitiesNoThinking(t *testing.T) {
	reasoning, levels, defaultLevel := normalizeCapabilities(anthropicCapabilities{})
	if reasoning {
		t.Fatal("expected reasoning=false")
	}
	if !reflect.DeepEqual(levels, []string{}) {
		t.Fatalf("levels = %v, want []", levels)
	}
	if defaultLevel != "" {
		t.Fatalf("defaultReasonLevel = %q, want empty", defaultLevel)
	}
}

func TestPruneStaleModels(t *testing.T) {
	providerOverlay := map[string]map[string]any{
		"$provider":                  {"name": "Anthropic"},
		"claude-sonnet-4-6":          {"reasoning": true},
		"claude-3-7-sonnet-20250219": {"reasoning": true},
	}
	pruneStaleModels(providerOverlay, map[string]anthropicModel{"claude-sonnet-4-6": {ID: "claude-sonnet-4-6"}})
	if _, ok := providerOverlay["claude-3-7-sonnet-20250219"]; ok {
		t.Fatal("expected stale model to be deleted")
	}
	if _, ok := providerOverlay["claude-sonnet-4-6"]; !ok {
		t.Fatal("expected live model to remain")
	}
	if _, ok := providerOverlay["$provider"]; !ok {
		t.Fatal("expected $provider metadata to remain")
	}
}

func TestMissingModeDoesNotPruneStaleModels(t *testing.T) {
	overlay := overlayFile{
		providerID: {
			"claude-sonnet-4-6":          {"reasoning": true},
			"claude-3-7-sonnet-20250219": {"reasoning": true},
		},
	}
	applyResults(overlay, "missing", nil)
	if _, ok := overlay[providerID]["claude-3-7-sonnet-20250219"]; !ok {
		t.Fatal("expected stale model to remain in missing mode")
	}
}

func TestApplyResultsMissingOnlyAddsNewModels(t *testing.T) {
	overlay := overlayFile{
		providerID: {
			"claude-opus-4-5-20251101": {
				"defaultReasonLevel": "auto",
				"reasoning":          true,
				"reasoningLevels":    []any{"auto", "low", "medium", "high", "xhigh", "none"},
			},
		},
	}
	applyResults(overlay, "missing", []syncResult{
		{
			Model:              "claude-opus-4-5-20251101",
			Reasoning:          true,
			ReasoningLevels:    []string{"auto", "low", "medium", "high", "none"},
			DefaultReasonLevel: "auto",
		},
		{
			Model:              "claude-3-haiku-20240307",
			Reasoning:          false,
			ReasoningLevels:    []string{},
			DefaultReasonLevel: "",
		},
	})

	existing := overlay[providerID]["claude-opus-4-5-20251101"]
	if levels, ok := existing["reasoningLevels"].([]any); !ok || len(levels) != 6 {
		t.Fatalf("existing model was modified in missing mode: %#v", existing)
	}
	added := overlay[providerID]["claude-3-haiku-20240307"]
	if added["reasoning"] != false {
		t.Fatalf("new model reasoning = %#v, want false", added["reasoning"])
	}
}
