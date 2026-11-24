package tool

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/KotovN/ai-warehouse/internal/config"
)

// SpecAdapter implements Adapter purely from a declarative ToolSpec.
type SpecAdapter struct {
	Spec  ToolSpec
	Root  string
	Scope string // "project" | "user"
}

func (a *SpecAdapter) ID() string { return a.Spec.ID }

func (a *SpecAdapter) scopePaths() ScopePaths {
	if a.Scope == "user" && a.Spec.User != nil {
		return *a.Spec.User
	}
	return a.Spec.Project
}

func (a *SpecAdapter) resolve(p string) string {
	return resolveToolPath(a.Root, p)
}

func (a *SpecAdapter) Detect() bool {
	for _, d := range a.Spec.Detect {
		if pathExists(a.resolve(d)) {
			return true
		}
	}
	return false
}

func (a *SpecAdapter) SupportsHooks() bool {
	p := a.scopePaths()
	return p.Hooks != nil && p.Hooks.Path != "" && p.Hooks.Event != ""
}

func (a *SpecAdapter) Install(res Resources) ([]config.ManagedEntry, error) {
	var managed []config.ManagedEntry
	p := a.scopePaths()

	if p.Skills != "" {
		skillsDir := a.resolve(p.Skills)
		for _, sk := range res.Skills {
			dst := filepath.Join(skillsDir, sk.Name)
			if err := config.RemoveAll(dst); err != nil {
				return managed, err
			}
			if err := CopyDir(sk.Dir, dst); err != nil {
				return managed, err
			}
			managed = append(managed, config.ManagedEntry{
				Path:     dst,
				Kind:     "file",
				Resource: "skill:" + sk.Name,
			})
		}
	}

	if p.Rules != nil && p.Rules.Path != "" {
		target := a.resolve(p.Rules.Path)
		switch p.Rules.Mode {
		case "dir":
			ext := p.Rules.Ext
			if ext == "" {
				ext = ".md"
			}
			for _, r := range res.Rules {
				dst := filepath.Join(target, r.Name+ext)
				if err := writeFile(dst, []byte(r.Content)); err != nil {
					return managed, err
				}
				managed = append(managed, config.ManagedEntry{
					Path:     dst,
					Kind:     "file",
					Resource: "rule:" + r.Name,
				})
			}
		case "block":
			if blk := rulesBlock(res); blk != "" {
				if err := replaceBlock(target, blk); err != nil {
					return managed, err
				}
				managed = append(managed, config.ManagedEntry{
					Path:     target,
					Kind:     "block",
					Resource: "rules",
				})
			}
		}
	}

	if p.MCP != nil && p.MCP.Path != "" && len(res.MCP.Servers) > 0 {
		target := a.resolve(p.MCP.Path)
		if err := mergeMCPConfig(target, *p.MCP, res.MCP.Servers); err != nil {
			return managed, err
		}
		names := make([]string, 0, len(res.MCP.Servers))
		for name := range res.MCP.Servers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			managed = append(managed, config.ManagedEntry{
				Path:     target,
				Kind:     "mcp",
				Key:      name,
				Section:  p.MCP.Key,
				Resource: "mcp:" + name,
			})
		}
	}

	return managed, nil
}

func (a *SpecAdapter) InjectHooks() ([]config.ManagedEntry, error) {
	p := a.scopePaths()
	if p.Hooks == nil || p.Hooks.Path == "" {
		return nil, nil
	}
	target := a.resolve(p.Hooks.Path)
	return injectHooks(target, *p.Hooks)
}

func (a *SpecAdapter) Inventory() (Inventory, error) {
	inv := Inventory{}
	p := a.scopePaths()

	if p.Skills != "" {
		sk, err := listDirs(a.resolve(p.Skills))
		if err != nil {
			return Inventory{}, err
		}
		inv.Skills = sk
	}

	if p.Rules != nil && p.Rules.Path != "" {
		target := a.resolve(p.Rules.Path)
		switch p.Rules.Mode {
		case "dir":
			ext := p.Rules.Ext
			if ext == "" {
				ext = ".md"
			}
			rules, err := listFilesByExt(target, ext)
			if err != nil {
				return Inventory{}, err
			}
			inv.Rules = rules
		case "block":
			if hasBlock(target) {
				inv.Rules = []string{filepath.Base(target)}
			}
		}
	}

	if p.MCP != nil && p.MCP.Path != "" {
		servers, err := readMCPServers(a.resolve(p.MCP.Path), *p.MCP)
		if err != nil {
			return Inventory{}, err
		}
		names := make([]string, 0, len(servers))
		for k := range servers {
			names = append(names, k)
		}
		sort.Strings(names)
		inv.Servers = names
	}

	if p.Hooks != nil && p.Hooks.Path != "" {
		inv.HooksSet = hooksInstalledSpec(a.resolve(p.Hooks.Path), *p.Hooks)
	}

	return inv, nil
}

func (a *SpecAdapter) ReadLocal() (LocalAssets, error) {
	assets := LocalAssets{
		Skills: map[string]string{},
		Rules:  map[string]string{},
		MCP:    map[string]config.MCPServer{},
	}
	p := a.scopePaths()

	if p.Skills != "" {
		skillsDir := a.resolve(p.Skills)
		dirs, err := listDirs(skillsDir)
		if err != nil {
			return assets, err
		}
		for _, name := range dirs {
			assets.Skills[name] = filepath.Join(skillsDir, name)
		}
	}

	if p.Rules != nil && p.Rules.Mode == "dir" && p.Rules.Path != "" {
		rulesDir := a.resolve(p.Rules.Path)
		ext := p.Rules.Ext
		if ext == "" {
			ext = ".md"
		}
		names, err := listFilesByExt(rulesDir, ext)
		if err != nil {
			return assets, err
		}
		for _, name := range names {
			b, err := os.ReadFile(filepath.Join(rulesDir, name+ext))
			if err != nil {
				return assets, err
			}
			assets.Rules[name] = string(b)
		}
	}

	if p.MCP != nil && p.MCP.Path != "" {
		servers, err := readMCPServers(a.resolve(p.MCP.Path), *p.MCP)
		if err != nil {
			return assets, err
		}
		assets.MCP = servers
	}

	return assets, nil
}

// --- MCP format codecs ---

func mergeMCPConfig(path string, spec MCPSpec, servers map[string]config.MCPServer) error {
	switch spec.Format {
	case "toml":
		return mergeTOMLMCP(path, spec.Key, servers)
	case "json-opencode":
		return mergeJSON(path, func(m map[string]any) error {
			dst := childMap(m, spec.Key)
			for name, s := range servers {
				dst[name] = encodeOpenCodeMCP(s)
			}
			return nil
		})
	case "json-standard":
		return mergeJSON(path, func(m map[string]any) error {
			dst := childMap(m, spec.Key)
			for name, s := range servers {
				dst[name] = encodeStandardMCP(s)
			}
			return nil
		})
	default:
		return fmt.Errorf("unsupported mcp format %q", spec.Format)
	}
}

func encodeStandardMCP(s config.MCPServer) map[string]any {
	if s.URL != "" {
		entry := map[string]any{"url": s.URL}
		if len(s.Headers) > 0 {
			entry["headers"] = s.Headers
		}
		return entry
	}
	entry := map[string]any{"command": s.Command}
	if len(s.Args) > 0 {
		entry["args"] = s.Args
	}
	if len(s.Env) > 0 {
		entry["env"] = s.Env
	}
	return entry
}

func encodeOpenCodeMCP(s config.MCPServer) map[string]any {
	if s.URL != "" {
		entry := map[string]any{"type": "remote", "url": s.URL}
		if len(s.Headers) > 0 {
			entry["headers"] = s.Headers
		}
		return entry
	}
	cmd := append([]string{s.Command}, s.Args...)
	entry := map[string]any{"type": "local", "command": cmd}
	if len(s.Env) > 0 {
		entry["environment"] = s.Env
	}
	return entry
}

func mergeTOMLMCP(path, key string, servers map[string]config.MCPServer) error {
	doc := map[string]any{}
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && len(bytes.TrimSpace(b)) > 0 {
		if err := toml.Unmarshal(b, &doc); err != nil {
			return err
		}
	}
	dst := childMap(doc, key)
	for name, s := range servers {
		if s.URL != "" {
			entry := map[string]any{"url": s.URL}
			if len(s.Headers) > 0 {
				entry["headers"] = s.Headers
			}
			dst[name] = entry
		} else {
			entry := map[string]any{"command": s.Command}
			if len(s.Args) > 0 {
				entry["args"] = s.Args
			}
			if len(s.Env) > 0 {
				entry["env"] = s.Env
			}
			dst[name] = entry
		}
	}
	return writeTOML(path, doc)
}

func writeTOML(path string, doc map[string]any) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(doc); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func readMCPServers(path string, spec MCPSpec) (map[string]config.MCPServer, error) {
	out := map[string]config.MCPServer{}
	var raw map[string]any
	if spec.Format == "toml" {
		b, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		doc := map[string]any{}
		if len(bytes.TrimSpace(b)) > 0 {
			if err := toml.Unmarshal(b, &doc); err != nil {
				return nil, err
			}
		}
		raw, _ = doc[spec.Key].(map[string]any)
	} else {
		m, err := readJSONMap(path)
		if err != nil {
			return nil, err
		}
		raw, _ = m[spec.Key].(map[string]any)
	}
	for name, v := range raw {
		entry, ok := v.(map[string]any)
		if !ok {
			continue
		}
		out[name] = decodeMCPServer(entry, spec.Format)
	}
	return out, nil
}

func decodeMCPServer(entry map[string]any, format string) config.MCPServer {
	var s config.MCPServer
	if u, ok := entry["url"].(string); ok && u != "" {
		s.URL = u
		s.Headers = toStringMap(entry["headers"])
		return s
	}
	if format == "json-opencode" {
		cmdSlice := toStringSlice(entry["command"])
		if len(cmdSlice) > 0 {
			s.Command = cmdSlice[0]
			if len(cmdSlice) > 1 {
				s.Args = cmdSlice[1:]
			}
		}
		s.Env = toStringMap(entry["environment"])
		return s
	}
	if cmd, ok := entry["command"].(string); ok {
		s.Command = cmd
	}
	s.Args = toStringSlice(entry["args"])
	s.Env = toStringMap(entry["env"])
	return s
}

func toStringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		if len(t) == 0 {
			return nil
		}
		return append([]string(nil), t...)
	case []any:
		if len(t) == 0 {
			return nil
		}
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func toStringMap(v any) map[string]string {
	switch t := v.(type) {
	case map[string]string:
		if len(t) == 0 {
			return nil
		}
		out := make(map[string]string, len(t))
		for k, val := range t {
			out[k] = val
		}
		return out
	case map[string]any:
		if len(t) == 0 {
			return nil
		}
		out := make(map[string]string, len(t))
		for k, val := range t {
			if s, ok := val.(string); ok {
				out[k] = s
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func listFilesByExt(dir, ext string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ext) {
			continue
		}
		names = append(names, strings.TrimSuffix(e.Name(), ext))
	}
	sort.Strings(names)
	return names, nil
}
