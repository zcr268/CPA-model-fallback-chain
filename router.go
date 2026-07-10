package main

import (
	"encoding/json"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// routeModel is the ModelRouter entry point.
// It checks if any configured chain matches the incoming request.
// If a chain matches and at least one backend is available, it returns
// Handled=true with TargetKind=self, so the host dispatches to our executor.
func routeModel(raw []byte) ([]byte, error) {
	var req rpcModelRouteRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	cfg := loadedConfig()
	if len(cfg.Chains) == 0 {
		return okEnvelope(pluginapi.ModelRouteResponse{Handled: false})
	}

	chain := findMatchingChain(cfg, req.ModelRouteRequest)
	if chain == nil {
		return okEnvelope(pluginapi.ModelRouteResponse{Handled: false})
	}

	// Check if at least one backend is available (not penalized and provider exists).
	available := availableBackends(cfg, chain, req.AvailableProviders, tracker)
	if len(available) == 0 {
		return okEnvelope(pluginapi.ModelRouteResponse{
			Handled: false,
			Reason:  "fallback_chain_no_available_backend: " + chain.Name,
		})
	}

	return okEnvelope(pluginapi.ModelRouteResponse{
		Handled:    true,
		TargetKind: pluginapi.ModelRouteTargetSelf,
		Reason:     "fallback_chain_matched: " + chain.Name,
	})
}

// findMatchingChain returns the first chain whose match rules accept the request.
func findMatchingChain(cfg pluginConfig, req pluginapi.ModelRouteRequest) *chainConfig {
	for i := range cfg.Chains {
		chain := &cfg.Chains[i]
		if chainMatches(chain, req) {
			return chain
		}
	}
	return nil
}

// chainMatches checks if a chain's match rules accept the incoming request.
func chainMatches(chain *chainConfig, req pluginapi.ModelRouteRequest) bool {
	// Source format filter (empty = accept all).
	if len(chain.Match.SourceFormats) > 0 {
		sf := strings.ToLower(strings.TrimSpace(req.SourceFormat))
		found := false
		for _, f := range chain.Match.SourceFormats {
			if f == sf {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Model filter (empty = accept all models).
	if len(chain.Match.Models) > 0 {
		requestedModel := strings.TrimSpace(req.RequestedModel)
		found := false
		for _, m := range chain.Match.Models {
			if modelMatch(m, requestedModel) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// modelMatch supports exact match, prefix match (trailing *), and case-insensitive comparison.
func modelMatch(pattern, model string) bool {
	pattern = strings.TrimSpace(pattern)
	model = strings.TrimSpace(model)
	if pattern == "" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(strings.ToLower(model), strings.ToLower(prefix))
	}
	return strings.EqualFold(pattern, model)
}
