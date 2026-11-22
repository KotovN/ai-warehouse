package tool

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Cleanup helpers for uninstall: they undo exactly what the adapters wrote.

// StripBlock removes the managed block from a markdown file, reporting
// whether the file became empty (and was removed).
func StripBlock(path string) (bool, error) {
	return stripBlock(path)
}

// RemoveHookEntry removes the aiw session-start hook entry from a hooks
// config ({hooks: {sessionStart: [...]}}), leaving other entries alone.
func RemoveHookEntry(path string) error {
	m, err := readJSONMap(path)
	if err != nil {
		return err
	}
	hooks, ok := m["hooks"].(map[string]any)
	if !ok {
		return nil
	}
	list, ok := hooks["sessionStart"].([]any)
	if !ok {
		return nil
	}
	cmd, _ := pullCommand()
	var kept []any
	for _, e := range list {
		if he, ok := e.(map[string]any); ok && he["command"] == cmd {
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == len(list) {
		return nil // nothing ours to remove
	}
	if len(kept) == 0 {
		delete(hooks, "sessionStart")
	} else {
		hooks["sessionStart"] = kept
	}
	if isEmptyConfig(m) {
		return os.Remove(path)
	}
	return writeJSON(path, m)
}

// isEmptyConfig reports whether a config object carries no meaningful data
// (only empty maps/arrays/null), so the file can be deleted on uninstall.
func isEmptyConfig(m map[string]any) bool {
	for _, v := range m {
		switch t := v.(type) {
		case map[string]any:
			if len(t) > 0 {
				return false
			}
		case []any:
			if len(t) > 0 {
				return false
			}
		case nil:
		default:
			return false
		}
	}
	return true
}

// RemoveServerKey removes a managed MCP server entry from a config file,
// dispatching on extension (.toml for Codex, .json otherwise).
func RemoveServerKey(path, name string) error {
	if filepath.Ext(path) == ".toml" {
		return removeTOMLServerKey(path, name)
	}
	return removeJSONServerKey(path, name)
}

func removeJSONServerKey(path, name string) error {
	m, err := readJSONMap(path)
	if err != nil {
		return err
	}
	changed := false
	for _, top := range []string{"mcpServers", "servers", "mcp"} {
		if c, ok := m[top].(map[string]any); ok {
			if _, exists := c[name]; exists {
				delete(c, name)
				changed = true
			}
		}
	}
	if !changed {
		return nil
	}
	if isEmptyConfig(m) {
		return os.Remove(path)
	}
	return writeJSON(path, m)
}

func removeTOMLServerKey(path, name string) error {
	doc := map[string]any{}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := toml.Unmarshal(b, &doc); err != nil {
		return err
	}
	servers, ok := doc["mcp_servers"].(map[string]any)
	if !ok {
		return nil
	}
	if _, exists := servers[name]; !exists {
		return nil
	}
	delete(servers, name)
	if len(servers) == 0 {
		delete(doc, "mcp_servers")
	}
	if len(doc) == 0 {
		return os.Remove(path)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(doc)
}
