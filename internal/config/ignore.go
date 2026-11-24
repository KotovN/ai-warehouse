package config

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// NormalizeSelector validates and canonicalizes a resource selector of the form
// "skill:<name>", "rule:<name>", "mcp:<name>", or "cli:<name>".
func NormalizeSelector(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	kindPart, namePart, ok := strings.Cut(s, ":")
	if !ok {
		return "", fmt.Errorf("invalid selector %q: want kind:name (e.g. skill:hello, rule:style, mcp:fs, cli:hex)", raw)
	}
	kind, valid := normalizeIgnoreKind(kindPart)
	if !valid {
		return "", fmt.Errorf("unknown resource kind %q in %q (want skill, rule, mcp, cli)", kindPart, raw)
	}
	name := strings.TrimSpace(namePart)
	if name == "" {
		return "", fmt.Errorf("missing resource name in selector %q", raw)
	}
	return kind + ":" + name, nil
}

func normalizeIgnoreKind(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "skill", "skills":
		return "skill", true
	case "rule", "rules":
		return "rule", true
	case "mcp", "mcps":
		return "mcp", true
	case "cli", "clis":
		return "cli", true
	default:
		return "", false
	}
}

// IsIgnored reports whether kind:name is listed in ignore.
func IsIgnored(ignore []string, kind, name string) bool {
	if len(ignore) == 0 {
		return false
	}
	k, ok := normalizeIgnoreKind(kind)
	if !ok {
		return false
	}
	want := k + ":" + strings.TrimSpace(name)
	for _, entry := range ignore {
		if norm, err := NormalizeSelector(entry); err == nil && norm == want {
			return true
		}
	}
	return false
}

// AddIgnore merges normalized selectors into ignore, returning the sorted updated
// list and the subset that was newly added.
func AddIgnore(ignore []string, selectors ...string) (updated, added []string, err error) {
	set := map[string]bool{}
	for _, cur := range ignore {
		if norm, nerr := NormalizeSelector(cur); nerr == nil {
			set[norm] = true
		}
	}
	for _, raw := range selectors {
		norm, nerr := NormalizeSelector(raw)
		if nerr != nil {
			return nil, nil, nerr
		}
		if !set[norm] {
			set[norm] = true
			added = append(added, norm)
		}
	}
	updated = make([]string, 0, len(set))
	for k := range set {
		updated = append(updated, k)
	}
	sort.Strings(updated)
	sort.Strings(added)
	return updated, added, nil
}

// RemoveIgnore removes normalized selectors from ignore, returning the sorted
// remaining list and the subset that was removed.
func RemoveIgnore(ignore []string, selectors ...string) (updated, removed []string, err error) {
	set := map[string]bool{}
	for _, cur := range ignore {
		if norm, nerr := NormalizeSelector(cur); nerr == nil {
			set[norm] = true
		}
	}
	for _, raw := range selectors {
		norm, nerr := NormalizeSelector(raw)
		if nerr != nil {
			return nil, nil, nerr
		}
		if set[norm] {
			delete(set, norm)
			removed = append(removed, norm)
		}
	}
	updated = make([]string, 0, len(set))
	for k := range set {
		updated = append(updated, k)
	}
	sort.Strings(updated)
	sort.Strings(removed)
	return updated, removed, nil
}

// CanonicalResource returns the canonical resource selector for a ManagedEntry
// (e.g. "skill:hello", "rule:style", "mcp:fs", "rules", "hook:sessionStart"),
// inferring it from Path/Kind/Key for legacy v1.0/v1.1 state entries.
func (e ManagedEntry) CanonicalResource() string {
	if strings.TrimSpace(e.Resource) != "" {
		return e.Resource
	}
	switch e.Kind {
	case "mcp":
		if e.Key != "" {
			return "mcp:" + e.Key
		}
	case "hook":
		if e.Key != "" {
			return "hook:" + e.Key
		}
		return "hook"
	case "block":
		return "rules"
	case "file":
		parent := strings.ToLower(filepath.Base(filepath.Dir(e.Path)))
		base := filepath.Base(e.Path)
		switch parent {
		case "skills":
			return "skill:" + base
		case "rules":
			return "rule:" + strings.TrimSuffix(base, filepath.Ext(base))
		}
	}
	return ""
}
