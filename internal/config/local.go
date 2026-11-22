package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LocalConfig is the machine-local config stored in a project partition.
type LocalConfig struct {
	Repo        string   `yaml:"repo"`
	Username    string   `yaml:"username"`
	Scope       string   `yaml:"scope"` // project | user
	ProjectRoot string   `yaml:"projectRoot,omitempty"`
	Tools       []string `yaml:"tools,omitempty"`
}

// State is mutable sync state for a project partition.
type State struct {
	LastPull string                    `yaml:"lastPull,omitempty"` // RFC3339 timestamp
	Revision string                    `yaml:"revision,omitempty"` // hub commit of last pull
	Managed  map[string][]ManagedEntry `yaml:"managed,omitempty"`  // adapter id -> what it owns
}

// ManagedEntry is one path an adapter owns, and how to undo it on uninstall.
type ManagedEntry struct {
	Path string `yaml:"path"`
	Kind string `yaml:"kind"`          // file | block | mcp
	Key  string `yaml:"key,omitempty"` // for mcp: server name
}

// SaveState writes state.json.
func SaveState(path string, st State) error {
	b, err := yaml.Marshal(st)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// LoadState reads state.json, returning a zero State if missing.
func LoadState(path string) (State, error) {
	var st State
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	return st, yaml.Unmarshal(b, &st)
}

func loadLocal(path string) (LocalConfig, error) {
	var cfg LocalConfig
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	return cfg, nil
}

func saveLocal(path string, cfg LocalConfig) error {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
