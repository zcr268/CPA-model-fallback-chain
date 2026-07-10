package main

import (
	"encoding/json"
	"strings"
	"sync/atomic"

	"gopkg.in/yaml.v3"
)

var currentConfig atomic.Value

// pluginConfig is the full configuration tree received from the host.
type pluginConfig struct {
	Chains               []chainConfig `yaml:"chains"`
	DefaultTimeoutSec    int           `yaml:"default_timeout_seconds"`
	PenaltyCooldownSec   int           `yaml:"penalty_cooldown_seconds"`
	MaxPenaltyFailures   int           `yaml:"max_penalty_failures"`
	CheckContentAnomaly  bool          `yaml:"check_content_anomaly"`
}

// chainConfig defines one fallback chain.
type chainConfig struct {
	Name     string         `yaml:"name"`
	Match    matchConfig    `yaml:"match"`
	Backends []backendConfig `yaml:"backends"`
}

// matchConfig determines which incoming requests this chain handles.
type matchConfig struct {
	Models       []string `yaml:"models"`
	SourceFormats []string `yaml:"source_formats"`
}

// backendConfig defines one step in a fallback chain.
type backendConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

func defaultPluginConfig() pluginConfig {
	return pluginConfig{
		DefaultTimeoutSec:   0,
		PenaltyCooldownSec:  60,
		MaxPenaltyFailures:  3,
		CheckContentAnomaly: true,
	}
}

func configure(raw []byte) error {
	var req lifecycleRequest
	if len(raw) > 0 {
		if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
			return errUnmarshal
		}
	}
	cfg := defaultPluginConfig()
	if len(req.ConfigYAML) > 0 {
		decoded, errDecode := decodeConfig(req.ConfigYAML)
		if errDecode != nil {
			return errDecode
		}
		cfg = decoded
	}
	currentConfig.Store(cfg)
	return nil
}

func decodeConfig(raw []byte) (pluginConfig, error) {
	cfg := defaultPluginConfig()
	if errUnmarshal := yaml.Unmarshal(raw, &cfg); errUnmarshal != nil {
		return pluginConfig{}, errUnmarshal
	}
	cfg = normalizeConfig(cfg)
	return cfg, nil
}

func normalizeConfig(cfg pluginConfig) pluginConfig {
	if cfg.PenaltyCooldownSec <= 0 {
		cfg.PenaltyCooldownSec = 60
	}
	if cfg.MaxPenaltyFailures <= 0 {
		cfg.MaxPenaltyFailures = 3
	}
	for i := range cfg.Chains {
		c := &cfg.Chains[i]
		c.Name = strings.TrimSpace(c.Name)
		for j := range c.Match.Models {
			c.Match.Models[j] = strings.TrimSpace(c.Match.Models[j])
		}
		for j := range c.Match.SourceFormats {
			c.Match.SourceFormats[j] = strings.TrimSpace(strings.ToLower(c.Match.SourceFormats[j]))
		}
		for j := range c.Backends {
			c.Backends[j].Provider = strings.ToLower(strings.TrimSpace(c.Backends[j].Provider))
			c.Backends[j].Model = strings.TrimSpace(c.Backends[j].Model)
		}
	}
	return cfg
}

func loadedConfig() pluginConfig {
	raw := currentConfig.Load()
	if cfg, ok := raw.(pluginConfig); ok {
		return cfg
	}
	return defaultPluginConfig()
}
