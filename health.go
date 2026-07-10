package main

import (
	"sync"
	"time"
)

// backendTracker tracks health state for backends across requests.
// It implements penalty/circuit-breaker: after N consecutive failures a backend
// is temporarily penalized and skipped for a cooldown period, then automatically
// restored.
type backendTracker struct {
	mu       sync.RWMutex
	records  map[backendKey]*backendState
	cooldown time.Duration
	maxFails int
}

type backendKey struct {
	provider string
	model    string
}

type backendState struct {
	consecutiveFails int
	penalizedUntil  time.Time
}

func newBackendTracker(cooldownSec, maxFails int) *backendTracker {
	if cooldownSec <= 0 {
		cooldownSec = 60
	}
	if maxFails <= 0 {
		maxFails = 3
	}
	return &backendTracker{
		records:  make(map[backendKey]*backendState),
		cooldown: time.Duration(cooldownSec) * time.Second,
		maxFails: maxFails,
	}
}

// isPenalized returns true if the backend is currently in cooldown.
func (bt *backendTracker) isPenalized(key backendKey) bool {
	bt.mu.RLock()
	defer bt.mu.RUnlock()
	st, ok := bt.records[key]
	if !ok {
		return false
	}
	return time.Now().Before(st.penalizedUntil)
}

// recordSuccess resets the failure counter for a backend.
func (bt *backendTracker) recordSuccess(key backendKey) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	delete(bt.records, key) // healthy again — drop all state
}

// recordFailure increments the failure counter and penalizes if threshold reached.
func (bt *backendTracker) recordFailure(key backendKey) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	st, ok := bt.records[key]
	if !ok {
		st = &backendState{}
		bt.records[key] = st
	}
	st.consecutiveFails++
	if st.consecutiveFails >= bt.maxFails {
		st.penalizedUntil = time.Now().Add(bt.cooldown)
	}
}

// availableBackends filters a chain's backends, returning only those that:
//  1. Have a matching built-in provider available in the host
//  2. Are not currently penalized
func availableBackends(cfg pluginConfig, chain *chainConfig, availableProviders []string, bt *backendTracker) []backendConfig {
	var result []backendConfig
	for _, b := range chain.Backends {
		if b.Provider == "" {
			continue
		}
		if !hasProvider(availableProviders, b.Provider) {
			continue
		}
		key := backendKey{provider: b.Provider, model: b.Model}
		if bt != nil && bt.isPenalized(key) {
			continue
		}
		result = append(result, b)
	}
	return result
}

func hasProvider(providers []string, key string) bool {
	key = lowerTrim(key)
	for _, p := range providers {
		if lowerTrim(p) == key {
			return true
		}
	}
	return false
}

func lowerTrim(s string) string {
	return lower(trim(s))
}
