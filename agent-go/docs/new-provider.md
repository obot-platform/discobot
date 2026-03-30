# Implementing a New LLM Provider

## File Structure

Create `providers/<id>/<id>.go` and `providers/<id>/<id>_test.go`. The `<id>` must match the provider's [models.dev](https://models.dev) identifier (e.g., `anthropic`, `google`, `mistral`).

## Step 1: Implement the Provider Interface

```go
// providers/<id>/<id>.go
package <id>

import (
    "context"
    "iter"
    "github.com/obot-platform/discobot/agent-go/message"
    "github.com/obot-platform/discobot/agent-go/providers"
)

const providerID = "<id>"

func init() {
    providers.Register(providerID, New)
}

type Provider struct { /* apiKey, baseURL, client */ }

func New(cfg providers.Config) (providers.Provider, error) { ... }
func (p *Provider) ID() string { return providerID }
func (p *Provider) Complete(ctx context.Context, req providers.CompleteRequest) iter.Seq2[message.ProviderMessageChunk, error] { ... }
func (p *Provider) ListModels(ctx context.Context) ([]providers.ModelInfo, error) { ... }
```

## Step 2: Factory Registration

The `init()` function calls `providers.Register(providerID, New)` to register a factory. The factory signature is `func(cfg providers.Config) (providers.Provider, error)`.

Config provides `cfg.APIKey()` and `cfg.BaseURL()`. Require `api_key`; use a sensible default for `base_url`. Strip trailing slashes from base URL.

## Step 3: Blank Import

Add to `cmd/agent-api/main.go`:

```go
import _ "github.com/obot-platform/discobot/agent-go/providers/<id>"
```

This ensures `init()` runs. The provider is auto-configured at startup via env var `{UPPER_ID}_API_KEY` (e.g., `ANTHROPIC_API_KEY`). Optional: `{UPPER_ID}_BASE_URL`.

## Step 4: Message Conversion

Convert `[]message.Message` to the provider's wire format. Each message has:
- `Role`: `"system"`, `"user"`, `"assistant"`, or `"tool"`
- `Parts`: slice of `message.Part` (type-switch on concrete types)

Handle these part types per role:

| Role | Part Types to Handle |
|------|---------------------|
| `system` | `TextPart` |
| `user` | `TextPart`, `ImagePart`, `FilePart` |
| `assistant` | `ReasoningPart`, `TextPart`, `ToolCallPart` |
| `tool` | `ToolResultPart` |

`tool` messages may also contain internal orchestration-only parts such as approval requests/responses. Provider conversion should ignore any non-`ToolResultPart` entries and serialize only concrete tool outputs back to the model.

**`ReasoningPart`** fields: `ID`, `Text` (summary), `ProviderMetadata` (opaque JSON from the provider — pass back as-is in multi-turn to preserve reasoning context). See "Reasoning Multi-Turn" section below.

**`ToolCallPart`** fields: `ToolCallID`, `ToolName`, `Input` (json.RawMessage).

**`ToolResultPart`** fields: `ToolCallID`, `ToolName`, `Output` (interface — type-switch to extract string):
- `TextOutput` → `.Value` string
- `JSONOutput` → `string(.Value)` raw JSON
- `ErrorTextOutput` → `.Value` string
- `ErrorJSONOutput` → `string(.Value)` raw JSON
- `ExecutionDeniedOutput` → `.Reason` string
- `ContentOutput` → iterate `.Value` items, extract `ContentTextItem.Text`

**`ImagePart`** fields: `Image` (URL or base64 string), `MediaType`.

## Step 5: Tool Conversion

Convert `[]providers.ToolDefinition` to the provider's tool/function format:
```go
type ToolDefinition struct {
    Name        string          // function name
    Description string          // may be empty
    InputSchema json.RawMessage // JSON Schema for parameters
}
```

## Step 6: Streaming via `Complete()`

`Complete()` returns `iter.Seq2[message.ProviderMessageChunk, error]` — a Go range iterator. Implementation pattern:

```go
func (p *Provider) Complete(ctx context.Context, req providers.CompleteRequest) iter.Seq2[message.ProviderMessageChunk, error] {
    return func(yield func(message.ProviderMessageChunk, error) bool) {
        // 1. Convert messages & tools to provider wire format
        // 2. Build HTTP request (with streaming enabled)
        // 3. Send request, check status
        // 4. Parse streaming response, yield chunks
        // 5. On error: yield(nil, err) then return
    }
}
```

**`yield` returns `false` when the consumer stops iterating (e.g., context cancelled). Always check the return value and stop if false.**

### Required Chunk Sequence

Yield chunks in this order:

1. `message.StreamStartChunk{}` — first chunk
2. `message.ResponseMetadataChunk{ID, Timestamp, ModelID}` — response metadata
3. Content chunks (text, tool calls, reasoning) — see below
4. `message.FinishChunk{FinishReason, Usage}` — last chunk

### Content Chunks

**Text streaming:**
```
TextStartChunk{ID}  →  TextDeltaChunk{ID, Delta} × N  →  TextEndChunk{ID}
```

**Tool call streaming:**
```
ToolInputStartChunk{ToolCallID, ToolName}  →  ToolInputDeltaChunk{ToolCallID, InputTextDelta} × N  →  ToolInputEndChunk{ToolCallID}
```

**Reasoning/thinking streaming:**
```
ReasoningStartChunk{ID}  →  ReasoningDeltaChunk{ID, Delta} × N  →  ReasoningEndChunk{ID}
```

Multiple content blocks can be interleaved (e.g., reasoning then text, or text then tool calls).

### Reasoning Multi-Turn

To maintain reasoning context across turns, the provider must:

1. **Request opaque reasoning data** from the API (e.g., OpenAI's `encrypted_content` via `include: ["reasoning.encrypted_content"]`).
2. **Store it on `ReasoningEndChunk.ProviderMetadata`** — the accumulator propagates this to `ReasoningPart.ProviderMetadata`, which persists in the thread store.
3. **Pass it back in `convertAssistantMessage`** — use `p.MetadataType()` to check whether the metadata is your provider's own format before using it. Fall back gracefully for foreign metadata (see below).
4. **Include required item fields** — some APIs require the message item following a reasoning item to include `id` and `status` fields. Capture item IDs during streaming (e.g., from `content_part.added` events) and include them when reconstructing items.

### Cross-Provider Reasoning (Required)

Thread history may contain `ReasoningPart` values produced by a different provider (e.g., the user switched from OpenAI to Anthropic mid-thread). Each provider's `ProviderMetadata` is opaque to other providers and must not be forwarded blindly.

**Always use `p.MetadataType()` to check the metadata type before using it:**

```go
case message.ReasoningPart:
    if p.MetadataType() == "<your-type>" {
        // Native format — pass through (e.g. Anthropic "thinking", OpenAI "reasoning").
        var block any
        json.Unmarshal(p.ProviderMetadata, &block)
        content = append(content, block)
    } else if p.Text != "" {
        // Foreign provider's reasoning — degrade gracefully using the summary text.
        // Wrap in a provider-appropriate construct (text block, summary item, etc.).
        content = append(content, map[string]any{"type": "text", "text": p.Text})
    }
    // Skip if no text and no compatible metadata.
```

`p.MetadataType()` reads the `"type"` field from `ProviderMetadata` — each provider embeds its own type tag (e.g. `"thinking"` for Anthropic, `"reasoning"` for OpenAI). If the type doesn't match, the metadata belongs to a different provider and must not be forwarded — use `p.Text` (the reasoning summary) as a plain-text fallback instead.

### FinishChunk

```go
message.FinishChunk{
    FinishReason: message.FinishReason{
        Unified: "stop"|"tool-calls"|"length"|"content-filter"|"error"|"other",
        Raw:     "<provider-specific string>",
    },
    Usage: message.Usage{
        InputTokens:  message.InputTokens{Total, NoCache, CacheRead, CacheWrite},
        OutputTokens: message.OutputTokens{Total, Text, Reasoning},
    },
}
```

Set `Unified` to `"tool-calls"` when the response contains tool call output items.

## Step 7: Data Storage

Disable provider-side data retention. Most providers store request/response data by default for monitoring. Set `"store": false` (or equivalent) in the request body for `Complete()` to prevent this. This ensures user data is not persisted on the provider's servers.

## Step 8: Handle `CompleteRequest` Parameters

| Field | Action |
|-------|--------|
| `req.Model` | Pass as model ID |
| `req.Messages` | Convert (Step 4) |
| `req.Tools` | Convert (Step 5), omit if empty |
| `req.MaxTokens` | Set if non-nil |
| `req.Temperature` | Set if non-nil |
| `req.TopP` | Set if non-nil |
| `req.Reasoning` | If `"enabled"`, activate provider's extended thinking/reasoning mode |
| `req.ProviderOptions` | Opaque JSON — merge into request body if non-nil |

## Step 9: `ListModels()`

Return `[]providers.ModelInfo` for known models. Preferred approach: query the provider's models endpoint for live IDs, then enrich with metadata from the `modelsdev` package.

```go
import "github.com/obot-platform/discobot/agent-go/providers/modelsdev"

// 1. Fetch live model IDs from provider API (e.g., GET /v1/models)
// 2. Enrich each model with metadata from models.dev:
for _, m := range apiModels {
    info := providers.ModelInfo{ID: m.ID, DisplayName: m.ID}
    if md := modelsdev.Lookup(providerID, m.ID); md != nil {
        info.DisplayName     = md.Name
        info.Reasoning       = md.Reasoning
        info.ContextWindow   = md.ContextWindow
        info.MaxOutputTokens = md.MaxOutputTokens
    }
    models = append(models, info)
}
```

The `modelsdev` package embeds `models-dev-api.json` and provides:
- `Lookup(providerID, modelID) *ModelInfo` — single model metadata
- `AllForProvider(providerID) []ModelInfo` — all models for a provider (fallback if no live API)

Do NOT set `ProviderID` — `ProviderRegistry` sets it automatically.

## Step 10: Tests

**Canonical reference: `providers/openai/openai_test.go` (unit) and `internal/integration/openai_test.go` (integration).** Every new provider must have equivalent coverage. Mirror the OpenAI test structure, adapting assertions to the new provider's wire format.

### Unit Tests (`providers/<id>/<id>_test.go`)

Test in the same package (access to internals). Use `httptest.NewServer` for HTTP mocking.

#### `TestNew`
- Missing API key returns error
- Default base URL is used when none provided
- Custom base URL is accepted and trailing slash is stripped

#### `TestProviderID`
- `p.ID()` returns the correct provider identifier string

#### `TestConvertMessages`
One subtest per role/part combination:
- System message → correct wire format
- User message with plain text (string shorthand vs array, per provider convention)
- User message with HTTP image URL
- User message with base64 image (data URL construction)
- Assistant message with text and tool call(s)
- Assistant message with tool call(s) only (no text)
- Assistant message with reasoning part **and** `ProviderMetadata` set (opaque data round-tripped)
- Assistant message with reasoning part **without** `ProviderMetadata` (fallback behavior — skipped, synthesized, or included as text, depending on the API)
- Tool result message (all `Output` types: `TextOutput`, `JSONOutput`, `ErrorTextOutput`, `ErrorJSONOutput`, `ExecutionDeniedOutput`, `ContentOutput`)
- Multiple tool results in one message

#### `TestConvertTools`
- Correct wire format (function schema nested correctly for the API)
- Empty description is omitted
- Nil input returns nil

#### `TestToolResultToString` (if helper exists)
Cover all 8 output types: `TextOutput`, `JSONOutput`, `ErrorTextOutput`, `ErrorJSONOutput`, `ExecutionDeniedOutput` (with and without reason), `ContentOutput`, nil.

#### `TestParseSSEStream`
One subtest per stream scenario:
- Text response — correct chunk sequence (`stream-start → response-metadata → text-start → text-delta(s) → text-end → finish`)
- Tool call response — `tool-input-start → tool-input-delta(s) → tool-input-end`
- Reasoning/thinking response — `reasoning-start → reasoning-delta(s) → reasoning-end`
- Stream terminated without a terminal sentinel (e.g., no `[DONE]` or no `message_stop`) — `FinishChunk` still emitted
- Error event/data — error propagated correctly (see "Error Handling" below)
- Unknown / ignored events (e.g., `ping`, comment lines) — no crash, no spurious chunks
- Provider-specific finish reasons map to correct `Unified` values (e.g., `max_tokens → "length"`)
- Usage tokens (including sub-fields: `CacheRead`, `CacheWrite`, `Reasoning`, `Text`) are extracted correctly

Add extra subtests for any streaming edge cases specific to the wire format — e.g., for Chat Completions APIs: tool call in one chunk, empty tool call arguments, no-duplicate `tool-input-end` when a trailing empty-args chunk arrives.

#### `TestComplete`
Use `httptest.NewServer`. One subtest per behavioral variant:
- Streaming text: verify endpoint URL, `Authorization` header, and full chunk sequence
- Tools: verify tool schema is serialised in the correct wire format; verify optional parameters (`max_tokens`, `temperature`) are sent when set
- Reasoning enabled: verify the provider-specific reasoning config field is sent (e.g., `reasoning_effort`, `thinking`, `include`)
- API error (non-200): error is returned without panicking

#### `TestListModels`
- Models are fetched from the provider API and enriched with `modelsdev` metadata
- API error is returned cleanly

#### `TestFactoryRegistration`
- `providers.Has("<id>")` returns true after the package is imported

### Integration Tests (`internal/integration/<id>_test.go`)

Package `integration`. Use the real API with a cheap model. Skip if API key unset:

```go
func provider(t *testing.T) providers.Provider {
    t.Helper()
    apiKey := os.Getenv("<UPPER_ID>_API_KEY")
    if apiKey == "" { t.Skip("<UPPER_ID>_API_KEY not set") }
    p, err := providers.New("<id>", providers.Config{"api_key": apiKey})
    if err != nil { t.Fatal(err) }
    return p
}
```

Required tests (mirror `internal/integration/openai_test.go`):

| Test | What it verifies |
|------|-----------------|
| `SimpleTextCompletion` | Response contains expected text; usage non-zero |
| `ToolCall` | Model calls the tool; finish reason is `"tool-calls"`; arguments are valid JSON |
| `ToolCallRoundTrip` | Two-turn conversation: tool call then tool result → text response mentioning the result |
| `MultiTurnConversation` | Model recalls information from an earlier turn |
| `StreamLifecycle` | Chunks arrive in the required order: `stream-start → response-metadata → text-start → text-delta → text-end → finish` |
| `ContextCancellation` | Cancelling the context terminates the iterator without a panic |
| `ReasoningCompletion` | Reasoning chunks arrive before text; reasoning text is non-empty (skip if no reasoning model available) |
| `ReasoningMultiTurn` | Reasoning context is preserved across turns — model answers a follow-up using prior reasoning (skip if no reasoning model available) |
| `ReasoningStreamLifecycle` | Chunk order: `stream-start → response-metadata → reasoning-start → reasoning-delta → reasoning-end → text-start → text-delta → text-end → finish` (skip if no reasoning model available) |

Run with: `go test -tags mcp_go_client_oauth -v ./internal/integration/...`

## SSE Parsing Pattern

Most LLM providers stream via Server-Sent Events. Reusable pattern:

```go
func parseSSEStream(body io.Reader, yield func(message.ProviderMessageChunk, error) bool) {
    scanner := bufio.NewScanner(body)
    scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
    var eventType string
    for scanner.Scan() {
        line := scanner.Text()
        if strings.HasPrefix(line, "event: ") {
            eventType = strings.TrimPrefix(line, "event: ")
        } else if strings.HasPrefix(line, "data: ") {
            data := strings.TrimPrefix(line, "data: ")
            if eventType != "" {
                if !handleEvent(eventType, []byte(data), yield) { return }
                eventType = ""
            }
        } else if line == "" {
            eventType = ""
        }
    }
    if err := scanner.Err(); err != nil {
        yield(nil, fmt.Errorf("<id>: read SSE stream: %w", err))
    }
}
```

## Error Handling

- Non-200 HTTP status: read body, yield `(nil, fmt.Errorf("<id>: API error %d: %s", status, body))`
- Stream errors: yield `(nil, err)` then return (no further chunks after error)
- JSON parse errors in events: yield `(nil, fmt.Errorf("<id>: parse <event>: %w", err))`
- Prefix all errors with `"<id>: "` for debuggability

## Reference Implementation

`providers/openai/` is the canonical reference:
- `providers/openai/openai.go` — complete implementation (Responses API)
- `providers/openai/openai_test.go` — full unit test suite to mirror
- `internal/integration/openai_test.go` — full integration test suite to mirror

`providers/openaicompatible/` is the reference for Chat Completions APIs (any provider using `POST /chat/completions`):
- `providers/openaicompatible/openaicompatible.go`
- `providers/openaicompatible/openaicompatible_test.go`
- `internal/integration/openaicompatible_test.go`
