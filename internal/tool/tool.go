// Package tool installs hub resources into AI tool config locations.
package tool

import (
	"bytes"
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
	SupportsHooks() bool
	Install(res Resources) ([]config.ManagedEntry, error)
	InjectHooks() ([]config.ManagedEntry, error)
	Inventory() (Inventory, error)
	ReadLocal() (LocalAssets, error)
}

// LocalAssets are the local skills, file-based rules, and MCP servers read from a tool.
type LocalAssets struct {
	Skills map[string]string           // skill name -> directory path
	Rules  map[string]string           // rule name -> markdown content
	MCP    map[string]config.MCPServer // server name -> definition
}

// Resources is everything the hub offers for distribution.
type Resources struct {
	Warehouse config.Warehouse
	Skills    []Skill
	Rules     []Rule
	MCP       config.MCPFile
	Hooks     config.HooksFile
	Clis      config.ClisFile
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
	Rules    []string
	Servers  []string // MCP server names
	HooksSet bool
}

// All returns the built-in (plus user-local custom) adapters rooted at root in project scope.
func All(root string) []Adapter {
	adapters, err := LoadAdapters(root, "project", config.UserToolsDir())
	if err != nil {
		// Fall back to built-in presets if user tools dir has a broken spec.
		adapters, _ = LoadAdapters(root, "project")
	}
	return adapters
}

// --- shared file helpers ---

// mergeJSON loads the JSON file at path (or starts empty if missing), applies fn, writes it back.
func mergeJSON(path string, fn func(m map[string]any) error) error {
	m, err := readJSONMap(path)
	if err != nil {
		return err
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

// readJSONMap loads a JSON object, tolerating a missing or empty file.
func readJSONMap(path string) (map[string]any, error) {
	m := map[string]any{}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return m, nil
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

func findBlockBounds(s string) (start, end int, ok bool) {
	start = strings.Index(s, blockBegin)
	if start < 0 {
		return -1, -1, false
	}
	relEnd := strings.Index(s[start+len(blockBegin):], blockEnd)
	if relEnd < 0 {
		return -1, -1, false
	}
	return start, start + len(blockBegin) + relEnd, true
}

func hasBlock(path string) bool {
	s, err := readString(path)
	if err != nil {
		return false
	}
	_, _, ok := findBlockBounds(s)
	return ok
}

// ReplaceRulesBlock rewrites the managed rules block in path using res.Rules.
// If res.Rules is empty, the managed block is stripped (deleting the file if empty).
func ReplaceRulesBlock(path string, res Resources) error {
	blk := rulesBlock(res)
	if blk == "" {
		_, err := stripBlock(path)
		return err
	}
	return replaceBlock(path, blk)
}

// replaceBlock inserts or replaces the managed block in a markdown file.
func replaceBlock(path, content string) error {
	s, err := readString(path)
	if err != nil {
		return err
	}
	if start, end, ok := findBlockBounds(s); ok {
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
	start, end, ok := findBlockBounds(s)
	if !ok {
		return false, nil
	}
	before := strings.TrimRight(s[:start], "\r\n")
	after := strings.TrimLeft(s[end+len(blockEnd):], "\r\n")
	var combined string
	switch {
	case before == "" && after == "":
		return true, os.Remove(path)
	case before == "":
		combined = after
	case after == "":
		combined = before + "\n"
	default:
		combined = before + "\n" + after
	}
	if strings.TrimSpace(combined) == "" {
		return true, os.Remove(path)
	}
	return false, os.WriteFile(path, []byte(combined), 0o644)
}

func readString(path string) (string, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(b), err
}

// CopyDir copies a directory tree recursively, preserving file permissions.
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
		info, err := d.Info()
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		perm := info.Mode().Perm()
		if perm == 0 {
			perm = 0o644
		}
		return os.WriteFile(target, b, perm)
	})
}
