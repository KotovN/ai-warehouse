// Package config defines the hub repo schemas and the machine-local config.
package config

// Warehouse is the team config committed to the hub repo (warehouse.yaml).
type Warehouse struct {
	Team      string   `yaml:"team"`
	Repo      string   `yaml:"repo,omitempty"`
	Reviewers []string `yaml:"reviewers,omitempty"`
	AutoPull  bool     `yaml:"autoPull"`
	ScopeHint string   `yaml:"scopeHint,omitempty"` // project | user, advisory only
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

// CliTool is one team CLI tool: how to verify and install it on a machine.
type CliTool struct {
	Check   string `yaml:"check"`   // command that verifies presence, e.g. "gh --version"
	Install string `yaml:"install"` // command that installs it
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

// Save writes the local config to a path with a single blank line indent style.
func Save(path string, cfg LocalConfig) error { return saveLocal(path, cfg) }
