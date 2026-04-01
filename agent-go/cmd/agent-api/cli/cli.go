// Package cli implements the interactive terminal mode for agent-api.
//
// Call AddFlags before flag.Parse, then Run to drive the agent via a
// stdin readline loop.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/term"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/agentimpl"
	"github.com/obot-platform/discobot/agent-go/internal/config"
	"github.com/obot-platform/discobot/agent-go/internal/credentials"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	"github.com/obot-platform/discobot/agent-go/thread"
	"github.com/obot-platform/discobot/agent-go/tools"
)

// Flags holds parsed CLI flag values for terminal mode.
type Flags struct {
	model     *string
	reasoning *bool
	plan      *bool
	newThread *bool
	resume    *string
	maxTurns  *int
	subagent  *string
}

// AddFlags registers terminal-mode flags with the default flag set and
// returns a Flags whose fields are populated after flag.Parse() is called.
// Must be called before flag.Parse().
func AddFlags() *Flags {
	model := new(string)
	flag.StringVar(model, "model", "", "Model to use, e.g. anthropic/claude-opus-4-6 (overrides MODEL env var)")
	flag.StringVar(model, "m", "", "Alias for --model")

	plan := new(bool)
	flag.BoolVar(plan, "plan", false, "Start in plan mode")
	flag.BoolVar(plan, "p", false, "Alias for --plan")

	newThread := new(bool)
	flag.BoolVar(newThread, "new-thread", false, "Start with a fresh thread ID (default behavior; retained for compatibility)")
	flag.BoolVar(newThread, "n", false, "Alias for --new-thread")

	resume := new(string)
	flag.StringVar(resume, "resume", "", "Resume an existing thread by ID")
	flag.StringVar(resume, "r", "", "Alias for --resume")

	return &Flags{
		model:     model,
		reasoning: flag.Bool("reasoning", true, "Enable extended thinking / reasoning (default on; use --reasoning=false to disable)"),
		plan:      plan,
		newThread: newThread,
		resume:    resume,
		maxTurns:  flag.Int("max-turns", 0, "Maximum LLM calls per turn (0 = unlimited)"),
		subagent:  flag.String("subagent", "", "Subagent config name from .claude/agents/*.md"),
	}
}

// Run runs the agent in interactive terminal mode.
//
// It builds the same infrastructure stack as the HTTP server but drives the
// agent via a stdin readline loop. Credentials are read from OS environment
// variables (e.g. ANTHROPIC_API_KEY) — no X-Discobot-Credentials header is
// needed in terminal mode.
func Run(cfg *config.Config, flags *Flags) {
	// Enable bracketed paste mode for the session.
	// The line readers use this to handle pasted blocks safely (including
	// multiline content) and provide compact paste summaries.
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, "\033[?2004h")
		defer fmt.Fprint(os.Stderr, "\033[?2004l") // restore on exit
	}

	// rootCtx lives for the entire program lifetime and is cancelled only on
	// SIGTERM or an explicit exit request (idle Ctrl+C race window).
	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	// turnCancel holds the cancel func for the currently running agent turn.
	// It is nil when the program is idle at the prompt (inside readLine).
	// Guarded by turnMu.
	var (
		turnMu     sync.Mutex
		turnCancel context.CancelFunc
	)

	// startTurn runs fn with a per-turn child context.  It registers the
	// cancel func so the SIGINT handler can cancel just the active turn
	// without exiting the program.
	startTurn := func(fn func(context.Context, context.CancelFunc)) {
		ctx, cancel := context.WithCancel(rootCtx)
		turnMu.Lock()
		turnCancel = cancel
		turnMu.Unlock()
		fn(ctx, cancel)
		turnMu.Lock()
		turnCancel = nil
		turnMu.Unlock()
		cancel()
	}

	// Signal handling:
	//   SIGTERM → cancel rootCtx (clean shutdown).
	//   SIGINT  → cancel the current turn if one is running; otherwise the
	//             program is idle at the prompt where readLine intercepts
	//             Ctrl+C as byte 0x03 (ISIG is off in raw mode), so a real
	//             SIGINT here is a race-window edge case — treat as exit.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		defer signal.Stop(sigCh)
		for {
			select {
			case sig, ok := <-sigCh:
				if !ok {
					return
				}
				if sig == syscall.SIGTERM {
					rootCancel()
					return
				}
				// SIGINT
				turnMu.Lock()
				cancel := turnCancel
				turnMu.Unlock()
				if cancel != nil {
					fmt.Fprintln(os.Stderr, "\n^C")
					cancel()
				} else {
					// Idle between readLine exits — exit the program.
					rootCancel()
					return
				}
			case <-rootCtx.Done():
				return
			}
		}
	}()

	// ── OAuth callback server ─────────────────────────────────────────────────
	// Start before building the agent stack so we have the redirect base URL
	// for MCP OAuth configuration. The server handles browser redirects for
	// MCP servers that require OAuth authorization.
	oauthBase, oauthSrv := startOAuthServer()
	if oauthSrv != nil {
		go func() {
			<-rootCtx.Done()
			_ = oauthSrv.Close()
		}()
	}

	// ── Agent stack ───────────────────────────────────────────────────────────
	// The credential manager starts empty; the provider registry falls back to
	// OS environment variables (ANTHROPIC_API_KEY, OPENAI_API_KEY, etc.) when
	// the manager has no credentials for a provider.
	credMgr := credentials.NewManager()
	reg := providers.NewProviderRegistry(credMgr)
	store := thread.NewStore(cfg.ThreadsDir)
	exec := tools.New(cfg.AgentCwd, cfg.DataDir, "")
	exec.SetThreadsDir(cfg.ThreadsDir)
	exec.SetBashEnvAllowlist(cfg.BashEnvAllowlist)

	mcpCfg := agentimpl.NewMCPConfig(
		oauthBase,
		cfg.SessionID,
		cfg.DiscobotServerURL,
		cfg.DiscobotProjectID,
	)
	a := agentimpl.NewDefaultAgent(store, reg, exec, cfg.AgentCwd, mcpCfg)

	// Wire the OAuth callback handler now that we have the agent reference.
	if oauthSrv != nil {
		wireOAuthCallbacks(oauthSrv, a)
	}

	// ── Startup recovery ──────────────────────────────────────────────────────
	threadID := selectInitialThreadID(store, cfg, *flags.newThread, *flags.resume)

	// Load persisted command history from .discobot/history (sibling of ThreadsDir).
	hist := loadCmdHistory(filepath.Join(filepath.Dir(cfg.ThreadsDir), "history"))

	// ── Resolve prompt defaults from flags ───────────────────────────────────
	// Flag values take precedence over env-var config defaults.
	model := cfg.Model
	if *flags.model != "" {
		model = *flags.model
	}
	reasoning := ""
	if *flags.reasoning {
		reasoning = "enabled"
	}
	planMode := getThreadPlanMode(store, threadID)
	if *flags.plan {
		planMode = true
		if threadExists(cfg.ThreadsDir, threadID) {
			saveThreadPlanMode(store, threadID, true)
		}
	}

	// ── Main input loop ───────────────────────────────────────────────────────
	showResume, showHistory := startupCommandHints(store, cfg, threadID)
	fmt.Fprintln(os.Stderr, startupMessage(showResume, showHistory))
	pendingFresh := map[string]bool{}

	// Handle any pending AskUserQuestion left from a previous session.
	if pending, _ := a.PendingQuestion(threadID); pending != nil {
		fmt.Fprintln(os.Stderr, "Resuming pending approval from previous session...")
		startTurn(func(ctx context.Context, cancel context.CancelFunc) {
			if handlePendingQuestion(ctx, a, threadID, pending) {
				runTurnLoop(ctx, cancel, a, threadID, agent.PromptRequest{Mode: planModeStr(planMode)}, true, func(enabled bool) {
					planMode = enabled
					saveThreadPlanMode(store, threadID, enabled)
				})
				planMode = getThreadPlanMode(store, threadID)
			}
		})
	}

	recoverIfInterrupted := func(ctx context.Context, cancel context.CancelFunc) {
		interrupted, err := a.HasInterruptedTurn(threadID)
		if err != nil || !interrupted {
			return
		}
		fmt.Fprintln(os.Stderr, "Recovering interrupted turn...")
		runTurnLoop(ctx, cancel, a, threadID, agent.PromptRequest{Mode: planModeStr(planMode)}, true, func(enabled bool) {
			planMode = enabled
			saveThreadPlanMode(store, threadID, enabled)
		})
		planMode = getThreadPlanMode(store, threadID)
	}
	consumeFreshContext := func(threadID string) bool {
		if !pendingFresh[threadID] {
			return false
		}
		delete(pendingFresh, threadID)
		return true
	}

	// ── Background MCP OAuth watcher ─────────────────────────────────────────
	go watchMCPOAuth(rootCtx, a)

	for {
		prompt := formatPrompt(model, planMode)
		line, err := readLineWithOptions(prompt, hist, commandCompletionOptions(a, cfg.AgentCwd))
		if err == io.EOF || err == errInterrupt {
			break // Ctrl+D or Ctrl+C at idle prompt → exit
		}
		if err != nil {
			break
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			break
		}
		if line == "/multiline" {
			hist.push(line)
			startTurn(func(ctx context.Context, cancel context.CancelFunc) {
				recoverIfInterrupted(ctx, cancel)
				parts, err := readMultilineInput("... ", "/end", cfg.AgentCwd)
				if err == errInterrupt {
					fmt.Fprintln(os.Stderr, "Multiline input cancelled.")
					return
				}
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error reading multiline input: %v\n", err)
					return
				}
				if len(parts) == 0 {
					fmt.Fprintln(os.Stderr, "No multiline input captured.")
					return
				}

				threadExistedBeforePrompt := threadExists(cfg.ThreadsDir, threadID)
				req := agent.PromptRequest{
					Model:        model,
					Reasoning:    reasoning,
					Mode:         planModeStr(planMode),
					MaxTurns:     *flags.maxTurns,
					SubagentType: *flags.subagent,
					FreshContext: consumeFreshContext(threadID),
					UserParts:    parts,
				}
				runTurnLoop(ctx, cancel, a, threadID, req, false, func(enabled bool) {
					planMode = enabled
					saveThreadPlanMode(store, threadID, enabled)
				})
				if !threadExistedBeforePrompt && planMode && threadExists(cfg.ThreadsDir, threadID) {
					saveThreadPlanMode(store, threadID, true)
				}
			})
			if rootCtx.Err() != nil {
				break
			}
			continue
		}

		hist.push(line)

		startTurn(func(ctx context.Context, cancel context.CancelFunc) {
			// Handle slash commands.
			if strings.HasPrefix(line, "/") {
				if newID, handled := handleSlashCommand(ctx, line, a, store, cfg, threadID, reg, &model, &planMode, pendingFresh); handled {
					if newID != threadID {
						threadID = newID
						planMode = getThreadPlanMode(store, threadID)
						fmt.Fprintf(os.Stderr, "Switched to thread %s\n", threadID)
						printThreadHistory(store, threadID)
						if pending, _ := a.PendingQuestion(threadID); pending != nil {
							fmt.Fprintln(os.Stderr, "Resuming pending approval...")
							if handlePendingQuestion(ctx, a, threadID, pending) {
								runTurnLoop(ctx, cancel, a, threadID, agent.PromptRequest{Mode: planModeStr(planMode)}, true, func(enabled bool) {
									planMode = enabled
									saveThreadPlanMode(store, threadID, enabled)
								})
								planMode = getThreadPlanMode(store, threadID)
							}
						}
					}
					return
				}
			}

			threadExistedBeforePrompt := threadExists(cfg.ThreadsDir, threadID)
			recoverIfInterrupted(ctx, cancel)
			req := agent.PromptRequest{
				Model:        model,
				Reasoning:    reasoning,
				Mode:         planModeStr(planMode),
				MaxTurns:     *flags.maxTurns,
				SubagentType: *flags.subagent,
				FreshContext: consumeFreshContext(threadID),
				UserParts:    []message.UIPart{message.UITextPart{Text: line}},
			}
			runTurnLoop(ctx, cancel, a, threadID, req, false, func(enabled bool) {
				planMode = enabled
				saveThreadPlanMode(store, threadID, enabled)
			})
			if !threadExistedBeforePrompt && planMode && threadExists(cfg.ThreadsDir, threadID) {
				saveThreadPlanMode(store, threadID, true)
			}
		})

		if rootCtx.Err() != nil {
			break // SIGTERM or exit requested
		}
	}

	a.Close()
	fmt.Fprintln(os.Stderr, "\nGoodbye.")
	if threadExists(cfg.ThreadsDir, threadID) {
		if cmd := resumeThreadCommand(threadID, os.Args[0]); cmd != "" {
			fmt.Fprintf(os.Stderr, "Resume this thread with:\n  %s\n", cmd)
		}
	}
}
