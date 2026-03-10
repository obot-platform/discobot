// Package cli implements the interactive terminal mode for agent-api.
//
// Call AddFlags before flag.Parse, then Run to drive the agent via a
// stdin readline loop.
package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/term"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/agentimpl"
	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/internal/config"
	"github.com/obot-platform/discobot/agent-go/internal/credentials"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	"github.com/obot-platform/discobot/agent-go/sessionconfig"
	"github.com/obot-platform/discobot/agent-go/thread"
	"github.com/obot-platform/discobot/agent-go/tools"
)

// noColor is true when the NO_COLOR environment variable is set (any value),
// per the convention at https://no-color.org/. When true, ANSI cursor-control
// and spinner output are suppressed.
var noColor = func() bool {
	_, set := os.LookupEnv("NO_COLOR")
	return set
}()

func promptColorsEnabled() bool {
	return !noColor && term.IsTerminal(int(os.Stderr.Fd()))
}

func formatPrompt(model string, planMode bool) string {
	planTag := ""
	if planMode {
		planTag = "[plan]"
	}

	if model == "" {
		if planTag != "" {
			if promptColorsEnabled() {
				return "\n\033[36m" + planTag + "\033[0m \033[1;36m>\033[0m "
			}
			return "\n" + planTag + " > "
		}
		if promptColorsEnabled() {
			return "\n\033[1;36m>\033[0m "
		}
		return "\n> "
	}

	if promptColorsEnabled() {
		return "\n\033[36m[" + model + "]" + planTag + "\033[0m \033[1;36m>\033[0m "
	}
	return "\n[" + model + "]" + planTag + " > "
}

func formatPromptHint() string {
	if promptColorsEnabled() {
		return "\033[1;36m>\033[0m "
	}
	return "> "
}

func startupMessage(showResume, showHistory bool) string {
	msg := "Discobot agent ready. Type your message"
	var commands []string
	if showResume {
		commands = append(commands, "/resume to switch threads")
	}
	if showHistory {
		commands = append(commands, "/history to view thread messages")
	}
	if len(commands) > 0 {
		msg += ", " + strings.Join(commands, ", ")
	}
	msg += ", or 'exit' to quit."
	return msg
}

// Flags holds parsed CLI flag values for terminal mode.
type Flags struct {
	model     *string
	reasoning *bool
	plan      *bool
	newThread *bool
	maxTurns  *int
	subagent  *string
}

// AddFlags registers terminal-mode flags with the default flag set and
// returns a Flags whose fields are populated after flag.Parse() is called.
// Must be called before flag.Parse().
func AddFlags() *Flags {
	newThread := new(bool)
	flag.BoolVar(newThread, "new-thread", false, "Start a new thread on launch instead of resuming")
	flag.BoolVar(newThread, "n", false, "Alias for --new-thread")

	return &Flags{
		model:     flag.String("model", "", "Model to use, e.g. anthropic/claude-opus-4-6 (overrides MODEL env var)"),
		reasoning: flag.Bool("reasoning", true, "Enable extended thinking / reasoning (default on; use --reasoning=false to disable)"),
		plan:      flag.Bool("plan", false, "Start in plan mode"),
		newThread: newThread,
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
	startTurn := func(fn func(context.Context)) {
		ctx, cancel := context.WithCancel(rootCtx)
		turnMu.Lock()
		turnCancel = cancel
		turnMu.Unlock()
		fn(ctx)
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
	threadID := selectInitialThreadID(store, cfg, *flags.newThread)

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
		saveThreadPlanMode(store, threadID, true)
	}

	// ── Main input loop ───────────────────────────────────────────────────────
	showResume, showHistory := startupCommandHints(store, cfg, threadID)
	fmt.Fprintln(os.Stderr, startupMessage(showResume, showHistory))

	// Resume any turn interrupted by a previous crash.
	interrupted, _ := a.InterruptedThreads()
	for _, id := range interrupted {
		if id == threadID {
			fmt.Fprintln(os.Stderr, "Resuming interrupted turn from previous session...")
			startTurn(func(ctx context.Context) {
				runTurnLoop(ctx, a, threadID, agent.PromptRequest{Mode: planModeStr(planMode)}, func(enabled bool) {
					planMode = enabled
					saveThreadPlanMode(store, threadID, enabled)
				})
				planMode = getThreadPlanMode(store, threadID)
			})
			break
		}
	}

	// Handle any pending AskUserQuestion left from a previous session.
	if pending, _ := a.PendingQuestion(threadID); pending != nil {
		fmt.Fprintln(os.Stderr, "Resuming pending approval from previous session...")
		startTurn(func(ctx context.Context) {
			if handlePendingQuestion(ctx, a, threadID, pending) {
				runTurnLoop(ctx, a, threadID, agent.PromptRequest{Mode: planModeStr(planMode)}, func(enabled bool) {
					planMode = enabled
					saveThreadPlanMode(store, threadID, enabled)
				})
				planMode = getThreadPlanMode(store, threadID)
			}
		})
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
			startTurn(func(ctx context.Context) {
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

				req := agent.PromptRequest{
					Model:        model,
					Reasoning:    reasoning,
					Mode:         planModeStr(planMode),
					MaxTurns:     *flags.maxTurns,
					SubagentType: *flags.subagent,
					UserParts:    parts,
				}
				runTurnLoop(ctx, a, threadID, req, func(enabled bool) {
					planMode = enabled
					saveThreadPlanMode(store, threadID, enabled)
				})
			})
			if rootCtx.Err() != nil {
				break
			}
			continue
		}

		hist.push(line)

		startTurn(func(ctx context.Context) {
			// Handle slash commands.
			if strings.HasPrefix(line, "/") {
				if newID, handled := handleSlashCommand(ctx, line, a, store, cfg, threadID, reg, &model, &planMode); handled {
					if newID != threadID {
						threadID = newID
						planMode = getThreadPlanMode(store, threadID)
						fmt.Fprintf(os.Stderr, "Switched to thread %s\n", threadID)
						printThreadHistory(store, threadID)
						// Resume any interrupted turn or pending question in the new thread.
						interrupted, _ := a.InterruptedThreads()
						for _, id := range interrupted {
							if id == threadID {
								fmt.Fprintln(os.Stderr, "Resuming interrupted turn...")
								runTurnLoop(ctx, a, threadID, agent.PromptRequest{Mode: planModeStr(planMode)}, func(enabled bool) {
									planMode = enabled
									saveThreadPlanMode(store, threadID, enabled)
								})
								planMode = getThreadPlanMode(store, threadID)
								break
							}
						}
						if pending, _ := a.PendingQuestion(threadID); pending != nil {
							fmt.Fprintln(os.Stderr, "Resuming pending approval...")
							if handlePendingQuestion(ctx, a, threadID, pending) {
								runTurnLoop(ctx, a, threadID, agent.PromptRequest{Mode: planModeStr(planMode)}, func(enabled bool) {
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

			req := agent.PromptRequest{
				Model:        model,
				Reasoning:    reasoning,
				Mode:         planModeStr(planMode),
				MaxTurns:     *flags.maxTurns,
				SubagentType: *flags.subagent,
				UserParts:    []message.Part{message.TextPart{Text: line}},
			}
			runTurnLoop(ctx, a, threadID, req, func(enabled bool) {
				planMode = enabled
				saveThreadPlanMode(store, threadID, enabled)
			})
		})

		if rootCtx.Err() != nil {
			break // SIGTERM or exit requested
		}
	}

	a.Close()
	fmt.Fprintln(os.Stderr, "\nGoodbye.")
}

// readMultilineInput captures input lines until endMarker is entered as a
// standalone line. It returns a mixed part list where pasted image file paths
// and raw image bytes are converted to ImagePart, and all remaining content is
// accumulated into TextPart blocks.
func readMultilineInput(prompt, endMarker, cwd string) ([]message.Part, error) {
	var parts []message.Part
	var text strings.Builder

	flushText := func() {
		if text.Len() == 0 {
			return
		}
		parts = append(parts, message.TextPart{Text: text.String()})
		text.Reset()
	}

	for {
		line, err := readLine(prompt, nil)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(line) == endMarker {
			flushText()
			return parts, nil
		}

		if part, ok, err := multilinePartFromInput([]byte(line), cwd); err != nil {
			return nil, err
		} else if ok {
			flushText()
			parts = append(parts, part)
			continue
		}

		if text.Len() > 0 {
			text.WriteByte('\n')
		}
		text.WriteString(line)
	}
}

func multilinePartFromInput(input []byte, cwd string) (message.Part, bool, error) {
	if part, ok, err := imagePartFromPathInput(input, cwd); err != nil {
		return nil, false, err
	} else if ok {
		return part, true, nil
	}
	if part, ok := imagePartFromRawBytes(input); ok {
		return part, true, nil
	}
	return nil, false, nil
}

func imagePartFromPathInput(input []byte, cwd string) (message.ImagePart, bool, error) {
	if !utf8.Valid(input) {
		return message.ImagePart{}, false, nil
	}
	text := strings.TrimSpace(string(input))
	if text == "" {
		return message.ImagePart{}, false, nil
	}
	text = strings.Trim(text, "\"'")
	if strings.ContainsAny(text, "\r\n\x00") {
		return message.ImagePart{}, false, nil
	}
	path := strings.TrimPrefix(text, "file://")
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	path = filepath.Clean(path)

	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return message.ImagePart{}, false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return message.ImagePart{}, false, err
	}
	mediaType := http.DetectContentType(data)
	if !strings.HasPrefix(mediaType, "image/") {
		extType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
		if !strings.HasPrefix(extType, "image/") {
			return message.ImagePart{}, false, nil
		}
		mediaType = extType
	}

	return message.ImagePart{Image: base64.StdEncoding.EncodeToString(data), MediaType: mediaType}, true, nil
}

func imagePartFromRawBytes(input []byte) (message.ImagePart, bool) {
	trimmed := bytes.Trim(input, "\r\n")
	if len(trimmed) == 0 {
		return message.ImagePart{}, false
	}
	mediaType := http.DetectContentType(trimmed)
	if !strings.HasPrefix(mediaType, "image/") {
		return message.ImagePart{}, false
	}
	return message.ImagePart{Image: base64.StdEncoding.EncodeToString(trimmed), MediaType: mediaType}, true
}

// handleSlashCommand dispatches a slash command entered in the main input loop.
// Returns the (possibly changed) threadID and true when handled locally by the
// CLI, or current threadID and false when the command should be passed through
// to the agent.
func handleSlashCommand(ctx context.Context, line string, a *agentimpl.DefaultAgent, store *thread.Store, cfg *config.Config, currentThreadID string, reg *providers.ProviderRegistry, currentModel *string, currentPlanMode *bool) (string, bool) {
	parts := strings.Fields(line)
	cmd := parts[0]
	switch cmd {
	case "/resume":
		return handleResumeCommand(ctx, a, store, cfg, currentThreadID), true
	case "/clear":
		newThreadID := "thread-" + agent.GenerateID()
		fmt.Fprintf(os.Stderr, "Started new thread %s\n", newThreadID)
		return newThreadID, true
	case "/plan":
		enabled := !*currentPlanMode
		*currentPlanMode = enabled
		saveThreadPlanMode(store, currentThreadID, enabled)
		if enabled {
			fmt.Fprintln(os.Stderr, "Plan mode enabled.")
		} else {
			fmt.Fprintln(os.Stderr, "Plan mode disabled.")
		}
		return currentThreadID, true
	case "/models":
		handleModelsCommand(ctx, reg, currentModel)
		return currentThreadID, true
	case "/history":
		if !printThreadHistory(store, currentThreadID) {
			fmt.Fprintln(os.Stderr, "No printable messages in current thread.")
		}
		return currentThreadID, true
	default:
		if isAgentSlashCommand(a, cfg.AgentCwd, cmd) {
			return currentThreadID, false
		}
		fmt.Fprintf(os.Stderr, "Unknown command %q. Available commands: %s\n", cmd, availableCommandsList(a, cfg.AgentCwd))
		return currentThreadID, true
	}
}

// cliBuiltinCommands are slash commands handled directly by the CLI, not by the agent.
var cliBuiltinCommands = []string{"resume", "clear", "plan", "models", "history", "multiline"}

// availableCommands returns a sorted list of slash-prefixed command names
// available to the user (CLI built-ins + agent commands).
func availableCommands(a *agentimpl.DefaultAgent, cwd string) []string {
	seen := make(map[string]struct{})
	var names []string

	add := func(name string) {
		name = "/" + strings.TrimPrefix(name, "/")
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}

	for _, name := range cliBuiltinCommands {
		add(name)
	}
	if a != nil {
		if cmds, err := a.ListCommands(); err == nil {
			for _, c := range cmds {
				add(c.Name)
			}
		}
	} else if agentCmds, err := agentSlashCommands(cwd); err == nil {
		for name := range agentCmds {
			add(name)
		}
	}

	sort.Strings(names)
	return names
}

// availableCommandsList returns a sorted, slash-prefixed, comma-separated list
// of all commands available to the user (CLI built-ins + agent commands).
func availableCommandsList(a *agentimpl.DefaultAgent, cwd string) string {
	return strings.Join(availableCommands(a, cwd), ", ")
}

func commandCompletionOptions(a *agentimpl.DefaultAgent, cwd string) *readLineOptions {
	cmds := availableCommands(a, cwd)
	if len(cmds) == 0 {
		return nil
	}
	return &readLineOptions{slashCommands: cmds}
}

func isAgentSlashCommand(a *agentimpl.DefaultAgent, cwd, cmd string) bool {
	if a != nil {
		cmds, err := a.ListCommands()
		if err != nil {
			return false
		}
		for _, c := range cmds {
			if "/"+strings.TrimPrefix(c.Name, "/") == cmd {
				return true
			}
		}
		return false
	}
	commands, err := agentSlashCommands(cwd)
	if err != nil {
		return false
	}
	_, ok := commands[cmd]
	return ok
}

func agentSlashCommands(cwd string) (map[string]struct{}, error) {
	sessionCfg, err := sessionconfig.Load(cwd)
	if err != nil {
		return nil, err
	}
	commands := make(map[string]struct{}, len(sessionCfg.Skills))
	for _, skill := range sessionCfg.Skills {
		name := strings.TrimSpace(skill.Name)
		if name == "" {
			continue
		}
		if !strings.HasPrefix(name, "/") {
			name = "/" + name
		}
		commands[name] = struct{}{}
	}
	return commands, nil
}

// handleModelsCommand lists available models from all configured providers and
// lets the user select one to use for subsequent turns in this session.
func handleModelsCommand(ctx context.Context, reg *providers.ProviderRegistry, currentModel *string) {
	fmt.Fprint(os.Stderr, "Fetching available models...")
	spin := newSpinner()
	spin.Start()
	models, err := reg.ListModels(ctx)
	spin.Stop()
	if noColor {
		fmt.Fprintln(os.Stderr) // newline after "Fetching..." text
	} else {
		fmt.Fprint(os.Stderr, "\r\033[2K") // erase the "Fetching..." line
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching models: %v\n", err)
		return
	}
	if len(models) == 0 {
		fmt.Fprintln(os.Stderr, "No models available. Check your provider credentials.")
		return
	}

	// Build a list of provider entries (use provider default) followed by individual models.
	// Each entry has a selection ID (what gets stored in currentModel) and a display string.
	type entry struct {
		selectionID string // stored as the model ref
		display     string
	}
	var entries []entry

	// Provider defaults first.
	providerIDs := reg.IDs()
	for _, id := range providerIDs {
		p, err := reg.Get(id)
		if err != nil {
			continue
		}
		ref := p.DefaultModels()[providers.ModelTaskChat]
		if ref.ModelID == "" {
			continue
		}
		current := ""
		if id == *currentModel {
			current = " (current)"
		}
		entries = append(entries, entry{
			selectionID: id,
			display:     fmt.Sprintf("%s (default: %s)%s", id, ref.ModelID, current),
		})
	}

	// Individual models.
	for _, m := range models {
		current := ""
		if m.ID == *currentModel {
			current = " (current)"
		}
		display := m.ID
		if m.DisplayName != "" {
			display += " — " + m.DisplayName
		}
		entries = append(entries, entry{selectionID: m.ID, display: display + current})
	}

	fmt.Fprintln(os.Stderr, "\nAvailable models:")
	for i, e := range entries {
		fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, e.display)
	}

	for {
		input, err := readLine("\nSelect model (number, provider, or provider/model, Enter to cancel): ", nil)
		if err != nil {
			return
		}
		input = strings.TrimSpace(input)
		if input == "" {
			fmt.Fprintln(os.Stderr, "Cancelled.")
			return
		}

		// Try as a 1-based index first.
		if n, err := strconv.Atoi(input); err == nil {
			if n < 1 || n > len(entries) {
				fmt.Fprintf(os.Stderr, "Please enter a number between 1 and %d.\n", len(entries))
				continue
			}
			*currentModel = entries[n-1].selectionID
			fmt.Fprintf(os.Stderr, "Model set to %s\n", *currentModel)
			return
		}

		// Accept a bare provider ID or a full "provider/model" ref.
		if _, err := reg.ResolveModel(input, providers.ModelTaskChat); err == nil {
			*currentModel = input
			fmt.Fprintf(os.Stderr, "Model set to %s\n", *currentModel)
			return
		}

		fmt.Fprintln(os.Stderr, "Enter a number from the list, a provider name, or a provider/model reference.")
	}
}

// threadSummary holds display metadata for a single thread.
type threadSummary struct {
	id      string
	modTime time.Time
	preview string // last user message text, truncated
	pending bool   // has a pending AskUserQuestion
}

func normalizeCWD(path string) string {
	if path == "" {
		return ""
	}
	path = filepath.Clean(path)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return path
}

func startupCommandHints(store *thread.Store, cfg *config.Config, threadID string) (showResume bool, showHistory bool) {
	if _, err := os.Stat(filepath.Join(cfg.ThreadsDir, threadID)); err == nil {
		showHistory = true
	}

	threadIDs, err := store.ListThreads()
	if err != nil || len(threadIDs) == 0 {
		return false, showHistory
	}

	targetCWD := normalizeCWD(cfg.AgentCwd)
	matchingCWD := 0
	currentMatchesCWD := false
	for _, id := range threadIDs {
		threadCfg, err := store.LoadConfig(id)
		if err != nil || strings.TrimSpace(threadCfg.CWD) == "" {
			continue
		}
		if normalizeCWD(threadCfg.CWD) != targetCWD {
			continue
		}
		matchingCWD++
		if id == threadID {
			currentMatchesCWD = true
		}
	}

	if currentMatchesCWD {
		showResume = matchingCWD > 1
	} else {
		showResume = matchingCWD > 0
	}
	return showResume, showHistory
}

func selectInitialThreadID(store *thread.Store, cfg *config.Config, forceNew bool) string {
	if forceNew {
		return "thread-" + agent.GenerateID()
	}

	// Respect explicit SESSION_ID so advanced workflows remain deterministic.
	if cfg.SessionID != "" && cfg.SessionID != "default" {
		return cfg.SessionID
	}

	threadIDs, err := store.ListThreads()
	if err != nil || len(threadIDs) == 0 {
		if cfg.SessionID != "" {
			return cfg.SessionID
		}
		return "thread-" + agent.GenerateID()
	}

	cwd := normalizeCWD(cfg.AgentCwd)
	latestID := ""
	latestModTime := time.Time{}
	for _, id := range threadIDs {
		threadCfg, err := store.LoadConfig(id)
		if err != nil || threadCfg.CWD == "" {
			continue
		}
		if normalizeCWD(threadCfg.CWD) != cwd {
			continue
		}
		modTime := time.Time{}
		if fi, err := os.Stat(filepath.Join(cfg.ThreadsDir, id)); err == nil {
			modTime = fi.ModTime()
		}
		if latestID == "" || modTime.After(latestModTime) {
			latestID = id
			latestModTime = modTime
		}
	}
	if latestID != "" {
		return latestID
	}

	return "thread-" + agent.GenerateID()
}

// handleResumeCommand lists available threads and lets the user select one.
// Returns the selected thread ID, or currentThreadID if the user cancels.
func handleResumeCommand(_ context.Context, a *agentimpl.DefaultAgent, store *thread.Store, cfg *config.Config, currentThreadID string) string {
	threadIDs, err := a.ListThreads()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing threads: %v\n", err)
		return currentThreadID
	}
	if len(threadIDs) == 0 {
		fmt.Fprintln(os.Stderr, "No threads found.")
		return currentThreadID
	}

	targetCWD := normalizeCWD(cfg.AgentCwd)
	summaries := make([]threadSummary, 0, len(threadIDs))
	otherDirCounts := map[string]int{}
	otherUnknown := 0

	for _, id := range threadIDs {
		threadCfg, cfgErr := store.LoadConfig(id)
		threadCWD := normalizeCWD(threadCfg.CWD)
		if cfgErr == nil && threadCWD != "" && threadCWD != targetCWD {
			otherDirCounts[threadCWD]++
			continue
		}
		if cfgErr == nil && threadCWD == "" {
			otherUnknown++
		}

		s := threadSummary{id: id}

		// Modification time: use the thread directory's mtime as a proxy.
		if fi, err := os.Stat(filepath.Join(cfg.ThreadsDir, id)); err == nil {
			s.modTime = fi.ModTime()
		}

		// Preview: walk from leaf looking for the most recent user message.
		if leafID, err := store.FindLeaf(id); err == nil && leafID != "" {
			s.preview = lastUserPreview(store, id, leafID)
		}

		// Pending question check.
		if turnState, _ := store.LoadTurnState(id); turnState != nil && turnState.Phase == thread.PhaseWaitingForAnswer {
			s.pending = true
		}

		summaries = append(summaries, s)
	}

	// Sort newest-first.
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].modTime.After(summaries[j].modTime)
	})

	if len(summaries) == 0 {
		fmt.Fprintf(os.Stderr, "No threads found for current directory: %s\n", targetCWD)
	} else {
		fmt.Fprintf(os.Stderr, "\nAvailable threads for %s:\n", targetCWD)
		for i, s := range summaries {
			marker := ""
			if s.id == currentThreadID {
				marker = " (current)"
			}
			if s.pending {
				marker += " [pending approval]"
			}
			age := formatAge(time.Since(s.modTime))
			fmt.Fprintf(os.Stderr, "  %d. %s  %s%s\n", i+1, s.id, age, marker)
			if s.preview != "" {
				fmt.Fprintf(os.Stderr, "     \"%s\"\n", s.preview)
			}
		}
	}

	if len(otherDirCounts) > 0 {
		fmt.Fprintln(os.Stderr, "")
		totalOther := 0
		for _, n := range otherDirCounts {
			totalOther += n
		}
		fmt.Fprintf(os.Stderr, "%d thread(s) belong to other directories.\n", totalOther)
		dirs := make([]string, 0, len(otherDirCounts))
		for dir := range otherDirCounts {
			dirs = append(dirs, dir)
		}
		sort.Strings(dirs)
		for _, dir := range dirs {
			fmt.Fprintf(os.Stderr, "  - %s (%d)\n", dir, otherDirCounts[dir])
		}
		fmt.Fprintln(os.Stderr, "To resume those threads, cd into that directory and run /resume.")
	}
	if otherUnknown > 0 {
		fmt.Fprintf(os.Stderr, "\nIncluding %d legacy thread(s) with unknown cwd.\n", otherUnknown)
	}

	if len(summaries) == 0 {
		return currentThreadID
	}

	for {
		input, err := readLine("\nSelect thread (number, or Enter to cancel): ", nil)
		if err != nil {
			return currentThreadID
		}
		input = strings.TrimSpace(input)
		if input == "" {
			fmt.Fprintln(os.Stderr, "Cancelled.")
			return currentThreadID
		}
		n, err := strconv.Atoi(input)
		if err != nil || n < 1 || n > len(summaries) {
			fmt.Fprintf(os.Stderr, "Please enter a number between 1 and %d.\n", len(summaries))
			continue
		}
		return summaries[n-1].id
	}
}

// lastUserPreview walks the message chain from leafID upward (up to 20 hops)
// looking for the most recent human-typed user message, and returns its text
// preview. Auto-injected setup messages (system prompts, <system-reminder>
// blocks, skills reminders) are skipped.
func lastUserPreview(store *thread.Store, threadID, leafID string) string {
	currentID := leafID
	for i := 0; i < 20 && currentID != ""; i++ {
		msg, err := store.LoadMessage(threadID, currentID)
		if err != nil {
			break
		}
		if msg.Message.Role == "user" && !isInjectedMessageID(msg.ID) {
			if text := extractMessageText(msg.Message.Parts); text != "" && !isInjectedText(text) {
				return abbreviate(text, 80)
			}
		}
		currentID = msg.ParentID
	}
	return ""
}

// isInjectedMessageID reports whether a stored message ID belongs to an
// auto-injected setup message (system prompt, instructions, skills reminder).
func isInjectedMessageID(id string) bool {
	return strings.HasPrefix(id, "system-") ||
		strings.HasPrefix(id, "instructions-") ||
		strings.HasPrefix(id, "skills-")
}

// isInjectedText reports whether message text is auto-injected content
// (system reminders wrapped in XML tags) rather than human-typed input.
func isInjectedText(text string) bool {
	return strings.HasPrefix(text, "<system-reminder>") ||
		strings.HasPrefix(text, "<skills-reminder>")
}

// extractMessageText returns the first non-empty text from a list of message parts.
func extractMessageText(parts []message.Part) string {
	for _, p := range parts {
		if tp, ok := p.(message.TextPart); ok && tp.Text != "" {
			return tp.Text
		}
	}
	return ""
}

func extractAllText(parts []message.Part) string {
	var b strings.Builder
	for _, p := range parts {
		if tp, ok := p.(message.TextPart); ok {
			b.WriteString(tp.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

func printThreadHistory(store *thread.Store, threadID string) bool {
	leafID, err := store.FindLeaf(threadID)
	if err != nil || leafID == "" {
		return false
	}

	history, err := store.BuildHistoryWithIDs(threadID, leafID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading thread history: %v\n", err)
		return false
	}

	md := newMarkdownRenderer(os.Stdout, term.IsTerminal(int(os.Stdout.Fd())), !noColor)
	printed := false
	for _, entry := range history {
		if isInjectedMessageID(entry.ID) {
			continue
		}
		if entry.Message.Role != "user" && entry.Message.Role != "assistant" {
			continue
		}

		text := extractAllText(entry.Message.Parts)
		if text == "" {
			continue
		}

		label := "Assistant"
		if entry.Message.Role == "user" {
			label = "User"
		}
		fmt.Fprintf(os.Stdout, "\n[%s]\n", label)
		md.WriteText(text)
		md.Finish()
		fmt.Fprintln(os.Stdout)
		printed = true
	}

	if printed {
		fmt.Fprintln(os.Stdout)
	}
	return printed
}

// formatAge returns a human-readable "X ago" string for a duration.
func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

// runTurnLoop drives an agent turn to completion, looping to handle
// intermediate AskUserQuestion / ExitPlanMode approval requests.
//
// req is the initial PromptRequest. On each approval loop iteration,
// the agent is resumed with an empty PromptRequest (the DefaultAgent
// detects the waiting_for_answer phase and continues from disk state).
func runTurnLoop(ctx context.Context, a *agentimpl.DefaultAgent, threadID string, req agent.PromptRequest, onPlanModeChange func(bool)) {
	toolState := newToolRenderState()
	pendingPlanToolCalls := map[string]string{}
	activePlanMode := req.Mode == "plan"
	setPlanMode := func(enabled bool) {
		activePlanMode = enabled
		if onPlanModeChange != nil {
			onPlanModeChange(enabled)
		}
	}
	for {
		md := newMarkdownRenderer(os.Stdout, term.IsTerminal(int(os.Stdout.Fd())), !noColor)

		// Show a spinner while waiting for the first response chunk.
		spin := newSpinner()
		spin.Start()

		// Stream the turn, printing chunks as they arrive.
		for chunk, err := range a.Prompt(ctx, threadID, req) {
			if err != nil {
				md.Finish()
				spin.Stop()
				fmt.Fprintf(os.Stderr, "\nError: %v\n", err)
				return
			}
			if chunk != nil {
				switch c := chunk.(type) {
				case message.ModeChangeChunk:
					setPlanMode(strings.EqualFold(c.Data.Mode, "planning") || strings.EqualFold(c.Data.Mode, "plan"))
				case message.ToolInputAvailableChunk:
					if isPlanToolName(c.ToolName) {
						pendingPlanToolCalls[c.ToolCallID] = c.ToolName
					}
				case message.ToolOutputAvailableChunk:
					if toolName, ok := pendingPlanToolCalls[c.ToolCallID]; ok {
						switch toolName {
						case "EnterPlanMode":
							setPlanMode(true)
						case "ExitPlanMode":
							if isExitPlanApproved(extractOutputText(c.Output)) {
								setPlanMode(false)
							}
						}
						delete(pendingPlanToolCalls, c.ToolCallID)
					}
				case message.ToolOutputErrorChunk:
					delete(pendingPlanToolCalls, c.ToolCallID)
				case message.ToolOutputDeniedChunk:
					delete(pendingPlanToolCalls, c.ToolCallID)
				}

				switch chunk.(type) {
				case message.TextDeltaChunk,
					message.ReasoningStartChunk,
					message.ReasoningDeltaChunk,
					message.ReasoningEndChunk,
					message.ToolInputAvailableChunk,
					message.ToolOutputAvailableChunk,
					message.ToolOutputErrorChunk,
					message.ErrorChunk,
					message.AbortChunk:
					spin.Stop()
				}

				if _, isText := chunk.(message.TextDeltaChunk); !isText {
					md.FlushForBoundary()
				}

				renderChunk(chunk, md, toolState)
				// After tool output, restart the spinner: the model is about
				// to process the result and stream its next response.
				switch chunk.(type) {
				case message.ToolOutputAvailableChunk, message.ToolOutputErrorChunk:
					spin = newSpinner()
					spin.Start()
				}
			}
		}
		md.Finish()
		spin.Stop()

		if ctx.Err() != nil {
			return // cancelled by Ctrl+C
		}

		// Check whether the turn paused waiting for user approval.
		pending, err := a.PendingQuestion(threadID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nError checking for pending question: %v\n", err)
			return
		}
		if pending == nil {
			// Turn complete — print a trailing newline after streamed text.
			fmt.Println()
			return
		}

		// Handle the approval interactively and resume the turn.
		if !handlePendingQuestion(ctx, a, threadID, pending) {
			return
		}
		req = agent.PromptRequest{Mode: planModeStr(activePlanMode)} // resume: DefaultAgent detects waiting_for_answer
	}
}

// handlePendingQuestion presents a pending AskUserQuestion / ExitPlanMode
// approval to the user, collects answers, and submits them.
// Returns false if stdin was closed or an error occurred.
func handlePendingQuestion(ctx context.Context, a *agentimpl.DefaultAgent, threadID string, pending *agent.PendingQuestion) bool {
	var questions []api.AskUserQuestion
	if err := json.Unmarshal(pending.Questions, &questions); err != nil {
		fmt.Fprintf(os.Stderr, "\nError parsing questions: %v\n", err)
		return false
	}

	answers := collectAnswers(ctx, questions)
	if answers == nil {
		return false // EOF or cancellation
	}

	if err := a.SubmitAnswer(threadID, pending.ToolCallID, answers); err != nil {
		fmt.Fprintf(os.Stderr, "\nError submitting answer: %v\n", err)
		return false
	}
	return true
}

// collectAnswers presents each question to the user on stderr and reads
// answers from stdin. Returns nil if stdin closes or ctx is done.
func collectAnswers(ctx context.Context, questions []api.AskUserQuestion) map[string]string {
	answers := make(map[string]string)
	fmt.Fprintln(os.Stderr)

	for _, q := range questions {
		if ctx.Err() != nil {
			return nil
		}

		// Print any context notes (e.g. the plan file content) before the question.
		if q.Notes != "" {
			md := newMarkdownRenderer(os.Stderr, term.IsTerminal(int(os.Stderr.Fd())), !noColor)
			md.WriteText(strings.TrimRight(q.Notes, "\n"))
			md.Finish()
			fmt.Fprintln(os.Stderr)
		}

		fmt.Fprintf(os.Stderr, "%s\n", q.Question)

		if len(q.Options) > 0 {
			for i, opt := range q.Options {
				if opt.Description != "" {
					fmt.Fprintf(os.Stderr, "  %d. %s — %s\n", i+1, opt.Label, opt.Description)
				} else {
					fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, opt.Label)
				}
			}
			otherNum := len(q.Options) + 1
			fmt.Fprintf(os.Stderr, "  %d. Other — Enter a custom response\n", otherNum)

			for {
				input, err := readLine(fmt.Sprintf("Choice (1-%d or label): ", otherNum), nil)
				if err != nil {
					return nil
				}
				input = strings.TrimSpace(input)

				// Try as 1-based index.
				if n, err := strconv.Atoi(input); err == nil {
					if n >= 1 && n <= len(q.Options) {
						answers[q.Question] = q.Options[n-1].Label
						break
					}
					if n == otherNum {
						custom, err := readLine("Custom response: ", nil)
						if err != nil {
							return nil
						}
						answers[q.Question] = strings.TrimSpace(custom)
						break
					}
				}

				// Try as label (case-insensitive).
				matched := false
				for _, opt := range q.Options {
					if strings.EqualFold(input, opt.Label) {
						answers[q.Question] = opt.Label
						matched = true
						break
					}
				}
				if matched {
					break
				}

				fmt.Fprintf(os.Stderr, "Please enter a number (1-%d) or a matching label.\n", otherNum)
			}
		} else {
			// Free-text answer.
			input, err := readLine("Answer: ", nil)
			if err != nil {
				return nil
			}
			answers[q.Question] = strings.TrimSpace(input)
		}
	}

	return answers
}

// renderChunk prints a MessageChunk to stdout (text) or stderr (tool info).
// Text deltas stream directly to stdout so they can be piped; tool and
// lifecycle events go to stderr to keep them out of pipe output.
func renderChunk(chunk message.MessageChunk, md *markdownRenderer, tools *toolRenderState) {
	switch c := chunk.(type) {
	case message.TextDeltaChunk:
		if md != nil {
			md.WriteText(c.Delta)
		} else {
			fmt.Print(c.Delta)
		}

	case message.ReasoningStartChunk:
		if noColor {
			fmt.Fprint(os.Stderr, "\n[thinking]\n")
		} else {
			fmt.Fprint(os.Stderr, "\n\033[2m")
		}

	case message.ReasoningDeltaChunk:
		fmt.Fprint(os.Stderr, c.Delta)

	case message.ReasoningEndChunk:
		if noColor {
			fmt.Fprint(os.Stderr, "\n[/thinking]\n")
		} else {
			fmt.Fprint(os.Stderr, "\033[0m\n")
		}

	case message.ToolInputAvailableChunk:
		tools.rememberInput(c.ToolCallID, c.ToolName, c.Input)
		label := tools.labelFor(c.ToolCallID, c.ToolName)
		summary := toolInputSummary(c.ToolName, c.Input)
		if summary != "" {
			fmt.Fprintf(os.Stderr, "\n%s [%s] %s\n", styleToolStartArrow(), styleToolLabel(label), summary)
		} else {
			fmt.Fprintf(os.Stderr, "\n%s [%s]\n", styleToolStartArrow(), styleToolLabel(label))
		}

	case message.ToolOutputAvailableChunk:
		label := tools.labelFor(c.ToolCallID, "")
		text := extractOutputText(c.Output)
		detail := toolOutputDetail(tools.toolNameFor(c.ToolCallID), tools.inputFor(c.ToolCallID))
		renderToolTail(label, false, text, detail)

	case message.ToolOutputErrorChunk:
		label := tools.labelFor(c.ToolCallID, "")
		renderToolTail(label, true, c.ErrorText, "")

	case message.ToolApprovalRequestChunk:
		// The turn will pause after the iterator ends; no action needed here.

	case message.ErrorChunk:
		fmt.Fprintf(os.Stderr, "\n[error: %s]\n", c.ErrorText)

	case message.AbortChunk:
		if c.Reason != "" {
			fmt.Fprintf(os.Stderr, "\n[aborted: %s]\n", c.Reason)
		}
	}
}

type toolRenderState struct {
	labels    map[string]string
	toolNames map[string]string
	inputs    map[string]json.RawMessage
}

func newToolRenderState() *toolRenderState {
	return &toolRenderState{
		labels:    map[string]string{},
		toolNames: map[string]string{},
		inputs:    map[string]json.RawMessage{},
	}
}

func (s *toolRenderState) rememberInput(toolCallID, toolName string, input json.RawMessage) {
	if s == nil {
		return
	}
	if toolName != "" {
		s.toolNames[toolCallID] = toolName
	}
	if len(input) > 0 {
		cp := make(json.RawMessage, len(input))
		copy(cp, input)
		s.inputs[toolCallID] = cp
	}
}

func (s *toolRenderState) toolNameFor(toolCallID string) string {
	if s == nil {
		return ""
	}
	return s.toolNames[toolCallID]
}

func (s *toolRenderState) inputFor(toolCallID string) json.RawMessage {
	if s == nil {
		return nil
	}
	return s.inputs[toolCallID]
}

func (s *toolRenderState) labelFor(toolCallID, toolName string) string {
	if s == nil {
		return buildToolLabel(toolCallID, toolName)
	}
	if toolName != "" {
		s.toolNames[toolCallID] = toolName
		label := buildToolLabel(toolCallID, toolName)
		s.labels[toolCallID] = label
		return label
	}
	if label, ok := s.labels[toolCallID]; ok {
		return label
	}
	fallbackName := s.toolNames[toolCallID]
	label := buildToolLabel(toolCallID, fallbackName)
	s.labels[toolCallID] = label
	return label
}

func buildToolLabel(toolCallID, toolName string) string {
	if toolName == "" {
		toolName = "tool"
	}
	return fmt.Sprintf("%s(%s)", toolName, shortToolID(toolCallID))
}

func shortToolID(id string) string {
	if id == "" {
		return "unknown"
	}
	if len(id) <= 8 {
		return id
	}
	return id[len(id)-8:]
}

func styleToolStartArrow() string {
	if noColor {
		return "→"
	}
	return "\033[36m→\033[0m"
}

func styleToolOutputArrow(isError bool) string {
	if noColor {
		return "←"
	}
	if isError {
		return "\033[31m←\033[0m"
	}
	return "\033[32m←\033[0m"
}

func styleToolLabel(label string) string {
	if noColor {
		return label
	}
	return "\033[1m" + label + "\033[0m"
}

func styleToolDivider() string {
	if noColor {
		return "    ------------------------------"
	}
	return "\033[2m    ------------------------------\033[0m"
}

func renderToolTail(label string, isError bool, text, detail string) {
	lineCount := countLines(text)
	kind := "output"
	if isError {
		kind = "error"
	}
	fmt.Fprintf(os.Stderr, "%s [%s] %s: %d lines\n", styleToolOutputArrow(isError), styleToolLabel(label), kind, lineCount)

	detail = strings.TrimSpace(strings.ReplaceAll(detail, "\r\n", "\n"))
	if detail == "" {
		return
	}

	fmt.Fprintln(os.Stderr, styleToolDivider())
	for _, line := range strings.Split(detail, "\n") {
		fmt.Fprintf(os.Stderr, "    %s\n", line)
	}
	fmt.Fprintln(os.Stderr, styleToolDivider())
}

func toolOutputDetail(toolName string, input json.RawMessage) string {
	switch strings.ToLower(toolName) {
	case "write":
		return writeOutputDetail(input)
	case "edit":
		return editOutputDetail(input)
	default:
		return ""
	}
}

func writeOutputDetail(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var payload struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return ""
	}
	if payload.Content == "" {
		return "written content:"
	}
	return "written content:\n" + normalizeNewlines(payload.Content)
}

func editOutputDetail(input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var payload struct {
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal(input, &payload); err != nil {
		return ""
	}
	if payload.OldString == "" && payload.NewString == "" {
		return ""
	}
	return "applied diff:\n" + renderLineDiff(payload.OldString, payload.NewString)
}

func renderLineDiff(oldText, newText string) string {
	oldLines := splitDiffLines(oldText)
	newLines := splitDiffLines(newText)
	if len(oldLines) == 0 && len(newLines) == 0 {
		return "--- old\n+++ new"
	}

	diffLines := lineDiff(oldLines, newLines)
	var b strings.Builder
	b.WriteString("--- old\n")
	b.WriteString("+++ new\n")
	for i, line := range diffLines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	return b.String()
}

func lineDiff(oldLines, newLines []string) []string {
	n := len(oldLines)
	m := len(newLines)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}

	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	var out []string
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case oldLines[i] == newLines[j]:
			out = append(out, " "+oldLines[i])
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			out = append(out, "-"+oldLines[i])
			i++
		default:
			out = append(out, "+"+newLines[j])
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, "-"+oldLines[i])
	}
	for ; j < m; j++ {
		out = append(out, "+"+newLines[j])
	}
	return out
}

func splitDiffLines(s string) []string {
	s = normalizeNewlines(s)
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

func isPlanToolName(toolName string) bool {
	return toolName == "EnterPlanMode" || toolName == "ExitPlanMode"
}

func isExitPlanApproved(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "Plan approved")
}

// toolInputSummary extracts a short human-readable summary from tool input JSON.
// Returns "" if no suitable field is found.
func toolInputSummary(toolName string, input json.RawMessage) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(input, &obj); err != nil {
		return ""
	}

	// Common meaningful fields in rough priority order.
	for _, field := range []string{"command", "path", "old_path", "file_path", "query", "pattern", "description"} {
		if v, ok := obj[field]; ok {
			var s string
			if err := json.Unmarshal(v, &s); err == nil && s != "" {
				return abbreviate(s, 120)
			}
		}
	}

	_ = toolName
	return ""
}

// extractOutputText pulls the human-readable text from tool output bytes.
//
// The format varies depending on how the chunk was produced:
//   - Bare JSON string: TextOutput marshalled as json.Marshal(value) → "\"...\""
//   - {"type":"text","value":"..."}: from MarshalToolResultOutput
//   - Raw JSON value: JSONOutput
func extractOutputText(output json.RawMessage) string {
	if len(output) == 0 {
		return ""
	}
	// Bare JSON string (most common for TextOutput).
	if output[0] == '"' {
		var s string
		if err := json.Unmarshal(output, &s); err == nil {
			return strings.TrimSpace(s)
		}
	}
	// Structured {"type":"...","value":"..."} from MarshalToolResultOutput.
	var obj struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(output, &obj); err == nil && obj.Value != "" {
		return strings.TrimSpace(obj.Value)
	}
	// Fallback: pretty-print whatever JSON we got.
	var v any
	if err := json.Unmarshal(output, &v); err == nil {
		if pretty, err := json.MarshalIndent(v, "", "  "); err == nil {
			return strings.TrimSpace(string(pretty))
		}
	}
	return strings.TrimSpace(string(output))
}

func countLines(text string) int {
	text = strings.TrimRight(text, "\r\n")
	if text == "" {
		return 0
	}
	return len(strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n"))
}

// abbreviate truncates s to maxLen characters, appending "..." if needed.
// Newlines are replaced with "↵" so the output stays on a single line.
func abbreviate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\n", "↵")
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

// watchMCPOAuth polls the MCP manager's server status and prints authorization
// URLs to stderr when a server requires OAuth. Runs until ctx is cancelled.
func watchMCPOAuth(ctx context.Context, a *agentimpl.DefaultAgent) {
	notified := make(map[string]bool)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			mgr := a.MCPManager()
			if mgr == nil {
				continue
			}
			for _, info := range mgr.Status() {
				if info.OAuthURL != "" && !notified[info.Name] {
					notified[info.Name] = true
					fmt.Fprintf(os.Stderr, "\nMCP server %q requires authorization.\n", info.Name)
					fmt.Fprintf(os.Stderr, "Open this URL in your browser:\n  %s\n", info.OAuthURL)
					fmt.Fprint(os.Stderr, formatPromptHint()) // re-print the prompt hint
				}
			}
		}
	}
}

// spinner animates a small rotating indicator on stderr while the agent is
// thinking. It is safe to call Stop multiple times.
type spinner struct {
	once   sync.Once
	stopCh chan struct{}
	doneCh chan struct{}
}

func newSpinner() *spinner {
	return &spinner{
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
}

// Start launches the spinner goroutine. Call Stop to clear it.
// When NO_COLOR is set the spinner is suppressed and the goroutine exits
// immediately so Stop() is always safe to call.
func (s *spinner) Start() {
	go func() {
		defer close(s.doneCh)
		if noColor {
			<-s.stopCh
			return
		}
		frames := []string{"|", "/", "-", "\\"}
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		i := 0
		for {
			select {
			case <-s.stopCh:
				fmt.Fprint(os.Stderr, "\r \r") // erase the spinner character
				return
			case <-ticker.C:
				fmt.Fprintf(os.Stderr, "\r%s", frames[i%len(frames)])
				i++
			}
		}
	}()
}

// Stop clears the spinner and blocks until the goroutine has exited.
// Safe to call multiple times or before Start.
func (s *spinner) Stop() {
	s.once.Do(func() { close(s.stopCh) })
	<-s.doneCh
}

// startOAuthServer starts a local HTTP server on a random loopback port to
// receive OAuth callbacks from MCP servers.
//
// Returns the server's base URL (e.g. "http://127.0.0.1:12345") and the
// *http.Server. The caller should call wireOAuthCallbacks after constructing
// the agent, and close the server when done.
//
// On failure (e.g. no ports available), returns ("", nil) — MCP OAuth will
// still work if MCPOAuthRedirectBase is set via environment variable.
func startOAuthServer() (string, *http.Server) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Printf("cli: could not start OAuth callback server: %v", err)
		return "", nil
	}

	port := l.Addr().(*net.TCPAddr).Port
	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	srv := &http.Server{
		// Handler is set by wireOAuthCallbacks after the agent is constructed.
		Handler: http.NotFoundHandler(),
	}

	go func() {
		if err := srv.Serve(l); err != nil && err != http.ErrServerClosed {
			log.Printf("cli: OAuth server error: %v", err)
		}
	}()

	return base, srv
}

// wireOAuthCallbacks replaces the OAuth server's handler with one that routes
// authorization callbacks to the correct MCP server connection.
//
// Expected path: /sessions/{sessionID}/mcp/{serverName}/callback?code=...&state=...
func wireOAuthCallbacks(srv *http.Server, a *agentimpl.DefaultAgent) {
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Parse: /sessions/{sessionID}/mcp/{serverName}/callback
		// After stripping "/sessions/": "{sessionID}/mcp/{serverName}/callback"
		tail := strings.TrimPrefix(r.URL.Path, "/sessions/")
		parts := strings.SplitN(tail, "/", 4)
		// parts: [sessionID, "mcp", serverName, "callback"]
		if len(parts) < 4 || parts[1] != "mcp" || parts[3] != "callback" {
			http.NotFound(w, r)
			return
		}
		serverName := parts[2]

		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code parameter", http.StatusBadRequest)
			return
		}
		state := r.URL.Query().Get("state")

		mgr := a.MCPManager()
		if mgr == nil {
			http.Error(w, "MCP manager not available", http.StatusServiceUnavailable)
			return
		}

		if err := mgr.SubmitOAuthCode(serverName, code, state); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html>
<html>
<head><title>Authorization successful</title></head>
<body style="font-family:sans-serif;text-align:center;padding:3em">
  <h2>Authorization successful</h2>
  <p>You can close this tab and return to your terminal.</p>
</body>
</html>`)
	})
}

// getThreadPlanMode reads the persisted plan mode state for a thread.
func getThreadPlanMode(store *thread.Store, threadID string) bool {
	cfg, err := store.LoadConfig(threadID)
	if err != nil {
		return false
	}
	return cfg.PlanMode
}

// saveThreadPlanMode persists the plan mode state for a thread, preserving other config fields.
func saveThreadPlanMode(store *thread.Store, threadID string, enabled bool) {
	cfg, _ := store.LoadConfig(threadID)
	cfg.PlanMode = enabled
	_ = store.SaveConfig(threadID, cfg)
}

// planModeStr converts a planMode bool to the Mode string expected by PromptRequest.
func planModeStr(enabled bool) string {
	if enabled {
		return "plan"
	}
	return ""
}
