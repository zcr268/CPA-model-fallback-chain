# model-fallback-chain

CLIProxyAPI C ABI plugin — multi-chain model fallback with circuit breaker and content anomaly detection.

## Purpose

A Go shared library plugin for [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) that provides configurable model fallback chains. When a model request fails (retryable HTTP status, timeout, or content anomaly), the plugin automatically tries the next backend in the chain.

## Dependencies

- Go 1.26+
- CLIProxyAPI v7 SDK (`replace` directive points to `../CLIProxyAPI`)
- CGO enabled (required for `-buildmode=c-shared`)

## Build

```bash
make build          # → bin/model-fallback-chain.so
make clean          # remove build artifacts
make fmt            # gofmt
```

## File Layout

| File | Purpose |
|------|---------|
| `main.go` | C FFI glue: plugin init, RPC dispatch, host callbacks, envelope helpers |
| `config.go` | YAML configuration parsing (chains, backends, global settings) |
| `router.go` | ModelRouter: request → chain matching (model + source_format rules) |
| `executor.go` | Executor + fallback engine: chain execution via host.model.execute callbacks |
| `health.go` | Circuit breaker: penalty tracking, cooldown, auto-recovery |
| `anomaly.go` | Content anomaly detection (empty body, error JSON, empty choices, no SSE data) |
| `util.go` | String helpers + fmtErrorf wrapper |

## Code Conventions

- English only in all code and docs
- `gofmt` compliant
- Comments explain "why", not "what"
- No `log.Fatal` (this is a shared library — cannot terminate the host process)

## How it works

```
Client → CLIProxyAPI → ModelRouter (this plugin)
  → matches chain by model/format rules
  → returns TargetSelf → host dispatches to Executor
Executor → iterates chain backends → host.model.execute per backend
  → first success: return response
  → failure (429/502/503/504/timeout/anomaly): try next backend
  → circuit breaker penalizes repeatedly failing backends
```

## Plugin Identifier

`model-fallback-chain`

## Supported Formats

Input/Output: `chat-completions`, `claude`, `gemini`, `openai-responses`
