package sessionconfig

import (
	"log"

	"github.com/obot-platform/discobot/agent-go/providers"
)

const DefaultMaxSubagentDepth = 4

// InstructionEntry represents a discovered instruction file with metadata.
type InstructionEntry struct {
	// Path is the display path (e.g., "CLAUDE.md", "~/.claude/CLAUDE.md").
	Path string

	// Description describes the source (e.g., "project instructions, checked into the codebase").
	Description string

	// Content is the file content.
	Content string
}

// SessionConfig holds the discovered session configuration.
type SessionConfig struct {
	// SystemPrompt is the default base system prompt (behavioral instructions).
	SystemPrompt string

	// UserInstructions are discovered CLAUDE.md, AGENTS.md, and rules files.
	// These are delivered separately from the system prompt in <system-reminder> tags.
	UserInstructions []InstructionEntry

	// Tools are the built-in tool definitions sent to the LLM provider.
	Tools []providers.ToolDefinition

	// MCPServers are parsed MCP server definitions from .mcp.json files.
	// These are not connected yet — just parsed for future use.
	MCPServers []MCPServerConfig

	// SubAgents are sub-agent configurations from .claude/agents/*.md.
	SubAgents []SubAgentConfig

	// MaxSubagentDepth limits how many nested Task/Agent hops may run beneath a
	// top-level thread. Top-level threads are depth 0.
	MaxSubagentDepth int

	// Skills are discovered skill configurations from .claude/skills/ and
	// .claude/commands/. They are listed in the system-reminder so the
	// model knows which slash commands are available.
	Skills []SkillConfig
}

// Load discovers and loads session configuration from the given working directory.
// Non-critical errors (missing optional files) are logged as warnings.
// Returns an error only for I/O failures on files that do exist.
func Load(cwd string) (*SessionConfig, error) {
	cfg := &SessionConfig{}

	// 1. Set the default base system prompt.
	cfg.SystemPrompt = defaultSystemPrompt()
	cfg.MaxSubagentDepth = DefaultMaxSubagentDepth

	// 2. Discover user instruction files (CLAUDE.md, AGENTS.md, rules).
	entries, err := discoverInstructions(cwd)
	if err != nil {
		return nil, err
	}
	cfg.UserInstructions = entries

	// 3. Load built-in tool definitions.
	cfg.Tools = BuiltinTools("")

	// 4. Discover MCP server configs.
	projectRoot := findProjectRoot(cwd)
	mcpServers, err := discoverMCPServers(projectRoot)
	if err != nil {
		log.Printf("sessionconfig: warning: MCP discovery: %v", err)
		// Non-fatal — continue without MCP servers.
	} else {
		cfg.MCPServers = mcpServers
	}

	// 5. Discover sub-agent configs.
	subAgents, err := discoverSubAgents(projectRoot)
	if err != nil {
		log.Printf("sessionconfig: warning: sub-agent discovery: %v", err)
		// Non-fatal — continue without sub-agents.
	} else {
		cfg.SubAgents = subAgents
	}

	// 6. Discover skills.
	skills, err := discoverSkills(projectRoot)
	if err != nil {
		log.Printf("sessionconfig: warning: skill discovery: %v", err)
		// Non-fatal — continue without skills.
	} else {
		cfg.Skills = skills
	}

	return cfg, nil
}
