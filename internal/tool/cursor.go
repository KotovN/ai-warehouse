package tool

import (
	"path/filepath"

	"github.com/KotovN/ai-warehouse/internal/config"
)

// Cursor syncs into .cursor/ (skills, rules, MCP config, hooks).
type Cursor struct{ Root string }

func (c *Cursor) ID() string        { return "cursor" }
func (c *Cursor) dir() string       { return filepath.Join(c.Root, ".cursor") }
func (c *Cursor) skills() string    { return filepath.Join(c.dir(), "skills") }
func (c *Cursor) rules() string     { return filepath.Join(c.dir(), "rules") }
func (c *Cursor) mcpPath() string   { return filepath.Join(c.dir(), "mcp.json") }
func (c *Cursor) hooksPath() string { return filepath.Join(c.dir(), "hooks.json") }

func (c *Cursor) Detect() bool { return dirExists(c.dir()) || dirExists(homeJoin(".cursor")) }

func (c *Cursor) Install(res Resources) ([]config.ManagedEntry, error) {
	var managed []config.ManagedEntry
	for _, sk := range res.Skills {
		dst := filepath.Join(c.skills(), sk.Name)
		if err := CopyDir(sk.Dir, dst); err != nil {
			return managed, err
		}
		managed = append(managed, config.ManagedEntry{Path: dst, Kind: "file"})
	}
	for _, r := range res.Rules {
		dst := filepath.Join(c.rules(), r.Name+".mdc")
		if err := writeFile(dst, []byte(r.Content)); err != nil {
			return managed, err
		}
		managed = append(managed, config.ManagedEntry{Path: dst, Kind: "file"})
	}
	if len(res.MCP.Servers) > 0 {
		if err := c.mergeMCP(res); err != nil {
			return managed, err
		}
		for name := range res.MCP.Servers {
			managed = append(managed, config.ManagedEntry{Path: c.mcpPath(), Kind: "mcp", Key: name})
		}
	}
	return managed, nil
}

// mergeMCP writes our servers into mcp.json under mcpServers.
func (c *Cursor) mergeMCP(res Resources) error {
	if len(res.MCP.Servers) == 0 {
		return nil
	}
	return mergeJSON(c.mcpPath(), func(m map[string]any) error {
		servers := childMap(m, "mcpServers")
		for name, s := range res.MCP.Servers {
			if s.URL != "" {
				servers[name] = map[string]any{"url": s.URL, "headers": s.Headers}
			} else {
				servers[name] = map[string]any{"command": s.Command, "args": s.Args, "env": s.Env}
			}
		}
		return nil
	})
}

// hooksEntry is the shape one hook takes in tool hook configs.
type hooksEntry struct {
	Command string `json:"command"`
}

func (c *Cursor) InjectHooks() ([]config.ManagedEntry, error) {
	return injectJSONHooks(c.hooksPath(), "hooks", "sessionStart")
}

func (c *Cursor) Inventory() (Inventory, error) {
	sk, err := listDirs(c.skills())
	if err != nil {
		return Inventory{}, err
	}
	m, err := readJSONMap(c.mcpPath())
	if err != nil {
		return Inventory{}, err
	}
	return Inventory{Skills: sk, Servers: keysOf(childMap(m, "mcpServers")), HooksSet: hooksInstalled(c.hooksPath())}, nil
}
