// Package hub builds resources from the hub clone and syncs them via adapters.
package hub

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	RepoPath  string           // hub clone path
	Partition config.Partition // machine-data partition holding state.json
	Tools     []string
}

// Report summarizes one pull.
type Report struct {
	Installed   []string
	Skipped     []string
	MissingClis []string
}

// Pull installs resources into every detected, enabled tool.
func (s Syncer) Pull(dryRun bool) (Report, error) {
	// Best-effort refresh of the hub clone: an offline machine still pulls
	// from its last synced revision.
	if !dryRun {
		_ = provider.UpdateClone(s.RepoPath)
	}
	res, err := BuildResources(s.RepoPath)
	if err != nil {
		return Report{}, err
	}
	report := Report{MissingClis: CheckClis(res.Clis)}
	for _, a := range tool.All(s.Root) {
		if !s.wants(a.ID()) {
			continue
		}
		if !a.Detect() {
			report.Skipped = append(report.Skipped, a.ID())
			continue
		}
		if dryRun {
			report.Installed = append(report.Installed, a.ID())
			continue
		}
		managed, err := a.Install(res)
		if err != nil {
			return report, fmt.Errorf("%s install: %w", a.ID(), err)
		}
		hooks, err := a.InjectHooks()
		if err != nil {
			return report, fmt.Errorf("%s hooks: %w", a.ID(), err)
		}
		report.Installed = append(report.Installed, a.ID())
		if err := s.recordManaged(a.ID(), append(managed, hooks...)); err != nil {
			return report, err
		}
	}
	if dryRun {
		return Report{Installed: report.Installed, Skipped: report.Skipped, MissingClis: report.MissingClis}, nil
	}
	st, err := config.LoadState(s.statePath())
	if err != nil {
		return report, err
	}
	st.LastPull = time.Now().Format(time.RFC3339)
	st.Revision = revision(s.RepoPath)
	if err := config.SaveState(s.statePath(), st); err != nil {
		return report, err
	}
	return report, nil
}

// recordManaged merges this pull's managed entries into state.json.
func (s Syncer) recordManaged(id string, entries []config.ManagedEntry) error {
	st, err := config.LoadState(s.statePath())
	if err != nil {
		return err
	}
	if st.Managed == nil {
		st.Managed = map[string][]config.ManagedEntry{}
	}
	st.Managed[id] = entries
	return config.SaveState(s.statePath(), st)
}

func (s Syncer) statePath() string {
	return s.Partition.State
}

func (s Syncer) wants(id string) bool {
	if len(s.Tools) == 0 {
		return true
	}
	for _, t := range s.Tools {
		if t == id {
			return true
		}
	}
	return false
}

func revision(repoPath string) string {
	// Keep hub dependency-free: read HEAD via git plumbing through go-git
	// would add an import here; shell out once per pull instead.
	out, err := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// BuildResources reads skills, rules, mcp, hooks and clis from the hub clone.
func BuildResources(repoPath string) (tool.Resources, error) {
	res := tool.Resources{}
	for _, name := range config.SkillDirs(repoPath) {
		res.Skills = append(res.Skills, tool.Skill{Name: name, Dir: filepath.Join(repoPath, "skills", name)})
	}
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
	}
	sort.Slice(res.Rules, func(i, j int) bool { return res.Rules[i].Name < res.Rules[j].Name })
	res.MCP, _ = config.LoadMCP(repoPath) // missing file = no servers
	res.Hooks, _ = config.LoadHooks(repoPath)
	res.Clis, _ = config.LoadClis(repoPath)
	return res, nil
}

// CheckClis runs each registered CLI's check command, returning names not found.
func CheckClis(clis config.ClisFile) []string {
	var missing []string
	for _, name := range sortedKeys(clis.Clis) {
		check := clis.Clis[name].Check
		if check == "" {
			continue
		}
		fields := strings.Fields(check)
		if err := exec.Command(fields[0], fields[1:]...).Run(); err != nil {
			missing = append(missing, name)
		}
	}
	return missing
}

func sortedKeys(m map[string]config.CliTool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
