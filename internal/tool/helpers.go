package tool

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/KotovN/ai-warehouse/internal/config"
)

// pullCommand returns the hook command that re-syncs when a session starts.
func pullCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return exe + " pull --quiet", nil
}

// rulesBlock joins all hub rules into one markdown block for tools that take a block.
func rulesBlock(res Resources) string {
	var b strings.Builder
	for i, r := range res.Rules {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("## " + r.Name + "\n\n" + r.Content)
	}
	return b.String()
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func homeJoin(parts ...string) string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(append([]string{h}, parts...)...)
}

func writeFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// childMap returns m[key] as a map, creating it when missing.
func childMap(m map[string]any, key string) map[string]any {
	if c, ok := m[key].(map[string]any); ok {
		return c
	}
	c := map[string]any{}
	m[key] = c
	return c
}

func keysOf(m map[string]any) []string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func listDirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// injectJSONHooks writes a session-start hook pointing at the aiw binary.
// Shape: {<hooksKey>: {<event>: [{"command": "..."}]}}.
func injectJSONHooks(path, hooksKey, event string) ([]config.ManagedEntry, error) {
	cmd, err := pullCommand()
	if err != nil {
		return nil, err
	}
	err = mergeJSON(path, func(m map[string]any) error {
		hooks := childMap(m, hooksKey)
		list, ok := hooks[event].([]any)
		if !ok || len(list) == 0 {
			list = []any{}
		}
		for _, e := range list {
			if he, ok := e.(map[string]any); ok && he["command"] == cmd {
				return nil // already injected
			}
		}
		hooks[event] = append(list, map[string]any{"command": cmd})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return []config.ManagedEntry{{Path: path, Kind: "hook"}}, nil
}

func hooksInstalled(path string) bool {
	m, err := readJSONMap(path)
	if err != nil {
		return false
	}
	cmd, _ := pullCommand()
	hooks, ok := m["hooks"].(map[string]any)
	if !ok {
		return false
	}
	list, ok := hooks["sessionStart"].([]any)
	if !ok {
		return false
	}
	for _, e := range list {
		if he, ok := e.(map[string]any); ok && he["command"] == cmd {
			return true
		}
	}
	return false
}
