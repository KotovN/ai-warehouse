package hub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KotovN/ai-warehouse/internal/config"
	"github.com/KotovN/ai-warehouse/internal/provider"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func gitTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(func() { _ = config.RemoveAll(dir) })
	return dir
}

// initHub creates a local git repo seeded with a README, usable as origin.
func initHub(t *testing.T) string {
	t.Helper()
	src := gitTempDir(t)
	r, err := git.PlainInit(src, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("hub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Add("README.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit("init", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@localhost", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	return src
}

// fakeRepo is a GenericGit transport with a scripted PR API on top.
type fakeRepo struct {
	provider.GenericGit
	existingPRs []provider.PR
	opened      []string
}

func (f *fakeRepo) ListOpenPRs() ([]provider.PR, error) { return f.existingPRs, nil }

func (f *fakeRepo) OpenPR(branch, title string, reviewers []string) (string, error) {
	f.opened = append(f.opened, branch)
	return "https://github.com/org/repo/pull/" + branch, nil
}

func pushFixture(t *testing.T) (origin string, projRoot string, pusher Pusher) {
	t.Helper()
	isolateHome(t)
	// Origin hub with one skill.
	origin = initHub(t)
	skill := filepath.Join(origin, "skills", "existing")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("hub version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitWorktree(t, origin, "add existing skill")

	// A project with a new skill, an edited copy, a rule, and a local MCP server with a secret env var.
	projRoot = t.TempDir()
	os.MkdirAll(filepath.Join(projRoot, ".cursor", "skills", "new-thing"), 0o755)
	os.WriteFile(filepath.Join(projRoot, ".cursor", "skills", "new-thing", "SKILL.md"), []byte("new\n"), 0o644)
	os.MkdirAll(filepath.Join(projRoot, ".cursor", "skills", "existing"), 0o755)
	os.WriteFile(filepath.Join(projRoot, ".cursor", "skills", "existing", "SKILL.md"), []byte("local edit\n"), 0o644)
	rules := filepath.Join(projRoot, ".cursor", "rules")
	os.MkdirAll(rules, 0o755)
	os.WriteFile(filepath.Join(rules, "extra.mdc"), []byte("extra rule\n"), 0o644)
	os.WriteFile(filepath.Join(projRoot, ".cursor", "mcp.json"), []byte(`{
  "mcpServers": {
    "local-mcp": {
      "command": "npx",
      "args": ["-y", "demo-mcp"],
      "env": {"API_KEY": "super-secret"}
    }
  }
}`), 0o644)

	part := config.PartitionFor(projRoot)
	if err := part.Ensure(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.RemoveAll(part.Root) })
	pusher = Pusher{
		Root:      projRoot,
		Partition: part,
		Repo:      &fakeRepo{GenericGit: provider.GenericGit{Remote: origin}},
		Username:  "Alice Example!",
	}
	return origin, projRoot, pusher
}

// commitWorktree stages everything in a worktree and commits.
func commitWorktree(t *testing.T, path, msg string) {
	t.Helper()
	r, err := git.PlainOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	w, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Add("."); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@localhost", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
}

func branchFile(t *testing.T, origin, branch, file string) string {
	t.Helper()
	r, err := git.PlainOpen(origin)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := r.Reference(plumbing.NewBranchReferenceName(branch), false)
	if err != nil {
		t.Fatalf("branch %s not on origin: %v", branch, err)
	}
	commit, err := r.CommitObject(ref.Hash())
	if err != nil {
		t.Fatal(err)
	}
	content, err := commit.File(file)
	if err != nil {
		t.Fatalf("file %s not in branch: %v", file, err)
	}
	b, err := content.Contents()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPushDryRunAndRepeatedPushWindowsSafe(t *testing.T) {
	origin, _, p := pushFixture(t)
	report, err := p.Push(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Added) != 3 { // skill "new-thing" + rule "extra" + mcp "local-mcp"
		t.Fatalf("added: %v", report.Added)
	}
	if len(report.Edited) != 1 || report.Edited[0] != "existing" {
		t.Fatalf("edited: %v", report.Edited)
	}
	// Second push (real) must cleanly remove knowledge-wt even on Windows.
	report2, err := p.Push(false)
	if err != nil {
		t.Fatalf("second push failed: %v", err)
	}
	if content := branchFile(t, origin, report2.Branch, "mcp/mcp.yaml"); strings.Contains(content, "super-secret") || !strings.Contains(content, "demo-mcp") {
		t.Fatalf("mcp.yaml should contain server without secrets:\n%s", content)
	}
}

func TestPushSelectiveFilter(t *testing.T) {
	origin, _, p := pushFixture(t)
	report, err := p.PushWithOptions(PushOptions{
		Select: []string{"skill:new-thing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Added) != 1 || report.Added[0] != "new-thing" || len(report.Edited) != 0 {
		t.Fatalf("selective report: %+v", report)
	}
	if content := branchFile(t, origin, report.Branch, "skills/new-thing/SKILL.md"); content != "new\n" {
		t.Fatalf("pushed skill: %q", content)
	}
	// Unselected edited skill must remain untouched in pushed branch.
	if content := branchFile(t, origin, report.Branch, "skills/existing/SKILL.md"); content != "hub version\n" {
		t.Fatalf("unselected skill was modified: %q", content)
	}
}

func TestPushMultiToolEditNotShadowed(t *testing.T) {
	isolateHome(t)
	origin := initHub(t)
	skill := filepath.Join(origin, "skills", "shared")
	os.MkdirAll(skill, 0o755)
	os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("hub\n"), 0o644)
	commitWorktree(t, origin, "add shared skill")

	projRoot := t.TempDir()
	// Cursor still has unmodified copy, while Antigravity (.agents) has an edited copy.
	os.MkdirAll(filepath.Join(projRoot, ".cursor", "skills", "shared"), 0o755)
	os.WriteFile(filepath.Join(projRoot, ".cursor", "skills", "shared", "SKILL.md"), []byte("hub\n"), 0o644)
	os.MkdirAll(filepath.Join(projRoot, ".agents", "skills", "shared"), 0o755)
	os.WriteFile(filepath.Join(projRoot, ".agents", "skills", "shared", "SKILL.md"), []byte("edited in antigravity\n"), 0o644)

	items, err := DiffLocalItems(projRoot, "project", origin, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "shared" || items[0].Status != "edited" || items[0].Source != "antigravity" {
		t.Fatalf("expected edit from antigravity, got %+v", items)
	}
}

func TestPushReusesSameUserPR(t *testing.T) {
	_, _, p := pushFixture(t)
	fake := p.Repo.(*fakeRepo)
	fake.existingPRs = []provider.PR{{Number: 7, Author: "alice", Head: "aiw/Alice-Example", URL: "https://pr/7"}}
	report, err := p.Push(false)
	if err != nil {
		t.Fatal(err)
	}
	if !report.ReusedPR || report.PRURL != "https://pr/7" {
		t.Fatalf("expected PR reuse, got %+v", report)
	}
	if len(fake.opened) != 0 {
		t.Fatalf("should not open duplicate PR, opened: %v", fake.opened)
	}
}

func TestAddCliToHub(t *testing.T) {
	origin, _, p := pushFixture(t)
	report, err := p.AddCli("hex", config.CliTool{
		Check: "hex --version",
		URL:   "https://hex.pm",
		Install: config.CliInstall{
			ByOS: map[string]string{"windows": "mix local.hex --force", "darwin": "mix local.hex --force"},
		},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Added) != 1 || report.Added[0] != "cli:hex" {
		t.Fatalf("report: %+v", report)
	}
	yamlContent := branchFile(t, origin, report.Branch, "clis/clis.yaml")
	if !strings.Contains(yamlContent, "hex:") || !strings.Contains(yamlContent, "https://hex.pm") {
		t.Fatalf("clis.yaml content:\n%s", yamlContent)
	}
}

func TestPushNothingNew(t *testing.T) {
	isolateHome(t)
	origin := initHub(t)
	skill := filepath.Join(origin, "skills", "same")
	os.MkdirAll(skill, 0o755)
	os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("same\n"), 0o644)
	commitWorktree(t, origin, "add skill")

	projRoot := t.TempDir()
	os.MkdirAll(filepath.Join(projRoot, ".cursor", "skills", "same"), 0o755)
	os.WriteFile(filepath.Join(projRoot, ".cursor", "skills", "same", "SKILL.md"), []byte("same\n"), 0o644)
	part := config.PartitionFor(projRoot)
	if err := part.Ensure(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.RemoveAll(part.Root) })
	p := Pusher{
		Root:      projRoot,
		Partition: part,
		Repo:      &fakeRepo{GenericGit: provider.GenericGit{Remote: origin}},
		Username:  "alice",
	}
	report, err := p.Push(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Added) != 0 || len(report.Edited) != 0 {
		t.Fatalf("identical content should be a no-op, got %+v", report)
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"Alice Example!": "Alice-Example",
		"  bob  ":        "bob",
		"":               "unknown",
		"joe":            "joe",
		"..alice.lock.":  "alice",
		"a..b":           "a-b",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Fatalf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}
