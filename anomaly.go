package main

import (
	"encoding/json"
	"strings"
)

// contentAnomaly checks whether a response body is anomalous and should trigger
// a fallback to the next backend.
//
// Checks performed (when CheckContentAnomaly is true):
//  1. Empty body (after trimming whitespace)
//  2. JSON parse failure (non-JSON response when JSON was expected)
//  3. Error object in the response (common patterns: {"error": {...}}, {"error": "..."})
//  4. Empty content in streaming SSE (no data: lines)
func contentAnomaly(cfg pluginConfig, payload []byte, stream bool) bool {
	if !cfg.CheckContentAnomaly {
		return false
	}
	if len(payload) == 0 {
		return true
	}
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" {
		return true
	}

	if stream {
		return streamContentAnomaly(trimmed)
	}
	return jsonContentAnomaly(trimmed)
}

// jsonContentAnomaly inspects non-streaming JSON responses.
func jsonContentAnomaly(body string) bool {
	// Parse as a generic JSON object.
	var obj map[string]any
	if errUnmarshal := json.Unmarshal([]byte(body), &obj); errUnmarshal != nil {
		// Not valid JSON — treat as anomaly.
		return true
	}

	// Check for top-level "error" field.
	if _, hasError := obj["error"]; hasError {
		return true
	}

	// Check for OpenAI-style error wrapper: {"error": {"message": "..."}}
	// (already covered above)

	// Check for empty choices array (OpenAI chat completion).
	if choices, ok := obj["choices"]; ok {
		if arr, okArr := choices.([]any); okArr && len(arr) == 0 {
			return true
		}
	}

	// Check for Anthropic-style error: {"type": "error"}
	if typ, ok := obj["type"]; ok {
		if s, okStr := typ.(string); okStr && s == "error" {
			return true
		}
	}

	return false
}

// streamContentAnomaly inspects buffered SSE stream responses.
func streamContentAnomaly(body string) bool {
	// For SSE streams, check if there are any data: lines with actual content.
	hasData := false
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if strings.TrimSpace(data) != "" && strings.TrimSpace(data) != "[DONE]" {
				hasData = true
				break
			}
		}
	}
	return !hasData
}
