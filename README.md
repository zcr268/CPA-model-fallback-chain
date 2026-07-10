# model-fallback-chain

**CLIProxyAPI plugin** — Multi-chain model fallback with circuit breaker and content anomaly detection.

## How it works

```
Client request → ModelRouter intercepts → matches a fallback chain
  → Executor tries backends in order:
     backend 1 (fail: 429/timeout/anomaly) → record penalty → try next
     backend 2 (fail: 502)                 → record penalty → try next
     backend 3 (success)                   → return response
  → If all fail: return last error
```

The plugin uses `host.model.execute` / `host.model.execute_stream` callbacks to dispatch
to built-in providers, so every backend in the chain is a real provider+model pair that
CLIProxyAPI already supports.

## Chain matching

Each chain has a `match` rule:

| Field | Type | Description |
|-------|------|-------------|
| `models` | `[]string` | Model names to match. Supports `*` suffix for prefix match. Empty = match all. |
| `source_formats` | `[]string` | Request protocols to match (`chat-completions`, `claude`, `gemini`, `openai-responses`). Empty = match all. |

The **first matching chain** wins.

## Fallback triggers

A backend is considered failed and the next one is tried when:

1. **HTTP retryable status**: 429, 502, 503, 504
2. **Timeout**: if `default_timeout_seconds` is set and the backend doesn't respond in time
3. **Content anomaly** (when `check_content_anomaly: true`):
   - Empty response body
   - Invalid JSON (non-streaming)
   - `{"error": ...}` in response body
   - Empty `choices` array (OpenAI format)
   - `{"type": "error"}` (Anthropic format)
   - SSE stream with no `data:` lines containing content

## Circuit breaker

After `max_penalty_failures` (default: 3) consecutive failures, a backend is temporarily
**penalized** for `penalty_cooldown_seconds` (default: 60). During cooldown, the backend
is skipped entirely. On success, all penalty state for that backend is cleared.

## Configuration

```yaml
plugins:
  enabled: true
  dir: /path/to/plugins
  configs:
    model-fallback-chain:
      enabled: true
      priority: 1
      # Global settings
      default_timeout_seconds: 30
      penalty_cooldown_seconds: 60
      max_penalty_failures: 3
      check_content_anomaly: true
      # Chain definitions
      chains:
        - name: "premium-fallback"
          match:
            models:
              - "claude-sonnet-4"
              - "claude-opus-4"
          backends:
            - provider: "anthropic"
              model: "claude-sonnet-4"
            - provider: "codex"
              model: "gpt-5.5"
            - provider: "xai"
              model: "grok-4.3"

        - name: "fast-fallback"
          match:
            models:
              - "gpt-4o*"
            source_formats:
              - "chat-completions"
          backends:
            - provider: "codex"
              model: "gpt-5.5"
            - provider: "antigravity"
              model: "gemini-3.1-pro"

        - name: "catch-all"
          match: {}
          backends:
            - provider: "antigravity"
              model: "gemini-3.1-pro"
            - provider: "codex"
              model: "gpt-5.5"
            - provider: "xai"
              model: "grok-4.3"
```

## Build

```bash
# Requires Go 1.26+ and CLIProxyAPI checked out as a sibling directory:
#   workspace/
#   ├── CLIProxyAPI/
#   └── model-fallback-chain/   (with replace directive in go.mod)

make build
# Output: bin/model-fallback-chain.so (Linux)
#         bin/model-fallback-chain.dylib (macOS)
#         bin/model-fallback-chain.dll (Windows)
```

Then copy the `.so`/`.dylib`/`.dll` to your CLIProxyAPI plugins directory.

## Architecture

```
main.go      — C FFI glue: plugin init, RPC dispatch, host callbacks
config.go    — YAML configuration parsing and normalization
router.go    — ModelRouter: request → chain matching
executor.go  — Executor: chain execution via host.model.execute callbacks
chain.go     — (logic is in executor.go's runFallbackChain)
health.go    — Circuit breaker: penalty tracking, cooldown, auto-recovery
anomaly.go   — Content anomaly detection for non-streaming and streaming
util.go      — String helpers
```

## Supported formats

The executor declares support for: `chat-completions`, `claude`, `gemini`, `openai-responses`.

This means the plugin can intercept requests from OpenAI-compatible clients, Claude Code,
Gemini clients, and OpenAI Responses API clients.
