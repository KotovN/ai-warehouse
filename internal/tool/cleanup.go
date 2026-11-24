package tool

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/KotovN/ai-warehouse/internal/config"
)

// Cleanup helpers for uninstall and stale-resource pruning: they undo exactly
// what the adapters wrote while preserving user-authored content.

// UndoManaged removes one managed entry; reports whether it acted.
func UndoManaged(e config.ManagedEntry, dryRun bool) (bool, error) {
	if _, err := os.Stat(e.Path); os.IsNotExist(err) {
		return false, nil // already gone
	}
	if dryRun {
		return true, nil
	}
	switch e.Kind {
	case "file":
		return true, config.RemoveAll(e.Path)
	case "block":
		_, err := StripBlock(e.Path)
		return true, err
	case "hook":
		return true, RemoveHookEntryAt(e.Path, e.Section, e.Key)
	case "mcp":
		return true, RemoveServerKeyIn(e.Path, e.Section, e.Key)
	default:
		return false, fmt.Errorf("unknown managed entry kind %q", e.Kind)
	}
}

// StripBlock removes the managed block from a markdown file, reporting
// whether the file became empty (and was removed).
func StripBlock(path string) (bool, error) {
	return stripBlock(path)
}

// RemoveHookEntry removes the aiw session-start hook entry from a default hooks
// config ({hooks: {sessionStart: [...]}}), leaving other entries alone.
func RemoveHookEntry(path string) error {
	return RemoveHookEntryAt(path, "", "")
}

// RemoveHookEntryAt removes the aiw hook entry from section/event (defaulting
// to hooks/sessionStart for legacy v1.0.0 state entries).
func RemoveHookEntryAt(path, section, event string) error {
	if section == "" {
		section = "hooks"
	}
	if event == "" {
		event = "sessionStart"
	}
	m, err := readJSONMap(path)
	if err != nil {
		return err
	}
	hooks, ok := m[section].(map[string]any)
	if !ok {
		return nil
	}
	list, ok := hooks[event].([]any)
	if !ok {
		return nil
	}
	var kept []any
	for _, e := range list {
		if hookItemIsAiw(e) {
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == len(list) {
		return nil // nothing ours to remove
	}
	if len(kept) == 0 {
		delete(hooks, event)
	} else {
		hooks[event] = kept
	}
	if len(hooks) == 0 {
		delete(m, section)
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
			if !isEmptyConfig(t) {
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
	return RemoveServerKeyIn(path, "", name)
}

// RemoveServerKeyIn removes a managed MCP server entry from a specific section
// (falling back to well-known sections when section is empty).
func RemoveServerKeyIn(path, section, name string) error {
	if filepath.Ext(path) == ".toml" {
		return removeTOMLServerKey(path, section, name)
	}
	return removeJSONServerKey(path, section, name)
}

func removeJSONServerKey(path, section, name string) error {
	m, err := readJSONMap(path)
	if err != nil {
		return err
	}
	sections := []string{"mcpServers", "servers", "mcp"}
	if section != "" {
		sections = []string{section}
	}
	changed := false
	for _, top := range sections {
		if c, ok := m[top].(map[string]any); ok {
			if _, exists := c[name]; exists {
				delete(c, name)
				changed = true
				if len(c) == 0 {
					delete(m, top)
				}
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

func removeTOMLServerKey(path, section, name string) error {
	if section == "" {
		section = "mcp_servers"
	}
	doc := map[string]any{}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(b)) > 0 {
		if err := toml.Unmarshal(b, &doc); err != nil {
			return err
		}
	}
	servers, ok := doc[section].(map[string]any)
	if !ok {
		return nil
	}
	if _, exists := servers[name]; !exists {
		return nil
	}
	delete(servers, name)
	if len(servers) == 0 {
		delete(doc, section)
	}
	if len(doc) == 0 {
		return os.Remove(path)
	}
	return writeTOML(path, doc)
}
