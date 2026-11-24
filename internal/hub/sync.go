// Package hub builds resources from the hub clone and syncs them via adapters.
package hub

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/KotovN/ai-warehouse/internal/config"
	"github.com/KotovN/ai-warehouse/internal/provider"
	"github.com/KotovN/ai-warehouse/internal/tool"
)

// Syncer pulls hub resources into the AI tools chosen for this machine.
type Syncer struct {
	Root      string           // install root: project dir (project scope) or home (user scope)
	Scope     string           // "project" | "user" (defaults to "project")
	RepoPath  string           // hub clone path
	Partition config.Partition // machine-data partition holding state.json
	Tools     []string
	Ignore    []string // canonical selectors to exclude: skill:<n>, rule:<n>, mcp:<n>, cli:<n>
}

// Report summarizes one pull.
type Report struct {
	Installed   []string
	Skipped     []string
	Pruned      int
	MissingClis []string
	CliDetails  []CliStatus
	Warnings    []string
}

// Pull installs non-ignored resources into every detected (or explicitly requested)
// tool and prunes previously managed entries that were deleted from the hub or ignored.
func (s Syncer) Pull(dryRun bool) (Report, error) {
	report := Report{}
	// Best-effort refresh of the hub clone: an offline machine still pulls
	// from its last synced revision, while reporting refresh failures as warnings.
	if !dryRun {
		if _, err := os.Stat(filepath.Join(s.RepoPath, ".git")); err == nil {
			if err := provider.UpdateClone(s.RepoPath); err != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("hub refresh failed (using cached clone): %v", err))
			}
		}
	}
	rawRes, err := BuildResources(s.RepoPath)
	if err != nil {
		return report, err
	}
	res := FilterResources(rawRes, s.Ignore)

	scope := s.Scope
	if scope == "" {
		scope = "project"
	}
	adapters, err := tool.LoadAdapters(s.Root, scope, tool.DefaultExtraDirs(s.RepoPath)...)
	if err != nil {
		return report, err
	}
	report.CliDetails = InspectClisWithIgnore(rawRes.Clis, runtime.GOOS, s.Ignore)
	for _, c := range report.CliDetails {
		if !c.Installed && !c.Ignored {
			report.MissingClis = append(report.MissingClis, c.Name)
		}
	}

	st, err := config.LoadState(s.statePath())
	if err != nil {
		return report, err
	}
	if st.Managed == nil {
		st.Managed = map[string][]config.ManagedEntry{}
	}

	explicit := len(s.Tools) > 0
	for _, a := range adapters {
		if !s.wants(a.ID()) {
			continue
		}
		// Only require auto-detection when the user didn't explicitly pick tools.
		if !explicit && !a.Detect() {
			report.Skipped = append(report.Skipped, a.ID())
			continue
		}
		if dryRun {
			report.Installed = append(report.Installed, a.ID())
			continue
		}
		managed, err := a.Install(res)
		if err != nil {
			if len(managed) > 0 {
				st.Managed[a.ID()] = mergeManaged(st.Managed[a.ID()], managed)
				_ = config.SaveState(s.statePath(), st)
			}
			return report, fmt.Errorf("%s install: %w", a.ID(), err)
		}
		if res.Warehouse.AutoPullEnabled() {
			hooks, err := a.InjectHooks()
			if err != nil {
				st.Managed[a.ID()] = mergeManaged(st.Managed[a.ID()], managed)
				_ = config.SaveState(s.statePath(), st)
				return report, fmt.Errorf("%s hooks: %w", a.ID(), err)
			}
			managed = append(managed, hooks...)
		}

		// Prune any previously managed entries for this adapter that are no
		// longer part of the hub's desired (non-ignored) state.
		pruned, pruneWarns := pruneStale(st.Managed[a.ID()], managed)
		report.Pruned += pruned
		report.Warnings = append(report.Warnings, pruneWarns...)

		st.Managed[a.ID()] = managed
		report.Installed = append(report.Installed, a.ID())
	}
	if dryRun {
		return report, nil
	}
	st.LastPull = time.Now().Format(time.RFC3339)
	st.Revision = revision(s.RepoPath)
	if err := config.SaveState(s.statePath(), st); err != nil {
		return report, err
	}
	return report, nil
}

// FilterResources returns a copy of res with any ignored skills, rules, and
// MCP servers excluded.
func FilterResources(res tool.Resources, ignore []string) tool.Resources {
	if len(ignore) == 0 {
		return res
	}
	out := res
	out.Skills = nil
	for _, sk := range res.Skills {
		if !config.IsIgnored(ignore, "skill", sk.Name) {
			out.Skills = append(out.Skills, sk)
		}
	}
	out.Rules = nil
	for _, r := range res.Rules {
		if !config.IsIgnored(ignore, "rule", r.Name) {
			out.Rules = append(out.Rules, r)
		}
	}
	if len(res.MCP.Servers) > 0 {
		servers := make(map[string]config.MCPServer, len(res.MCP.Servers))
		for name, s := range res.MCP.Servers {
			if !config.IsIgnored(ignore, "mcp", name) {
				servers[name] = s
			}
		}
		out.MCP = config.MCPFile{Servers: servers}
	}
	return out
}

// ReapplyRuleBlocks updates or strips any managed markdown rule blocks recorded
// in st using the cached hub repo's rules after excluding excludeRules.
func ReapplyRuleBlocks(repoPath string, excludeRules []string, st *config.State, dryRun bool) (int, error) {
	rawRes, err := BuildResources(repoPath)
	if err != nil {
		return 0, err
	}
	filtered := FilterResources(rawRes, excludeRules)
	updated := 0
	seenPaths := map[string]bool{}
	for id, entries := range st.Managed {
		var kept []config.ManagedEntry
		for _, e := range entries {
			if e.Kind != "block" {
				kept = append(kept, e)
				continue
			}
			if _, statErr := os.Stat(e.Path); os.IsNotExist(statErr) {
				continue
			}
			updated++
			if !dryRun && !seenPaths[e.Path] {
				seenPaths[e.Path] = true
				if err := tool.ReplaceRulesBlock(e.Path, filtered); err != nil {
					return updated, err
				}
			}
			if len(filtered.Rules) > 0 {
				kept = append(kept, e)
			}
		}
		if !dryRun {
			if len(kept) == 0 {
				delete(st.Managed, id)
			} else {
				st.Managed[id] = kept
			}
		}
	}
	return updated, nil
}

type managedKey struct {
	Path    string
	Kind    string
	Key     string
	Section string
}

func keyForManaged(e config.ManagedEntry) managedKey {
	return managedKey{Path: e.Path, Kind: e.Kind, Key: e.Key, Section: e.Section}
}

func pruneStale(prev, current []config.ManagedEntry) (int, []string) {
	keep := make(map[managedKey]bool, len(current))
	for _, e := range current {
		keep[keyForManaged(e)] = true
	}
	pruned := 0
	var warnings []string
	for _, old := range prev {
		if keep[keyForManaged(old)] {
			continue
		}
		acted, err := tool.UndoManaged(old, false)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("prune %s (%s): %v", old.Path, old.Kind, err))
			continue
		}
		if acted {
			pruned++
		}
	}
	return pruned, warnings
}

func mergeManaged(existing, added []config.ManagedEntry) []config.ManagedEntry {
	seen := make(map[managedKey]bool, len(existing)+len(added))
	out := make([]config.ManagedEntry, 0, len(existing)+len(added))
	for _, e := range added {
		k := keyForManaged(e)
		if !seen[k] {
			seen[k] = true
			out = append(out, e)
		}
	}
	for _, e := range existing {
		k := keyForManaged(e)
		if !seen[k] {
			seen[k] = true
			out = append(out, e)
		}
	}
	return out
}

func (s Syncer) statePath() string {
	return s.Partition.State
}

func (s Syncer) wants(id string) bool {
	if len(s.Tools) == 0 {
		return true
	}
	for _, t := range s.Tools {
		if strings.EqualFold(strings.TrimSpace(t), id) {
			return true
		}
	}
	return false
}

func revision(repoPath string) string {
	out, err := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// BuildResources reads warehouse config, skills, rules, mcp, hooks and clis
// from the hub clone. Missing optional files are ignored, while YAML syntax
// or read errors are returned.
func BuildResources(repoPath string) (tool.Resources, error) {
	res := tool.Resources{}
	if w, err := config.LoadWarehouse(repoPath); err == nil {
		res.Warehouse = w
	} else if !os.IsNotExist(err) {
		return res, fmt.Errorf("warehouse.yaml: %w", err)
	}
	for _, name := range config.SkillDirs(repoPath) {
		res.Skills = append(res.Skills, tool.Skill{Name: name, Dir: filepath.Join(repoPath, "skills", name)})
	}
	sort.Slice(res.Skills, func(i, j int) bool { return res.Skills[i].Name < res.Skills[j].Name })

	ruleFiles, err := os.ReadDir(filepath.Join(repoPath, "rules"))
	if err == nil {
		for _, e := range ruleFiles {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(repoPath, "rules", e.Name()))
			if err != nil {
				return res, err
			}
			res.Rules = append(res.Rules, tool.Rule{Name: strings.TrimSuffix(e.Name(), ".md"), Content: string(b)})
		}
	} else if !os.IsNotExist(err) {
		return res, fmt.Errorf("read rules: %w", err)
	}
	sort.Slice(res.Rules, func(i, j int) bool { return res.Rules[i].Name < res.Rules[j].Name })

	if m, err := config.LoadMCP(repoPath); err == nil {
		res.MCP = m
	} else if !os.IsNotExist(err) {
		return res, fmt.Errorf("mcp/mcp.yaml: %w", err)
	}
	if h, err := config.LoadHooks(repoPath); err == nil {
		res.Hooks = h
	} else if !os.IsNotExist(err) {
		return res, fmt.Errorf("hooks/hooks.yaml: %w", err)
	}
	if c, err := config.LoadClis(repoPath); err == nil {
		res.Clis = c
	} else if !os.IsNotExist(err) {
		return res, fmt.Errorf("clis/clis.yaml: %w", err)
	}
	return res, nil
}
