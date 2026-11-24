package tool

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/KotovN/ai-warehouse/internal/config"
)

// pullCommand returns the hook command that re-syncs when a session starts.
// Paths with spaces are quoted so shells execute the binary reliably.
func pullCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(exe, " \t") {
		exe = `"` + exe + `"`
	}
	return exe + " pull --quiet", nil
}

// isAiwPullCommand reports whether a hook command string was installed by
// ai-warehouse, even if the binary was moved, upgraded, or invoked via go run.
func isAiwPullCommand(cmd string) bool {
	trimmed := strings.TrimSpace(cmd)
	if !strings.HasSuffix(trimmed, " pull --quiet") {
		return false
	}
	if cur, err := pullCommand(); err == nil && trimmed == cur {
		return true
	}
	prefix := strings.TrimSpace(strings.TrimSuffix(trimmed, " pull --quiet"))
	prefix = strings.Trim(prefix, `"'`)
	base := strings.ToLower(filepath.Base(prefix))
	base = strings.TrimSuffix(base, ".exe")
	return base == "aiw" || base == "aiwarehouse" || strings.HasSuffix(base, ".test")
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

func pathExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func resolveToolPath(root, p string) string {
	if p == "" {
		return ""
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		h, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		if p == "~" {
			return h
		}
		return filepath.Join(h, filepath.FromSlash(p[2:]))
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(root, filepath.FromSlash(p))
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

// injectHooks writes a session-start hook matching spec into a JSON config.
// Supported styles:
//   - "flat" (default):   {<key>: {<event>: [{"command": "<cmd>"}]}}
//   - "nested" (Claude):  {<key>: {<event>: [{"hooks": [{"type": "command", "command": "<cmd>"}]}]}}
func injectHooks(path string, spec HookSpec) ([]config.ManagedEntry, error) {
	cmd, err := pullCommand()
	if err != nil {
		return nil, err
	}
	hooksKey := spec.Key
	if hooksKey == "" {
		hooksKey = "hooks"
	}
	style := spec.Style
	if style == "" {
		style = "flat"
	}
	err = mergeJSON(path, func(m map[string]any) error {
		hooks := childMap(m, hooksKey)
		list, _ := hooks[spec.Event].([]any)
		// Replace any outdated aiw pull command in-place instead of duplicating.
		replaced := false
		for i, e := range list {
			if hookItemIsAiw(e) {
				list[i] = buildHookEntry(style, cmd)
				replaced = true
			}
		}
		if !replaced {
			list = append(list, buildHookEntry(style, cmd))
		}
		hooks[spec.Event] = list
		return nil
	})
	if err != nil {
		return nil, err
	}
	return []config.ManagedEntry{{
		Path:    path,
		Kind:    "hook",
		Key:     spec.Event,
		Section: hooksKey,
	}}, nil
}

func buildHookEntry(style, cmd string) map[string]any {
	if style == "nested" {
		return map[string]any{
			"hooks": []any{
				map[string]any{"type": "command", "command": cmd},
			},
		}
	}
	return map[string]any{"command": cmd}
}

func hookItemIsAiw(e any) bool {
	he, ok := e.(map[string]any)
	if !ok {
		return false
	}
	if cmd, ok := he["command"].(string); ok && isAiwPullCommand(cmd) {
		return true
	}
	if inner, ok := he["hooks"].([]any); ok {
		for _, sub := range inner {
			if sm, ok := sub.(map[string]any); ok {
				if cmd, ok := sm["command"].(string); ok && isAiwPullCommand(cmd) {
					return true
				}
			}
		}
	}
	return false
}

func hooksInstalledSpec(path string, spec HookSpec) bool {
	m, err := readJSONMap(path)
	if err != nil {
		return false
	}
	hooksKey := spec.Key
	if hooksKey == "" {
		hooksKey = "hooks"
	}
	hooks, ok := m[hooksKey].(map[string]any)
	if !ok {
		return false
	}
	list, ok := hooks[spec.Event].([]any)
	if !ok {
		return false
	}
	for _, e := range list {
		if hookItemIsAiw(e) {
			return true
		}
	}
	return false
}
