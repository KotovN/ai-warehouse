package tool

import (
	"path/filepath"

	"github.com/KotovN/ai-warehouse/internal/config"
)

// VSCode syncs into .vscode/mcp.json and .github/copilot-instructions.md.
// VS Code has no agent lifecycle hooks, so InjectHooks is a no-op.
type VSCode struct{ Root string }

func (v *VSCode) ID() string      { return "vscode" }
func (v *VSCode) mcpPath() string { return filepath.Join(v.Root, ".vscode", "mcp.json") }
func (v *VSCode) instructions() string {
	return filepath.Join(v.Root, ".github", "copilot-instructions.md")
}

func (v *VSCode) Detect() bool {
	return dirExists(filepath.Join(v.Root, ".vscode")) || dirExists(homeJoin(".vscode"))
}

func (v *VSCode) Install(res Resources) ([]config.ManagedEntry, error) {
	var managed []config.ManagedEntry
	if len(res.MCP.Servers) > 0 {
		if err := v.mergeMCP(res); err != nil {
			return managed, err
		}
		for name := range res.MCP.Servers {
			managed = append(managed, config.ManagedEntry{Path: v.mcpPath(), Kind: "mcp", Key: name})
		}
	}
	if rulesBlock(res) != "" {
		if err := replaceBlock(v.instructions(), rulesBlock(res)); err != nil {
			return managed, err
		}
		managed = append(managed, config.ManagedEntry{Path: v.instructions(), Kind: "block"})
	}
	return managed, nil
}

// mergeMCP writes our servers into mcp.json under servers (VS Code GA format).
func (v *VSCode) mergeMCP(res Resources) error {
	return mergeJSON(v.mcpPath(), func(m map[string]any) error {
		servers := childMap(m, "servers")
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

func (v *VSCode) InjectHooks() ([]config.ManagedEntry, error) { return nil, nil }

func (v *VSCode) Inventory() (Inventory, error) {
	m, err := readJSONMap(v.mcpPath())
	if err != nil {
		return Inventory{}, err
	}
	return Inventory{Servers: keysOf(childMap(m, "servers"))}, nil
}
