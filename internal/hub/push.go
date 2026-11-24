package hub

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/KotovN/ai-warehouse/internal/config"
	"github.com/KotovN/ai-warehouse/internal/provider"
	"github.com/KotovN/ai-warehouse/internal/tool"
)

// Pusher shares local tool-dir resources (and CLI registrations) back to the hub repo.
type Pusher struct {
	Root      string // install root to scan for resources
	Scope     string // "project" | "user" (defaults to "project")
	Partition config.Partition
	Repo      provider.Repo
	Username  string
	Reviewers []string
}

// PushOptions controls what PushWithOptions selects and shares.
type PushOptions struct {
	DryRun   bool
	Select   []string // selectors: "skill:name", "rule:name", "mcp:name", or bare "name"
	Types    []string // category filter: "skills", "rules", "mcp"
	FromTool string   // optional single tool ID to scan from
}

// DiffItem describes one shareable local addition or modification.
type DiffItem struct {
	Kind       string           // "skill" | "rule" | "mcp"
	Name       string           // asset name
	Status     string           // "added" | "edited"
	Source     string           // adapter ID where this version was read from
	SkillDir   string           // local skill dir (Kind == "skill")
	Content    string           // rule markdown (Kind == "rule")
	Server     config.MCPServer // sanitized server definition (Kind == "mcp")
	HadSecrets bool             // true if env/headers were stripped from MCP server
}

// ID returns the canonical selector for this item (e.g. "skill:eli5").
func (d DiffItem) ID() string { return d.Kind + ":" + d.Name }

// PushReport summarizes one push.
type PushReport struct {
	Items    []DiffItem
	Added    []string
	Edited   []string
	Warnings []string
	Branch   string
	PRURL    string
	ReusedPR bool
}

// Push scans all local shareable resources and pushes them (backwards-compatible wrapper).
func (p Pusher) Push(dryRun bool) (PushReport, error) {
	return p.PushWithOptions(PushOptions{DryRun: dryRun})
}

// PushWithOptions scans local resources, filters them per opts, commits only
// selected changes to the user's branch, force-pushes, and opens or reuses a PR.
func (p Pusher) PushWithOptions(opts PushOptions) (PushReport, error) {
	report := PushReport{}
	// Fresh full clone from origin = up-to-date diff base and commit base.
	// config.RemoveAll clears read-only .git/objects bits so repeated pushes work on Windows.
	if err := config.RemoveAll(p.Partition.Worktree); err != nil {
		return report, err
	}
	if err := p.Repo.CloneFull(p.Partition.Worktree); err != nil {
		return report, fmt.Errorf("clone for push: %w", err)
	}
	allItems, err := DiffLocalItems(p.Root, p.scope(), p.Partition.Worktree, opts.FromTool)
	if err != nil {
		return report, err
	}
	selected, err := FilterItems(allItems, opts.Select, opts.Types)
	if err != nil {
		return report, err
	}
	report.Items = selected
	for _, it := range selected {
		label := formatDiffLabel(it)
		if it.Status == "added" {
			report.Added = append(report.Added, label)
		} else {
			report.Edited = append(report.Edited, label)
		}
		if it.HadSecrets {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s: stripped env/headers to avoid pushing secrets", it.ID()))
		}
	}
	if len(selected) == 0 {
		return report, nil
	}
	if opts.DryRun {
		return report, nil
	}

	branch := "aiw/" + sanitize(p.Username)
	if err := provider.CreateBranch(p.Partition.Worktree, branch); err != nil {
		return report, err
	}
	if err := p.copySelectedIntoWorktree(selected); err != nil {
		return report, err
	}
	msg := fmt.Sprintf("aiw: %s shares %s", p.Username, summarizeItems(selected))
	return p.commitPushAndPR(branch, msg, fmt.Sprintf("aiw: %s shares %d resource(s)", p.Username, len(selected)), report)
}

// AddCli adds or updates a CLI definition in clis/clis.yaml and shares it via PR.
func (p Pusher) AddCli(name string, cli config.CliTool, dryRun bool) (PushReport, error) {
	report := PushReport{}
	name = strings.TrimSpace(name)
	if name == "" {
		return report, fmt.Errorf("cli name is required")
	}
	if err := config.RemoveAll(p.Partition.Worktree); err != nil {
		return report, err
	}
	if err := p.Repo.CloneFull(p.Partition.Worktree); err != nil {
		return report, fmt.Errorf("clone for push: %w", err)
	}
	clis, err := config.LoadClis(p.Partition.Worktree)
	if err != nil && !os.IsNotExist(err) {
		return report, fmt.Errorf("load clis/clis.yaml: %w", err)
	}
	if clis.Clis == nil {
		clis.Clis = map[string]config.CliTool{}
	}
	status := "added"
	if existing, ok := clis.Clis[name]; ok {
		if reflect.DeepEqual(existing, cli) {
			return report, nil
		}
		status = "edited"
	}
	label := "cli:" + name
	if status == "added" {
		report.Added = []string{label}
	} else {
		report.Edited = []string{label}
	}
	if dryRun {
		return report, nil
	}
	clis.Clis[name] = cli
	branch := "aiw/" + sanitize(p.Username)
	if err := provider.CreateBranch(p.Partition.Worktree, branch); err != nil {
		return report, err
	}
	if err := config.SaveClis(p.Partition.Worktree, clis); err != nil {
		return report, err
	}
	msg := fmt.Sprintf("aiw: %s %s %s", p.Username, status, label)
	title := fmt.Sprintf("aiw: %s %s CLI %s", p.Username, status, name)
	return p.commitPushAndPR(branch, msg, title, report)
}

func (p Pusher) commitPushAndPR(branch, commitMsg, prTitle string, report PushReport) (PushReport, error) {
	if _, err := provider.CommitAll(p.Partition.Worktree, commitMsg); err != nil {
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
	url, err := p.Repo.OpenPR(branch, prTitle, p.Reviewers)
	report.PRURL = url
	return report, err
}

func (p Pusher) scope() string {
	if p.Scope == "" {
		return "project"
	}
	return p.Scope
}

// DiffLocal compares skills, dir-mode rules, and MCP servers across tools at
// root against clonePath (backwards-compatible summary helper).
func DiffLocal(root, clonePath string) (added, edited []string, err error) {
	return DiffLocalScope(root, "project", clonePath)
}

// DiffLocalScope is DiffLocal with an explicit install scope ("project"|"user").
func DiffLocalScope(root, scope, clonePath string) (added, edited []string, err error) {
	items, err := DiffLocalItems(root, scope, clonePath, "")
	if err != nil {
		return nil, nil, err
	}
	for _, it := range items {
		label := formatDiffLabel(it)
		if it.Status == "added" {
			added = append(added, label)
		} else {
			edited = append(edited, label)
		}
	}
	return added, edited, nil
}

// DiffLocalItems inspects every configured tool adapter (or only fromTool if set)
// and returns all added or edited skills, rules, and secret-stripped MCP servers.
// When multiple tools contain the same asset, any edited copy wins over an
// unchanged hub copy so edits made in secondary tools are never shadowed.
func DiffLocalItems(root, scope, clonePath, fromTool string) ([]DiffItem, error) {
	if scope == "" {
		scope = "project"
	}
	hubRes, err := BuildResources(clonePath)
	if err != nil {
		return nil, err
	}
	hubSkills := map[string]string{}
	for _, sk := range hubRes.Skills {
		hubSkills[sk.Name] = sk.Dir
	}
	hubRules := map[string]string{}
	for _, r := range hubRes.Rules {
		hubRules[r.Name] = r.Content
	}
	hubMCP := map[string]config.MCPServer{}
	for name, s := range hubRes.MCP.Servers {
		clean, _ := stripMCPSecrets(s)
		hubMCP[name] = clean
	}

	adapters, err := tool.LoadAdapters(root, scope, tool.DefaultExtraDirs(clonePath)...)
	if err != nil {
		return nil, err
	}
	if fromTool != "" {
		var matched []tool.Adapter
		for _, a := range adapters {
			if strings.EqualFold(a.ID(), fromTool) {
				matched = append(matched, a)
			}
		}
		if len(matched) == 0 {
			return nil, fmt.Errorf("unknown tool %q (available: %s)", fromTool, strings.Join(tool.ValidIDs(adapters), ", "))
		}
		adapters = matched
	}

	found := map[string]DiffItem{}
	for _, a := range adapters {
		local, err := a.ReadLocal()
		if err != nil {
			continue
		}
		// Skills
		for _, name := range sortedStr(keys(local.Skills)) {
			dir := local.Skills[name]
			key := "skill:" + name
			if hdir, ok := hubSkills[name]; !ok {
				if _, exists := found[key]; !exists {
					found[key] = DiffItem{Kind: "skill", Name: name, Status: "added", Source: a.ID(), SkillDir: dir}
				}
			} else if !dirsEqual(dir, hdir) {
				if _, exists := found[key]; !exists {
					found[key] = DiffItem{Kind: "skill", Name: name, Status: "edited", Source: a.ID(), SkillDir: dir}
				}
			}
		}
		// File-based rules
		for _, name := range sortedStr(keys(local.Rules)) {
			content := local.Rules[name]
			key := "rule:" + name
			if cur, ok := hubRules[name]; !ok {
				if _, exists := found[key]; !exists {
					found[key] = DiffItem{Kind: "rule", Name: name, Status: "added", Source: a.ID(), Content: content}
				}
			} else if cur != content {
				if _, exists := found[key]; !exists {
					found[key] = DiffItem{Kind: "rule", Name: name, Status: "edited", Source: a.ID(), Content: content}
				}
			}
		}
		// MCP servers (secrets stripped before comparing & sharing)
		mcpNames := make([]string, 0, len(local.MCP))
		for name := range local.MCP {
			mcpNames = append(mcpNames, name)
		}
		sort.Strings(mcpNames)
		for _, name := range mcpNames {
			clean, hadSecrets := stripMCPSecrets(local.MCP[name])
			if clean.Command == "" && clean.URL == "" {
				continue
			}
			key := "mcp:" + name
			if cur, ok := hubMCP[name]; !ok {
				if _, exists := found[key]; !exists {
					found[key] = DiffItem{Kind: "mcp", Name: name, Status: "added", Source: a.ID(), Server: clean, HadSecrets: hadSecrets}
				}
			} else if !mcpEqual(cur, clean) {
				if _, exists := found[key]; !exists {
					found[key] = DiffItem{Kind: "mcp", Name: name, Status: "edited", Source: a.ID(), Server: clean, HadSecrets: hadSecrets}
				}
			}
		}
	}

	items := make([]DiffItem, 0, len(found))
	for _, it := range found {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return kindOrder(items[i].Kind) < kindOrder(items[j].Kind)
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

// FilterItems filters diff items by resource types and/or explicit selectors.
func FilterItems(items []DiffItem, selectArgs, types []string) ([]DiffItem, error) {
	filtered := items
	if len(types) > 0 {
		allowed := map[string]bool{}
		for _, t := range types {
			k, ok := normalizeKind(t)
			if !ok {
				return nil, fmt.Errorf("unknown push type %q (want skills, rules, mcp)", t)
			}
			allowed[k] = true
		}
		var out []DiffItem
		for _, it := range filtered {
			if allowed[it.Kind] {
				out = append(out, it)
			}
		}
		filtered = out
	}
	if len(selectArgs) == 0 {
		return filtered, nil
	}
	var out []DiffItem
	seen := map[string]bool{}
	for _, sel := range selectArgs {
		sel = strings.TrimSpace(sel)
		if sel == "" {
			continue
		}
		var matches []DiffItem
		if kindPart, namePart, hasColon := strings.Cut(sel, ":"); hasColon {
			k, ok := normalizeKind(kindPart)
			if !ok {
				return nil, fmt.Errorf("unknown selector kind in %q (want skill:<name>, rule:<name>, mcp:<name>)", sel)
			}
			for _, it := range filtered {
				if it.Kind == k && it.Name == namePart {
					matches = append(matches, it)
				}
			}
		} else {
			for _, it := range filtered {
				if it.Name == sel {
					matches = append(matches, it)
				}
			}
		}
		if len(matches) == 0 {
			avail := make([]string, 0, len(filtered))
			for _, it := range filtered {
				avail = append(avail, it.ID())
			}
			if len(avail) == 0 {
				return nil, fmt.Errorf("nothing matching %q to push", sel)
			}
			return nil, fmt.Errorf("no changed resource matches %q (available: %s)", sel, strings.Join(avail, ", "))
		}
		if len(matches) > 1 {
			ids := make([]string, 0, len(matches))
			for _, m := range matches {
				ids = append(ids, m.ID())
			}
			return nil, fmt.Errorf("ambiguous selector %q matches %s; prefix with kind (e.g. %s)", sel, strings.Join(ids, ", "), ids[0])
		}
		m := matches[0]
		if !seen[m.ID()] {
			seen[m.ID()] = true
			out = append(out, m)
		}
	}
	return out, nil
}

func (p Pusher) copySelectedIntoWorktree(items []DiffItem) error {
	var mcpItems []DiffItem
	for _, it := range items {
		switch it.Kind {
		case "skill":
			dst := filepath.Join(p.Partition.Worktree, "skills", it.Name)
			if err := config.RemoveAll(dst); err != nil {
				return err
			}
			if err := tool.CopyDir(it.SkillDir, dst); err != nil {
				return err
			}
		case "rule":
			dst := filepath.Join(p.Partition.Worktree, "rules", it.Name+".md")
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(dst, []byte(it.Content), 0o644); err != nil {
				return err
			}
		case "mcp":
			mcpItems = append(mcpItems, it)
		}
	}
	if len(mcpItems) > 0 {
		m, err := config.LoadMCP(p.Partition.Worktree)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("load mcp/mcp.yaml: %w", err)
		}
		if m.Servers == nil {
			m.Servers = map[string]config.MCPServer{}
		}
		for _, it := range mcpItems {
			m.Servers[it.Name] = it.Server
		}
		if err := config.SaveMCP(p.Partition.Worktree, m); err != nil {
			return err
		}
	}
	return nil
}

func stripMCPSecrets(s config.MCPServer) (config.MCPServer, bool) {
	hadSecrets := len(s.Env) > 0 || len(s.Headers) > 0
	clean := config.MCPServer{
		Command: s.Command,
		URL:     s.URL,
	}
	if len(s.Args) > 0 {
		clean.Args = append([]string(nil), s.Args...)
	}
	return clean, hadSecrets
}

func mcpEqual(a, b config.MCPServer) bool {
	if a.Command != b.Command || a.URL != b.URL || len(a.Args) != len(b.Args) {
		return false
	}
	for i := range a.Args {
		if a.Args[i] != b.Args[i] {
			return false
		}
	}
	return true
}

func normalizeKind(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "skill", "skills":
		return "skill", true
	case "rule", "rules":
		return "rule", true
	case "mcp", "mcps":
		return "mcp", true
	default:
		return "", false
	}
}

func kindOrder(kind string) int {
	switch kind {
	case "skill":
		return 0
	case "rule":
		return 1
	case "mcp":
		return 2
	default:
		return 3
	}
}

func formatDiffLabel(it DiffItem) string {
	if it.Kind == "skill" {
		return it.Name
	}
	return it.Kind + ":" + it.Name
}

func summarizeItems(items []DiffItem) string {
	labels := make([]string, 0, len(items))
	for _, it := range items {
		labels = append(labels, it.ID())
	}
	return strings.Join(labels, ", ")
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

var (
	unsafeName  = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	multiDots   = regexp.MustCompile(`\.{2,}`)
	multiDashes = regexp.MustCompile(`-{2,}`)
)

// sanitize makes a string safe as a git branch name segment.
func sanitize(s string) string {
	s = unsafeName.ReplaceAllString(strings.TrimSpace(s), "-")
	s = multiDots.ReplaceAllString(s, "-")
	s = multiDashes.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-.")
	for strings.HasSuffix(strings.ToLower(s), ".lock") {
		s = strings.TrimSuffix(s, s[len(s)-5:])
		s = strings.Trim(s, "-.")
	}
	if s == "" {
		return "unknown"
	}
	return s
}
