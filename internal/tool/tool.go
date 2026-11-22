// Package tool installs hub resources into AI tool config locations.
package tool

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/KotovN/ai-warehouse/internal/config"
)

// Adapter syncs hub resources into one AI tool's config locations.
type Adapter interface {
	ID() string
	Detect() bool
	Install(res Resources) ([]config.ManagedEntry, error)
	InjectHooks() ([]config.ManagedEntry, error)
	Inventory() (Inventory, error)
}

// Resources is everything the hub offers for distribution.
type Resources struct {
	Skills []Skill
	Rules  []Rule
	MCP    config.MCPFile
	Hooks  config.HooksFile
	Clis   config.ClisFile
}

// Skill is one skill directory in the hub clone.
type Skill struct {
	Name string
	Dir  string // source path in the hub clone
}

// Rule is one shared rules markdown file.
type Rule struct {
	Name    string
	Content string
}

// Inventory reports what an adapter has installed.
type Inventory struct {
	Skills   []string
	Servers  []string // MCP server names
	HooksSet bool
}

// All returns the supported adapters rooted at root (project dir or home).
func All(root string) []Adapter {
	return []Adapter{
		&Cursor{Root: root},
		&Codex{Root: root},
		&VSCode{Root: root},
		&OpenCode{Root: root},
	}
}

// --- shared file helpers ---

// mergeJSON loads the JSON file at path (or starts empty), applies fn, writes it back.
func mergeJSON(path string, fn func(m map[string]any) error) error {
	m := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
	}
	if err := fn(m); err != nil {
		return err
	}
	return writeJSON(path, m)
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// readJSONMap loads a JSON object, tolerating a missing file.
func readJSONMap(path string) (map[string]any, error) {
	m := map[string]any{}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

const (
	blockBegin = "<!-- aiw:begin -->"
	blockEnd   = "<!-- aiw:end -->"
)

// replaceBlock inserts or replaces the managed block in a markdown file.
func replaceBlock(path, content string) error {
	s, err := readString(path)
	if err != nil {
		return err
	}
	start := strings.Index(s, blockBegin)
	end := strings.Index(s, blockEnd)
	if start >= 0 && end > start {
		s = s[:start] + blockBegin + "\n" + content + "\n" + blockEnd + s[end+len(blockEnd):]
	} else {
		if s != "" && !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		s += blockBegin + "\n" + content + "\n" + blockEnd + "\n"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(s), 0o644)
}

// stripBlock removes the managed block, reporting whether the file became empty.
func stripBlock(path string) (bool, error) {
	s, err := readString(path)
	if err != nil {
		return false, err
	}
	start := strings.Index(s, blockBegin)
	end := strings.Index(s, blockEnd)
	if start < 0 || end <= start {
		return false, nil
	}
	s = strings.TrimRight(s[:start], "\n") + s[end+len(blockEnd):]
	if strings.TrimSpace(s) == "" {
		return true, os.Remove(path)
	}
	return false, os.WriteFile(path, []byte(strings.TrimLeft(s, "\n")), 0o644)
}

func readString(path string) (string, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(b), err
}

// CopyDir copies a directory tree recursively.
func CopyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}
