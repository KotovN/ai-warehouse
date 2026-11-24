// Package config defines the hub repo schemas and the machine-local config.
package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Warehouse is the team config committed to the hub repo (warehouse.yaml).
type Warehouse struct {
	Team      string   `yaml:"team"`
	Repo      string   `yaml:"repo,omitempty"`
	Reviewers []string `yaml:"reviewers,omitempty"`
	AutoPull  *bool    `yaml:"autoPull,omitempty"`
	ScopeHint string   `yaml:"scopeHint,omitempty"` // project | user, advisory only
}

// AutoPullEnabled reports whether session-start auto-pull hooks should be injected.
// Omitting autoPull defaults to true.
func (w Warehouse) AutoPullEnabled() bool {
	if w.AutoPull == nil {
		return true
	}
	return *w.AutoPull
}

// MCPServer is one shared MCP server definition.
type MCPServer struct {
	Command string            `yaml:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
	URL     string            `yaml:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
}

// MCPFile is the shared MCP registry committed to the hub repo (mcp/mcp.yaml).
type MCPFile struct {
	Servers map[string]MCPServer `yaml:"servers"`
}

// CliInstall holds either a single install command (v1.0.0 string) or per-OS
// commands keyed by GOOS ("windows", "darwin", "linux").
type CliInstall struct {
	Default string
	ByOS    map[string]string
}

// IsZero reports whether no install command is configured (used by yaml omitempty).
func (c CliInstall) IsZero() bool {
	return strings.TrimSpace(c.Default) == "" && len(c.ByOS) == 0
}

// ForOS returns the install command matching goos (e.g. runtime.GOOS),
// falling back to "default" or the single command string.
func (c CliInstall) ForOS(goos string) string {
	key := normalizeOS(goos)
	if len(c.ByOS) > 0 {
		if cmd := strings.TrimSpace(c.ByOS[key]); cmd != "" {
			return cmd
		}
		if cmd := strings.TrimSpace(c.ByOS["default"]); cmd != "" {
			return cmd
		}
	}
	return strings.TrimSpace(c.Default)
}

func normalizeOS(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "macos", "osx", "darwin":
		return "darwin"
	case "win", "win32", "windows":
		return "windows"
	case "linux":
		return "linux"
	default:
		return strings.ToLower(strings.TrimSpace(s))
	}
}

// UnmarshalYAML accepts either a plain string or an OS-keyed map.
func (c *CliInstall) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		var s string
		if err := value.Decode(&s); err != nil {
			return err
		}
		c.Default = s
		c.ByOS = nil
		return nil
	case yaml.MappingNode:
		var m map[string]string
		if err := value.Decode(&m); err != nil {
			return err
		}
		c.Default = ""
		c.ByOS = make(map[string]string, len(m))
		for k, v := range m {
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				c.ByOS[normalizeOS(k)] = trimmed
			}
		}
		return nil
	default:
		return fmt.Errorf("cli install must be a string or OS map, got YAML node kind %d", value.Kind)
	}
}

// MarshalYAML emits a plain string when only Default is set, or a map for ByOS.
func (c CliInstall) MarshalYAML() (any, error) {
	if len(c.ByOS) > 0 {
		return c.ByOS, nil
	}
	if strings.TrimSpace(c.Default) != "" {
		return c.Default, nil
	}
	return nil, nil
}

// CliTool is one team CLI tool: how to verify and install it on a machine.
type CliTool struct {
	Check   string     `yaml:"check"`             // command that verifies presence, e.g. "gh --version"
	URL     string     `yaml:"url,omitempty"`     // optional download or documentation URL
	Install CliInstall `yaml:"install,omitempty"` // command string or per-OS map
}

// ClisFile is the shared CLI registry committed to the hub repo (clis/clis.yaml).
type ClisFile struct {
	Clis map[string]CliTool `yaml:"clis"`
}

// Hook is one lifecycle hook; when it runs is defined by the Event name.
type Hook struct {
	Event   string `yaml:"event"` // session-start, session-end
	Command string `yaml:"command"`
}

// HooksFile is the shared hook registry committed to the hub repo (hooks/hooks.yaml).
type HooksFile struct {
	Hooks []Hook `yaml:"hooks"`
}

// Skill is the metadata of a hub skill directory (skills/<name>/SKILL.md front matter).
type Skill struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// Load loads the local config from a path, returning a zero config if missing.
func Load(path string) (LocalConfig, error) { return loadLocal(path) }

// Save writes the local config to a path.
func Save(path string, cfg LocalConfig) error { return saveLocal(path, cfg) }
