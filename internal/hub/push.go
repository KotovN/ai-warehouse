package hub

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/KotovN/ai-warehouse/internal/config"
	"github.com/KotovN/ai-warehouse/internal/provider"
	"github.com/KotovN/ai-warehouse/internal/tool"
)

// Pusher shares local tool-dir resources back to the hub repo.
type Pusher struct {
	Root      string // install root to scan for resources
	Partition config.Partition
	Repo      provider.Repo
	Username  string
	Reviewers []string
}

// PushReport summarizes one push.
type PushReport struct {
	Added    []string
	Edited   []string
	Branch   string
	PRURL    string
	ReusedPR bool
}

// Push scans local resources, commits changes to the user's branch, pushes,
// and opens or reuses a pull request. A fresh clone of origin is used as the
// base so the branch always builds on the latest default.
func (p Pusher) Push(dryRun bool) (PushReport, error) {
	report := PushReport{}
	skills, rules := scanLocal(p.Root)
	// Fresh full clone from origin = up-to-date diff base and commit base.
	// Full history is required to push safely from this checkout.
	if err := os.RemoveAll(p.Partition.Worktree); err != nil {
		return report, err
	}
	if err := p.Repo.CloneFull(p.Partition.Worktree); err != nil {
		return report, fmt.Errorf("clone for push: %w", err)
	}
	added, edited, err := DiffLocal(p.Root, p.Partition.Worktree)
	if err != nil {
		return report, err
	}
	report.Added, report.Edited = added, edited
	if len(added)+len(edited) == 0 {
		return report, nil
	}
	if dryRun {
		return report, nil
	}

	branch := "aiw/" + sanitize(p.Username)
	if err := provider.CreateBranch(p.Partition.Worktree, branch); err != nil {
		return report, err
	}
	if err := p.copyIntoWorktree(skills, rules, edited); err != nil {
		return report, err
	}
	msg := fmt.Sprintf("aiw: %s adds %v, edits %v", p.Username, report.Added, report.Edited)
	if _, err := provider.CommitAll(p.Partition.Worktree, msg); err != nil {
		return report, err
	}
	if err := p.Repo.PushBranch(p.Partition.Worktree, branch); err != nil {
		return report, err
	}
	report.Branch = branch

	// Reuse an existing PR for this user's branch instead of opening a duplicate.
	if prs, err := p.Repo.ListOpenPRs(); err == nil {
		for _, pr := range prs {
			if pr.Head == branch {
				report.PRURL, report.ReusedPR = pr.URL, true
				return report, nil
			}
		}
	}
	title := fmt.Sprintf("aiw: %s shares %d resource(s)", p.Username, len(report.Added)+len(report.Edited))
	url, err := p.Repo.OpenPR(branch, title, p.Reviewers)
	report.PRURL = url
	return report, err
}

// DiffLocal compares skills/rules from tool dirs at root with the resources
// in a hub clone, returning what push would add or edit.
func DiffLocal(root, clonePath string) (added, edited []string, err error) {
	hubRes, err := BuildResources(clonePath)
	if err != nil {
		return nil, nil, err
	}
	hubSkills := map[string]string{}
	for _, sk := range hubRes.Skills {
		hubSkills[sk.Name] = sk.Dir
	}
	hubRules := map[string]string{}
	for _, r := range hubRes.Rules {
		hubRules[r.Name] = r.Content
	}
	skills, rules := scanLocal(root)
	for _, name := range sortedStr(keys(skills)) {
		hdir, ok := hubSkills[name]
		if !ok {
			added = append(added, name)
		} else if !dirsEqual(skills[name], hdir) {
			edited = append(edited, name)
		}
	}
	for _, name := range sortedStr(keys(rules)) {
		if cur, ok := hubRules[name]; !ok {
			added = append(added, "rule:"+name)
		} else if cur != rules[name] {
			edited = append(edited, "rule:"+name)
		}
	}
	return added, edited, nil
}

// copyIntoWorktree writes new/edited skills and rules into the fresh clone.
func (p Pusher) copyIntoWorktree(skills, rules map[string]string, edited []string) error {
	skillsDir := filepath.Join(p.Partition.Worktree, "skills")
	if len(skills) > 0 {
		if err := os.MkdirAll(skillsDir, 0o755); err != nil {
			return err
		}
	}
	editedSet := make(map[string]bool, len(edited))
	for _, name := range edited {
		editedSet[name] = true
	}
	for _, name := range sortedStr(keys(skills)) {
		dst := filepath.Join(skillsDir, name)
		if editedSet[name] {
			if err := os.RemoveAll(dst); err != nil {
				return err
			}
		}
		if err := tool.CopyDir(skills[name], dst); err != nil {
			return err
		}
	}
	rulesDir := filepath.Join(p.Partition.Worktree, "rules")
	if len(rules) > 0 {
		if err := os.MkdirAll(rulesDir, 0o755); err != nil {
			return err
		}
	}
	for _, name := range sortedStr(keys(rules)) {
		dst := filepath.Join(rulesDir, name+".md")
		if err := os.WriteFile(dst, []byte(rules[name]), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// scanLocal collects skills and rules from installed tools' dirs at root.
// Skill names found in several tools are taken from the first tool.
func scanLocal(root string) (skills map[string]string, rules map[string]string) {
	skills, rules = map[string]string{}, map[string]string{}
	dirs := []string{
		filepath.Join(root, ".cursor", "skills"),
		filepath.Join(root, ".codex", "skills"),
		filepath.Join(root, ".opencode", "skills"),
	}
	for _, dir := range dirs {
		for _, name := range mustDirs(dir) {
			if _, ok := skills[name]; !ok {
				skills[name] = filepath.Join(dir, name)
			}
		}
	}
	rulesDir := filepath.Join(root, ".cursor", "rules")
	entries, err := os.ReadDir(rulesDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".mdc") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".mdc")
			if _, ok := rules[name]; ok {
				continue
			}
			if b, err := os.ReadFile(filepath.Join(rulesDir, e.Name())); err == nil {
				rules[name] = string(b)
			}
		}
	}
	return skills, rules
}

func mustDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

// dirsEqual compares two directory trees by relative path and content.
func dirsEqual(a, b string) bool {
	ma, mb := fileMap(a), fileMap(b)
	if len(ma) != len(mb) {
		return false
	}
	for k, va := range ma {
		vb, ok := mb[k]
		if !ok || va != vb {
			return false
		}
	}
	return true
}

func fileMap(root string) map[string]string {
	m := map[string]string{}
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		m[rel] = string(b)
		return nil
	})
	return m
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func sortedStr(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

var unsafeName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// sanitize makes a string safe as a branch name segment.
func sanitize(s string) string {
	s = unsafeName.ReplaceAllString(strings.TrimSpace(s), "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "unknown"
	}
	return s
}
