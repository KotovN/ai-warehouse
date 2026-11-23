package tool

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/KotovN/ai-warehouse/internal/config"
)

// Codex syncs into .codex/ (skills, AGENTS.md rules, config.toml MCP, hooks.json).
type Codex struct{ Root string }

func (c *Codex) ID() string        { return "codex" }
func (c *Codex) dir() string       { return filepath.Join(c.Root, ".codex") }
func (c *Codex) skills() string    { return filepath.Join(c.dir(), "skills") }
func (c *Codex) agentsMD() string  { return filepath.Join(c.Root, "AGENTS.md") }
func (c *Codex) tomlPath() string  { return filepath.Join(c.dir(), "config.toml") }
func (c *Codex) hooksPath() string { return filepath.Join(c.dir(), "hooks.json") }

func (c *Codex) Detect() bool { return dirExists(c.dir()) || dirExists(homeJoin(".codex")) }

func (c *Codex) Install(res Resources) ([]config.ManagedEntry, error) {
	var managed []config.ManagedEntry
	for _, sk := range res.Skills {
		dst := filepath.Join(c.skills(), sk.Name)
		if err := CopyDir(sk.Dir, dst); err != nil {
			return managed, err
		}
		managed = append(managed, config.ManagedEntry{Path: dst, Kind: "file"})
	}
	if rulesBlock(res) != "" {
		if err := replaceBlock(c.agentsMD(), rulesBlock(res)); err != nil {
			return managed, err
		}
		managed = append(managed, config.ManagedEntry{Path: c.agentsMD(), Kind: "block"})
	}
	if len(res.MCP.Servers) > 0 {
		if err := c.mergeMCP(res); err != nil {
			return managed, err
		}
		for name := range res.MCP.Servers {
			managed = append(managed, config.ManagedEntry{Path: c.tomlPath(), Kind: "mcp", Key: name})
		}
	}
	return managed, nil
}

// mergeMCP rewrites config.toml with our servers under mcp_servers.
// The file is re-serialized, so hand-written comments there are lost.
func (c *Codex) mergeMCP(res Resources) error {
	doc := map[string]any{}
	if b, err := os.ReadFile(c.tomlPath()); err == nil {
		if err := toml.Unmarshal(b, &doc); err != nil {
			return err
		}
	}
	servers := childMap(doc, "mcp_servers")
	for name, s := range res.MCP.Servers {
		if s.URL != "" {
			servers[name] = map[string]any{"url": s.URL, "headers": s.Headers}
		} else {
			entry := map[string]any{"command": s.Command, "args": s.Args}
			if len(s.Env) > 0 {
				entry["env"] = s.Env
			}
			servers[name] = entry
		}
	}
	if err := os.MkdirAll(c.dir(), 0o755); err != nil {
		return err
	}
	f, err := os.Create(c.tomlPath())
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(doc)
}

func (c *Codex) InjectHooks() ([]config.ManagedEntry, error) {
	return injectJSONHooks(c.hooksPath(), "hooks", "sessionStart")
}

func (c *Codex) Inventory() (Inventory, error) {
	sk, err := listDirs(c.skills())
	if err != nil {
		return Inventory{}, err
	}
	doc := map[string]any{}
	if b, err := os.ReadFile(c.tomlPath()); err == nil {
		if err := toml.Unmarshal(b, &doc); err != nil {
			return Inventory{}, err
		}
	}
	return Inventory{
		Skills:   sk,
		Servers:  keysOf(childMap(doc, "mcp_servers")),
		HooksSet: hooksInstalled(c.hooksPath()),
	}, nil
}
