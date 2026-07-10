package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// tracker is the global health tracker, initialized on first configure.
var tracker = newBackendTracker(60, 3)

// execute handles non-streaming model execution.
func execute(raw []byte) ([]byte, error) {
	var req rpcExecutorRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	cfg := loadedConfig()
	ctx := context.Background()
	if cfg.DefaultTimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.DefaultTimeoutSec)*time.Second)
		defer cancel()
	}

	body, headers, errRun := runFallbackChain(ctx, cfg, req, false)
	if errRun != nil {
		return errorEnvelope("executor_error", errRun.Error()), nil
	}
	return okEnvelope(pluginapi.ExecutorResponse{Payload: body, Headers: headers})
}

// executeStream handles streaming model execution.
// Note: CLIProxyAPI's plugin executor streams via buffered chunks in the RPC response.
// We buffer the first successful backend's full stream, supporting inter-backend fallback.
func executeStream(raw []byte) ([]byte, error) {
	var req rpcExecutorRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	cfg := loadedConfig()
	ctx := context.Background()
	if cfg.DefaultTimeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(cfg.DefaultTimeoutSec)*time.Second)
		defer cancel()
	}

	body, headers, errRun := runFallbackChain(ctx, cfg, req, true)
	if errRun != nil {
		return errorEnvelope("executor_error", errRun.Error()), nil
	}

	chunks := []pluginapi.ExecutorStreamChunk{{Payload: body}}
	return okEnvelope(streamResponse{Headers: headers, Chunks: chunks})
}

// hostModelExecutionRequest wraps the host model execution request with a host callback id.
type hostModelExecutionRequest struct {
	pluginapi.HostModelExecutionRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

// runFallbackChain is the core engine: it finds the matching chain, iterates through
// available backends, and returns the first successful response.
// On failure (HTTP retryable status, timeout, or content anomaly), it records the failure
// and tries the next backend.
func runFallbackChain(ctx context.Context, cfg pluginConfig, req rpcExecutorRequest, stream bool) ([]byte, http.Header, error) {
	// Build a ModelRouteRequest-like structure to find the matching chain.
	routeReq := pluginapi.ModelRouteRequest{
		SourceFormat:       req.SourceFormat,
		RequestedModel:     strings.TrimSpace(req.Model),
		Body:               requestBody(req),
		AvailableProviders: availableProvidersFromMetadata(req.Metadata),
	}

	chain := findMatchingChain(cfg, routeReq)
	if chain == nil {
		// No chain matched — pass through to host model execution with original model.
		payload, status, errPass := hostExecute(ctx, req, req.Model, stream)
		if errPass != nil || isRetryableStatus(status) {
			return nil, nil, errPass
		}
		contentType := "application/json"
		if stream {
			contentType = "text/event-stream"
		}
		return payload, http.Header{"Content-Type": []string{contentType}}, nil
	}

	backends := availableBackends(cfg, chain, routeReq.AvailableProviders, tracker)
	if len(backends) == 0 {
		// All backends penalized or unavailable — try host default with original model.
		payload, status, errPass := hostExecute(ctx, req, req.Model, stream)
		if errPass != nil || isRetryableStatus(status) {
			return nil, nil, errPass
		}
		contentType := "application/json"
		if stream {
			contentType = "text/event-stream"
		}
		return payload, http.Header{"Content-Type": []string{contentType}}, nil
	}

	var lastErr error
	for _, backend := range backends {
		execModel := backend.Model
		if execModel == "" {
			execModel = strings.TrimSpace(req.Model)
		}
		key := backendKey{provider: backend.Provider, model: backend.Model}

		payload, status, errRun := hostExecute(ctx, req, execModel, stream)
		if errRun == nil && !isRetryableStatus(status) && !contentAnomaly(cfg, payload, stream) {
			tracker.recordSuccess(key)
			contentType := "application/json"
			if stream {
				contentType = "text/event-stream"
			}
			headers := http.Header{"Content-Type": []string{contentType}}
			return payload, headers, nil
		}

		// Failure — record it and try next backend.
		if isRetryableStatus(status) {
			tracker.recordFailure(key)
		}
		if errRun != nil {
			lastErr = errRun
		} else if contentAnomaly(cfg, payload, stream) {
			lastErr = fmt.Errorf("backend %s/%s: content anomaly detected", backend.Provider, backend.Model)
			tracker.recordFailure(key)
		} else {
			lastErr = fmt.Errorf("backend %s/%s: HTTP %d", backend.Provider, backend.Model, status)
		}
		// Continue to next backend
	}

	if lastErr != nil {
		return nil, nil, fmt.Errorf("fallback chain %q: all backends failed: %w", chain.Name, lastErr)
	}
	return nil, nil, fmt.Errorf("fallback chain %q: no backend available", chain.Name)
}

// hostExecute calls the host's model execution callback (non-streaming or streaming).
func hostExecute(ctx context.Context, req rpcExecutorRequest, execModel string, stream bool) ([]byte, int, error) {
	if stream {
		return hostExecuteStream(ctx, req, execModel)
	}
	raw, errCall := callHost(pluginabi.MethodHostModelExecute, hostModelExecutionRequest{
		HostModelExecutionRequest: pluginapi.HostModelExecutionRequest{
			EntryProtocol: req.SourceFormat,
			ExitProtocol:  req.SourceFormat,
			Model:         execModel,
			Stream:        false,
			Body:          requestBody(req),
		},
		HostCallbackID: req.HostCallbackID,
	})
	if errCall != nil {
		return nil, hostHTTPStatusFromError(errCall), errCall
	}
	var resp pluginapi.HostModelExecutionResponse
	if errDecode := json.Unmarshal(raw, &resp); errDecode != nil {
		return nil, 0, errDecode
	}
	if resp.StatusCode >= 400 {
		return nil, resp.StatusCode, fmt.Errorf("host model status %d", resp.StatusCode)
	}
	return resp.Body, resp.StatusCode, nil
}

// hostExecuteStream calls the host's streaming execution callback, reading all chunks.
func hostExecuteStream(ctx context.Context, req rpcExecutorRequest, execModel string) ([]byte, int, error) {
	raw, errCall := callHost(pluginabi.MethodHostModelExecuteStream, hostModelExecutionRequest{
		HostModelExecutionRequest: pluginapi.HostModelExecutionRequest{
			EntryProtocol: req.SourceFormat,
			ExitProtocol:  req.SourceFormat,
			Model:         execModel,
			Stream:        true,
			Body:          requestBody(req),
		},
		HostCallbackID: req.HostCallbackID,
	})
	if errCall != nil {
		return nil, hostHTTPStatusFromError(errCall), errCall
	}
	var resp pluginapi.HostModelStreamResponse
	if errDecode := json.Unmarshal(raw, &resp); errDecode != nil {
		return nil, 0, errDecode
	}
	if resp.StatusCode >= 400 {
		_ = closeHostModelStream(resp.StreamID)
		return nil, resp.StatusCode, fmt.Errorf("host model stream status %d", resp.StatusCode)
	}
	if strings.TrimSpace(resp.StreamID) == "" {
		return nil, 0, fmt.Errorf("host model stream: empty stream_id")
	}
	defer func() { _ = closeHostModelStream(resp.StreamID) }()

	var buf []byte
	for {
		chunkRaw, errRead := callHost(pluginabi.MethodHostModelStreamRead, pluginapi.HostModelStreamReadRequest{StreamID: resp.StreamID})
		if errRead != nil {
			return nil, hostHTTPStatusFromError(errRead), errRead
		}
		var chunk pluginapi.HostModelStreamReadResponse
		if errDecode := json.Unmarshal(chunkRaw, &chunk); errDecode != nil {
			return nil, 0, errDecode
		}
		if chunk.Error != "" {
			code := hostHTTPStatusFromError(fmtErrorf("%s", chunk.Error))
			return nil, code, fmtErrorf("%s", chunk.Error)
		}
		if len(chunk.Payload) > 0 {
			buf = append(buf, chunk.Payload...)
		}
		if chunk.Done {
			break
		}
	}
	return buf, http.StatusOK, nil
}

func closeHostModelStream(streamID string) error {
	_, errCall := callHost(pluginabi.MethodHostModelStreamClose, pluginapi.HostModelStreamCloseRequest{StreamID: streamID})
	return errCall
}

// ---------- helpers ----------

func requestBody(req rpcExecutorRequest) []byte {
	if len(req.OriginalRequest) > 0 {
		return req.OriginalRequest
	}
	return req.Payload
}

func isRetryableStatus(code int) bool {
	return code == 429 || code == 503 || code == 502 || code == 504
}

func hostHTTPStatusFromError(err error) int {
	if err == nil {
		return 0
	}
	msg := err.Error()
	for _, code := range []int{429, 503, 502, 504, 500} {
		if strings.Contains(msg, fmt.Sprintf("%d", code)) {
			return code
		}
	}
	return 0
}

func availableProvidersFromMetadata(meta map[string]any) []string {
	if meta == nil {
		return nil
	}
	raw, ok := meta["available_providers"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, okItem := item.(string); okItem {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
