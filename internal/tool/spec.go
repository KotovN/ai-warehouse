package tool

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/KotovN/ai-warehouse/internal/config"
	"gopkg.in/yaml.v3"
)

//go:embed presets/*.yaml
var builtinPresets embed.FS

// ToolSpec declaratively describes how an AI IDE/agent stores skills, rules,
// MCP servers, and session hooks. Built-in presets live in presets/*.yaml;
// teams and users can override or add new tools via <hub>/tools/*.yaml or
// ~/.ai-warehouse/tools/*.yaml without changing Go code.
type ToolSpec struct {
	ID      string      `yaml:"id"`
	Detect  []string    `yaml:"detect"`
	Project ScopePaths  `yaml:"project"`
	User    *ScopePaths `yaml:"user,omitempty"`
}

// ScopePaths maps each resource type to its destination path and format.
type ScopePaths struct {
	Skills string    `yaml:"skills,omitempty"`
	Rules  *RuleSpec `yaml:"rules,omitempty"`
	MCP    *MCPSpec  `yaml:"mcp,omitempty"`
	Hooks  *HookSpec `yaml:"hooks,omitempty"`
}

// RuleSpec configures rule installation: one file per rule ("dir") or a
// delimited markdown block inside a single file ("block").
type RuleSpec struct {
	Mode string `yaml:"mode"`          // "dir" | "block"
	Path string `yaml:"path"`          // directory path (dir) or file path (block)
	Ext  string `yaml:"ext,omitempty"` // file extension for dir mode, default ".md"
}

// MCPSpec configures MCP server serialization format and target config file.
type MCPSpec struct {
	Path   string `yaml:"path"`   // target JSON or TOML file
	Format string `yaml:"format"` // "json-standard" | "json-opencode" | "toml"
	Key    string `yaml:"key"`    // top-level server map key (e.g. "mcpServers", "servers", "mcp", "mcp_servers")
}

// HookSpec configures session-start hook injection.
type HookSpec struct {
	Path  string `yaml:"path"`            // target JSON hooks file
	Key   string `yaml:"key,omitempty"`   // top-level key, default "hooks"
	Event string `yaml:"event"`           // event name, e.g. "sessionStart" or "SessionStart"
	Style string `yaml:"style,omitempty"` // "flat" (default) | "nested"
}

// Validate checks a ToolSpec for required fields and supported codec names.
func (s ToolSpec) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("tool spec missing id")
	}
	if err := s.Project.validate(s.ID); err != nil {
		return err
	}
	if s.User != nil {
		if err := s.User.validate(s.ID); err != nil {
			return err
		}
	}
	return nil
}

func (p ScopePaths) validate(id string) error {
	if p.Rules != nil {
		if p.Rules.Path == "" {
			return fmt.Errorf("tool %q: rules.path is required", id)
		}
		switch p.Rules.Mode {
		case "dir", "block":
		default:
			return fmt.Errorf("tool %q: unsupported rules.mode %q (want dir|block)", id, p.Rules.Mode)
		}
	}
	if p.MCP != nil {
		if p.MCP.Path == "" || p.MCP.Key == "" {
			return fmt.Errorf("tool %q: mcp.path and mcp.key are required", id)
		}
		switch p.MCP.Format {
		case "json-standard", "json-opencode", "toml":
		default:
			return fmt.Errorf("tool %q: unsupported mcp.format %q (want json-standard|json-opencode|toml)", id, p.MCP.Format)
		}
	}
	if p.Hooks != nil {
		if p.Hooks.Path == "" || p.Hooks.Event == "" {
			return fmt.Errorf("tool %q: hooks.path and hooks.event are required", id)
		}
		switch p.Hooks.Style {
		case "", "flat", "nested":
		default:
			return fmt.Errorf("tool %q: unsupported hooks.style %q (want flat|nested)", id, p.Hooks.Style)
		}
	}
	return nil
}

// LoadSpecs loads built-in presets and merges any custom *.yaml/*.yml specs
// from extraDirs in order (later dirs override earlier ones by ID).
func LoadSpecs(extraDirs ...string) ([]ToolSpec, error) {
	byID := map[string]ToolSpec{}
	entries, err := fs.ReadDir(builtinPresets, "presets")
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !isYAMLFile(e.Name()) {
			continue
		}
		b, err := fs.ReadFile(builtinPresets, "presets/"+e.Name())
		if err != nil {
			return nil, err
		}
		spec, err := parseSpec(b, "presets/"+e.Name())
		if err != nil {
			return nil, err
		}
		byID[spec.ID] = spec
	}
	for _, dir := range extraDirs {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		dirEntries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read tool specs dir %s: %w", dir, err)
		}
		for _, e := range dirEntries {
			if e.IsDir() || !isYAMLFile(e.Name()) {
				continue
			}
			full := filepath.Join(dir, e.Name())
			b, err := os.ReadFile(full)
			if err != nil {
				return nil, err
			}
			spec, err := parseSpec(b, full)
			if err != nil {
				return nil, err
			}
			byID[spec.ID] = spec
		}
	}
	specs := make([]ToolSpec, 0, len(byID))
	for _, s := range byID {
		specs = append(specs, s)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].ID < specs[j].ID })
	return specs, nil
}

func parseSpec(b []byte, source string) (ToolSpec, error) {
	var spec ToolSpec
	if err := yaml.Unmarshal(b, &spec); err != nil {
		return ToolSpec{}, fmt.Errorf("parse tool spec %s: %w", source, err)
	}
	spec.ID = strings.ToLower(strings.TrimSpace(spec.ID))
	if err := spec.Validate(); err != nil {
		return ToolSpec{}, fmt.Errorf("%s: %w", source, err)
	}
	return spec, nil
}

func isYAMLFile(name string) bool {
	low := strings.ToLower(name)
	return strings.HasSuffix(low, ".yaml") || strings.HasSuffix(low, ".yml")
}

// LoadAdapters builds adapters for root and scope ("project"|"user"), merging
// built-in presets with custom tool specs from extraDirs.
func LoadAdapters(root, scope string, extraDirs ...string) ([]Adapter, error) {
	specs, err := LoadSpecs(extraDirs...)
	if err != nil {
		return nil, err
	}
	out := make([]Adapter, 0, len(specs))
	for _, spec := range specs {
		out = append(out, &SpecAdapter{Spec: spec, Root: root, Scope: scope})
	}
	return out, nil
}

// DefaultExtraDirs returns the standard custom tool spec directories:
// user-local (~/.ai-warehouse/tools) followed by hub-repo (<repoPath>/tools).
func DefaultExtraDirs(repoPath string) []string {
	dirs := []string{config.UserToolsDir()}
	if strings.TrimSpace(repoPath) != "" {
		dirs = append(dirs, filepath.Join(repoPath, "tools"))
	}
	return dirs
}

// ValidIDs returns the sorted list of adapter IDs.
func ValidIDs(adapters []Adapter) []string {
	ids := make([]string, 0, len(adapters))
	for _, a := range adapters {
		ids = append(ids, a.ID())
	}
	sort.Strings(ids)
	return ids
}
