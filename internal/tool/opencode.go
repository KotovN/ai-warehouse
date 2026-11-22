package tool

import (
	"path/filepath"

	"github.com/KotovN/ai-warehouse/internal/config"
)

// OpenCode syncs into .opencode/ (skills, AGENTS.md rules, opencode.json MCP).
// Hook injection via plugins lands with the plugin API; for now it is a no-op.
type OpenCode struct{ Root string }

func (o *OpenCode) ID() string         { return "opencode" }
func (o *OpenCode) dir() string        { return filepath.Join(o.Root, ".opencode") }
func (o *OpenCode) skills() string     { return filepath.Join(o.dir(), "skills") }
func (o *OpenCode) agentsMD() string   { return filepath.Join(o.Root, "AGENTS.md") }
func (o *OpenCode) configPath() string { return filepath.Join(o.Root, "opencode.json") }

func (o *OpenCode) Detect() bool {
	return dirExists(o.dir()) || dirExists(homeJoin(".config", "opencode"))
}

func (o *OpenCode) Install(res Resources) ([]config.ManagedEntry, error) {
	var managed []config.ManagedEntry
	for _, sk := range res.Skills {
		dst := filepath.Join(o.skills(), sk.Name)
		if err := CopyDir(sk.Dir, dst); err != nil {
			return managed, err
		}
		managed = append(managed, config.ManagedEntry{Path: dst, Kind: "file"})
	}
	if rulesBlock(res) != "" {
		if err := replaceBlock(o.agentsMD(), rulesBlock(res)); err != nil {
			return managed, err
		}
		managed = append(managed, config.ManagedEntry{Path: o.agentsMD(), Kind: "block"})
	}
	if len(res.MCP.Servers) > 0 {
		if err := o.mergeMCP(res); err != nil {
			return managed, err
		}
		for name := range res.MCP.Servers {
			managed = append(managed, config.ManagedEntry{Path: o.configPath(), Kind: "mcp", Key: name})
		}
	}
	return managed, nil
}

// mergeMCP writes our servers into opencode.json under mcp.
func (o *OpenCode) mergeMCP(res Resources) error {
	return mergeJSON(o.configPath(), func(m map[string]any) error {
		servers := childMap(m, "mcp")
		for name, s := range res.MCP.Servers {
			if s.URL != "" {
				servers[name] = map[string]any{"type": "remote", "url": s.URL, "headers": s.Headers}
			} else {
				cmd := append([]string{s.Command}, s.Args...)
				servers[name] = map[string]any{"type": "local", "command": cmd, "environment": s.Env}
			}
		}
		return nil
	})
}

func (o *OpenCode) InjectHooks() ([]config.ManagedEntry, error) { return nil, nil }

func (o *OpenCode) Inventory() (Inventory, error) {
	sk, err := listDirs(o.skills())
	if err != nil {
		return Inventory{}, err
	}
	m, err := readJSONMap(o.configPath())
	if err != nil {
		return Inventory{}, err
	}
	return Inventory{Skills: sk, Servers: keysOf(childMap(m, "mcp"))}, nil
}
