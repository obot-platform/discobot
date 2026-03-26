package sessionconfig

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_EndToEnd(t *testing.T) {
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, ".git"))

	// Create instruction files.
	writeFile(t, filepath.Join(root, "CLAUDE.md"), "Project instructions.")

	// Create rules.
	rulesDir := filepath.Join(root, ".claude", "rules")
	mkdirAll(t, rulesDir)
	writeFile(t, filepath.Join(rulesDir, "style.md"), "Use gofmt.")

	// Create MCP config.
	writeFile(t, filepath.Join(root, ".mcp.json"), `{
		"mcpServers": {
			"test-server": {"command": "test-mcp"}
		}
	}`)

	// Create sub-agents.
	agentsDir := filepath.Join(root, ".claude", "agents")
	mkdirAll(t, agentsDir)
	writeFile(t, filepath.Join(agentsDir, "helper.md"), "---\nname: helper\n---\nI help with tasks.")

	// Create a skill.
	skillDir := filepath.Join(root, ".claude", "skills", "deploy")
	mkdirAll(t, skillDir)
	writeFile(t, filepath.Join(skillDir, "SKILL.md"), "---\nname: deploy\ndescription: Deploy the project.\n---\nRun deploy.")

	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	// Check system prompt is the default base prompt.
	if !strings.Contains(cfg.SystemPrompt, "You are an AI coding agent powered by Discobot") {
		t.Error("expected default system prompt")
	}

	// Check user instructions are separate.
	if len(cfg.UserInstructions) != 2 {
		t.Errorf("expected 2 user instruction entries (CLAUDE.md + rule), got %d", len(cfg.UserInstructions))
	} else {
		if cfg.UserInstructions[0].Content != "Project instructions." {
			t.Errorf("first instruction content = %q", cfg.UserInstructions[0].Content)
		}
		if cfg.UserInstructions[1].Content != "Use gofmt." {
			t.Errorf("second instruction content = %q", cfg.UserInstructions[1].Content)
		}
	}

	// User instructions should NOT be in the system prompt.
	if strings.Contains(cfg.SystemPrompt, "Project instructions.") {
		t.Error("system prompt should not contain user instructions")
	}

	// Check tools.
	if len(cfg.Tools) == 0 {
		t.Error("expected built-in tools")
	}

	// Check MCP servers.
	if len(cfg.MCPServers) != 1 {
		t.Errorf("expected 1 MCP server, got %d", len(cfg.MCPServers))
	} else if cfg.MCPServers[0].Name != "test-server" {
		t.Errorf("MCP server name = %s, want test-server", cfg.MCPServers[0].Name)
	}

	// Check sub-agents.
	if len(cfg.SubAgents) != 1 {
		t.Errorf("expected 1 sub-agent, got %d", len(cfg.SubAgents))
	} else if cfg.SubAgents[0].Name != "helper" {
		t.Errorf("sub-agent name = %s, want helper", cfg.SubAgents[0].Name)
	}
	if cfg.MaxSubagentDepth != DefaultMaxSubagentDepth {
		t.Errorf("max sub-agent depth = %d, want %d", cfg.MaxSubagentDepth, DefaultMaxSubagentDepth)
	}

	// Check skills — "deploy" must be present (user-level commands may also appear).
	var deploySkill *SkillConfig
	for i := range cfg.Skills {
		if cfg.Skills[i].Name == "deploy" {
			deploySkill = &cfg.Skills[i]
			break
		}
	}
	if deploySkill == nil {
		t.Errorf("expected skill \"deploy\" to be present, got %v", cfg.Skills)
	} else if deploySkill.Description != "Deploy the project." {
		t.Errorf("skill description = %s", deploySkill.Description)
	}
}

func TestLoad_EmptyDirectory(t *testing.T) {
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, ".git"))

	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	// System prompt should contain the default base prompt.
	if !strings.Contains(cfg.SystemPrompt, "You are an AI coding agent powered by Discobot") {
		t.Error("expected default system prompt")
	}

	// User instructions should be empty.
	if len(cfg.UserInstructions) != 0 {
		t.Errorf("expected 0 user instructions, got %d", len(cfg.UserInstructions))
	}

	// Tools should still be populated.
	if len(cfg.Tools) == 0 {
		t.Error("expected built-in tools even with no config files")
	}

	// MCP and sub-agents should be empty.
	if len(cfg.MCPServers) != 0 {
		t.Errorf("expected 0 MCP servers, got %d", len(cfg.MCPServers))
	}
	if len(cfg.SubAgents) != 0 {
		t.Errorf("expected 0 sub-agents, got %d", len(cfg.SubAgents))
	}
	if cfg.MaxSubagentDepth != DefaultMaxSubagentDepth {
		t.Errorf("max sub-agent depth = %d, want %d", cfg.MaxSubagentDepth, DefaultMaxSubagentDepth)
	}
}

func TestLoad_SubdirectoryWorkingDir(t *testing.T) {
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, ".git"))
	writeFile(t, filepath.Join(root, "CLAUDE.md"), "Root instructions")

	subdir := filepath.Join(root, "src", "app")
	mkdirAll(t, subdir)
	writeFile(t, filepath.Join(subdir, "CLAUDE.md"), "App instructions")

	cfg, err := Load(subdir)
	if err != nil {
		t.Fatal(err)
	}

	// Both should be in user instructions.
	if len(cfg.UserInstructions) != 2 {
		t.Fatalf("expected 2 user instruction entries, got %d", len(cfg.UserInstructions))
	}

	// Closer file comes first.
	if cfg.UserInstructions[0].Content != "App instructions" {
		t.Errorf("first = %q, want %q", cfg.UserInstructions[0].Content, "App instructions")
	}
	if cfg.UserInstructions[1].Content != "Root instructions" {
		t.Errorf("second = %q, want %q", cfg.UserInstructions[1].Content, "Root instructions")
	}
}
