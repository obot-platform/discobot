package sessionconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/obot-platform/discobot/agent-go/providers"
)

// SubAgentConfig represents a sub-agent defined in .claude/agents/*.md (Claude Code convention).
type SubAgentConfig struct {
	Name             string                     `yaml:"name" json:"name"`
	Description      string                     `yaml:"description" json:"description"`
	Model            string                     `yaml:"model,omitempty" json:"model,omitempty"`
	SupportingModels providers.SupportingModels `yaml:"supportingModels,omitempty" json:"supportingModels,omitempty"`
	AllowedTools     []string                   `yaml:"allowedTools,omitempty" json:"allowedTools,omitempty"`
	DisallowedTools  []string                   `yaml:"disallowedTools,omitempty" json:"disallowedTools,omitempty"`
	MaxTurns         int                        `yaml:"maxTurns,omitempty" json:"maxTurns,omitempty"`
	Prompt           string                     `yaml:"-" json:"prompt"` // Markdown body
}

// discoverSubAgents loads sub-agent configs from .claude/agents/*.md (Claude Code convention).
func discoverSubAgents(projectRoot string) ([]SubAgentConfig, error) {
	agentsDir := filepath.Join(projectRoot, ".claude", "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read agents dir: %w", err)
	}

	// Sort by name for deterministic ordering.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	var agents []SubAgentConfig
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p := filepath.Join(agentsDir, e.Name())
		content, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read agent %s: %w", p, err)
		}

		agent, err := parseSubAgent(e.Name(), string(content))
		if err != nil {
			return nil, fmt.Errorf("parse agent %s: %w", e.Name(), err)
		}
		agents = append(agents, agent)
	}

	return agents, nil
}

// parseSubAgent parses a single sub-agent markdown file.
// The file may have YAML frontmatter followed by the agent's prompt.
func parseSubAgent(filename, content string) (SubAgentConfig, error) {
	fm, body, err := parseFrontmatter(content)
	if err != nil {
		return SubAgentConfig{}, fmt.Errorf("parse frontmatter: %w", err)
	}

	var agent SubAgentConfig

	if fm != nil {
		if err := normalizeSupportingModelsFrontmatter(fm); err != nil {
			return SubAgentConfig{}, err
		}

		// Re-marshal the frontmatter map to YAML and unmarshal into the struct.
		yamlBytes, err := yaml.Marshal(fm)
		if err != nil {
			return SubAgentConfig{}, fmt.Errorf("re-marshal frontmatter: %w", err)
		}
		if err := yaml.Unmarshal(yamlBytes, &agent); err != nil {
			return SubAgentConfig{}, fmt.Errorf("unmarshal frontmatter: %w", err)
		}
	}

	// Default name from filename (without extension).
	if agent.Name == "" {
		agent.Name = strings.TrimSuffix(filename, ".md")
	}

	agent.Prompt = strings.TrimSpace(body)

	return agent, nil
}

func normalizeSupportingModelsFrontmatter(fm map[string]any) error {
	raw, ok := fm["supportingModels"]
	if !ok {
		return nil
	}

	value, ok := raw.(string)
	if !ok {
		return nil
	}

	value = strings.TrimSpace(value)
	if value == "" {
		delete(fm, "supportingModels")
		return nil
	}

	models := make(providers.SupportingModels)
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}

		key, model, ok := strings.Cut(item, "=")
		if !ok {
			return fmt.Errorf("parse supportingModels: expected key=value, got %q", item)
		}

		key = strings.TrimSpace(key)
		model = strings.TrimSpace(model)
		if key == "" || model == "" {
			return fmt.Errorf("parse supportingModels: expected non-empty key=value, got %q", item)
		}

		models[providers.SupportingModelType(key)] = model
	}

	fm["supportingModels"] = models
	return nil
}
