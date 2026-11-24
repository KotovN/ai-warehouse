package config

import (
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LocalConfig is the machine-local config stored in a project partition.
type LocalConfig struct {
	Repo        string   `yaml:"repo"`
	Username    string   `yaml:"username,omitempty"`
	Scope       string   `yaml:"scope"` // project | user
	ProjectRoot string   `yaml:"projectRoot,omitempty"`
	Tools       []string `yaml:"tools,omitempty"`
	Ignore      []string `yaml:"ignore,omitempty"` // canonical selectors: skill:<n>, rule:<n>, mcp:<n>, cli:<n>
}

// State is mutable sync state for a project partition.
type State struct {
	LastPull string                    `json:"lastPull,omitempty" yaml:"lastPull,omitempty"` // RFC3339 timestamp
	Revision string                    `json:"revision,omitempty" yaml:"revision,omitempty"` // hub commit of last pull
	Managed  map[string][]ManagedEntry `json:"managed,omitempty" yaml:"managed,omitempty"`   // adapter id -> what it owns
}

// ManagedEntry is one path an adapter owns, and how to undo it on uninstall or prune.
type ManagedEntry struct {
	Path     string `json:"path" yaml:"path"`
	Kind     string `json:"kind" yaml:"kind"`                               // file | block | mcp | hook
	Key      string `json:"key,omitempty" yaml:"key,omitempty"`             // for mcp: server name; for hook: event name
	Section  string `json:"section,omitempty" yaml:"section,omitempty"`     // optional container key (e.g. mcpServers, hooks)
	Resource string `json:"resource,omitempty" yaml:"resource,omitempty"`   // canonical id: skill:<n>, rule:<n>, mcp:<n>, rules, hook:<ev>
}

// SaveState writes state.json as formatted JSON.
func SaveState(path string, st State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// LoadState reads state.json, returning a zero State if missing.
// Falls back to YAML for backwards compatibility with v1.0.0 state files.
func LoadState(path string) (State, error) {
	var st State
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(b, &st); err == nil {
		return st, nil
	}
	if err := yaml.Unmarshal(b, &st); err != nil {
		return st, fmt.Errorf("parse %s: %w", path, err)
	}
	return st, nil
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
