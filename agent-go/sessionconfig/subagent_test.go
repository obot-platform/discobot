package sessionconfig

import (
	"path/filepath"
	"testing"

	"github.com/obot-platform/discobot/agent-go/providers"
)

func TestDiscoverSubAgents_WithFrontmatter(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, ".claude", "agents")
	mkdirAll(t, agentsDir)

	writeFile(t, filepath.Join(agentsDir, "reviewer.md"), `---
name: code-reviewer
description: Reviews code for quality
model: gpt-4
supportingModels:
  thread_summarization: openai/gpt-5.4-mini
allowedTools:
  - Read
  - Grep
  - Glob
maxTurns: 5
---
You are a code reviewer. Review the code for quality and correctness.`)

	agents, err := discoverSubAgents(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}

	a := agents[0]
	if a.Name != "code-reviewer" {
		t.Errorf("name = %s, want code-reviewer", a.Name)
	}
	if a.Description != "Reviews code for quality" {
		t.Errorf("description = %s", a.Description)
	}
	if a.Model != "gpt-4" {
		t.Errorf("model = %s, want gpt-4", a.Model)
	}
	if got := a.SupportingModels[providers.SupportingModelThreadSummarization]; got != "openai/gpt-5.4-mini" {
		t.Errorf("supportingModels.thread_summarization = %q", got)
	}
	if len(a.AllowedTools) != 3 {
		t.Errorf("allowedTools = %v, want 3 items", a.AllowedTools)
	}
	if a.MaxTurns != 5 {
		t.Errorf("maxTurns = %d, want 5", a.MaxTurns)
	}
	if a.Prompt != "You are a code reviewer. Review the code for quality and correctness." {
		t.Errorf("prompt = %q", a.Prompt)
	}
}

func TestDiscoverSubAgents_WithSupportingModelsKeyValueString(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, ".claude", "agents")
	mkdirAll(t, agentsDir)

	writeFile(t, filepath.Join(agentsDir, "reviewer.md"), `---
name: code-reviewer
supportingModels: thread_summarization=openai/gpt-5.4-nano
---
You are a code reviewer.`)

	agents, err := discoverSubAgents(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if got := agents[0].SupportingModels[providers.SupportingModelThreadSummarization]; got != "openai/gpt-5.4-nano" {
		t.Fatalf("supportingModels.thread_summarization = %q", got)
	}
}

func TestDiscoverSubAgents_WithSupportingModelsKeyValueStringList(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, ".claude", "agents")
	mkdirAll(t, agentsDir)

	writeFile(t, filepath.Join(agentsDir, "reviewer.md"), `---
name: code-reviewer
supportingModels: thread_summarization=openai/gpt-5.4-nano, custom_helper=anthropic/claude-haiku-4-5-20251001
---
You are a code reviewer.`)

	agents, err := discoverSubAgents(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if got := agents[0].SupportingModels[providers.SupportingModelThreadSummarization]; got != "openai/gpt-5.4-nano" {
		t.Fatalf("thread_summarization = %q", got)
	}
	if got := agents[0].SupportingModels[providers.SupportingModelType("custom_helper")]; got != "anthropic/claude-haiku-4-5-20251001" {
		t.Fatalf("custom_helper = %q", got)
	}
}

func TestDiscoverSubAgents_InvalidSupportingModelsKeyValueString(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, ".claude", "agents")
	mkdirAll(t, agentsDir)

	writeFile(t, filepath.Join(agentsDir, "reviewer.md"), `---
name: code-reviewer
supportingModels: thread_summarization
---
You are a code reviewer.`)

	_, err := discoverSubAgents(root)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestDiscoverSubAgents_NoFrontmatter(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, ".claude", "agents")
	mkdirAll(t, agentsDir)

	writeFile(t, filepath.Join(agentsDir, "simple.md"), "Just a simple agent prompt.")

	agents, err := discoverSubAgents(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}

	a := agents[0]
	if a.Name != "simple" {
		t.Errorf("name = %s, want simple (from filename)", a.Name)
	}
	if a.Prompt != "Just a simple agent prompt." {
		t.Errorf("prompt = %q", a.Prompt)
	}
}

func TestDiscoverSubAgents_MultipleAgents(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, ".claude", "agents")
	mkdirAll(t, agentsDir)

	writeFile(t, filepath.Join(agentsDir, "alpha.md"), "---\nname: alpha\n---\nAlpha agent.")
	writeFile(t, filepath.Join(agentsDir, "beta.md"), "---\nname: beta\n---\nBeta agent.")

	agents, err := discoverSubAgents(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}

	// Should be sorted by filename.
	if agents[0].Name != "alpha" {
		t.Errorf("first agent = %s, want alpha", agents[0].Name)
	}
	if agents[1].Name != "beta" {
		t.Errorf("second agent = %s, want beta", agents[1].Name)
	}
}

func TestDiscoverSubAgents_MissingDir(t *testing.T) {
	root := t.TempDir()
	agents, err := discoverSubAgents(root)
	if err != nil {
		t.Fatal(err)
	}
	if agents != nil {
		t.Errorf("expected nil for missing dir, got %v", agents)
	}
}

func TestDiscoverSubAgents_SkipsNonMarkdown(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, ".claude", "agents")
	mkdirAll(t, agentsDir)

	writeFile(t, filepath.Join(agentsDir, "agent.md"), "---\nname: real\n---\nReal agent.")
	writeFile(t, filepath.Join(agentsDir, "notes.txt"), "Not an agent.")
	mkdirAll(t, filepath.Join(agentsDir, "subdir"))

	agents, err := discoverSubAgents(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent (skipping non-md files), got %d", len(agents))
	}
	if agents[0].Name != "real" {
		t.Errorf("name = %s, want real", agents[0].Name)
	}
}

func TestDiscoverSubAgents_DisallowedTools(t *testing.T) {
	root := t.TempDir()
	agentsDir := filepath.Join(root, ".claude", "agents")
	mkdirAll(t, agentsDir)

	writeFile(t, filepath.Join(agentsDir, "safe.md"), `---
name: safe-agent
disallowedTools:
  - Bash
  - Write
---
I cannot run commands or write files.`)

	agents, err := discoverSubAgents(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if len(agents[0].DisallowedTools) != 2 {
		t.Errorf("disallowedTools = %v, want 2 items", agents[0].DisallowedTools)
	}
}
