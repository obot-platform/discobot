package server

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	gosdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	acpagent "github.com/obot-platform/discobot/agent-go/acp/agent"
)

// acpClaudeCodeBackend is the DISCOBOT_AGENT_BACKEND value that routes prompts
// through the official Claude Code CLI's ACP server adapter instead of the
// built-in API-key agent.
const acpClaudeCodeBackend = "claude-code-acp"

// claudeCodeACPCommand is the stdio ACP server installed in the sandbox image
// (see Dockerfile: npm install -g @zed-industries/claude-code-acp).
const claudeCodeACPCommand = "claude-code-acp"

// acpConnectTimeout bounds the spawn + ACP initialize handshake so a stalled
// handshake can't wedge agent-api startup. On timeout we fall back to the
// default agent instead of blocking initialization forever.
const acpConnectTimeout = 20 * time.Second

// connectClaudeCodeACP spawns claude-code-acp as a stdio subprocess and wraps it
// as a Discobot conversation agent. The child inherits this process's
// environment, so the Claude subscription token (CLAUDE_CODE_OAUTH_TOKEN) and the
// sandbox proxy settings (HTTPS_PROXY, NODE_EXTRA_CA_CERTS) reach the CLI and it
// authenticates against the user's subscription itself.
func connectClaudeCodeACP(cwd string, store acpagent.ThreadStore) (*acpagent.Agent, error) {
	cmd := exec.Command(claudeCodeACPCommand)
	// Surface claude-code-acp's own startup diagnostics in the agent-api journal.
	cmd.Stderr = os.Stderr
	transport := &gosdkmcp.CommandTransport{Command: cmd}

	type connectResult struct {
		agent *acpagent.Agent
		err   error
	}
	done := make(chan connectResult, 1)
	go func() {
		agent, err := acpagent.Connect(context.Background(), transport, cwd, store)
		done <- connectResult{agent: agent, err: err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			return nil, fmt.Errorf("connect %s: %w", claudeCodeACPCommand, r.err)
		}
		return r.agent, nil
	case <-time.After(acpConnectTimeout):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return nil, fmt.Errorf("connect %s: timed out after %s (handshake stalled)", claudeCodeACPCommand, acpConnectTimeout)
	}
}
