package hub

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KotovN/ai-warehouse/internal/config"
	"github.com/KotovN/ai-warehouse/internal/provider"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// initHub creates a local git repo seeded with a README, usable as origin.
func initHub(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
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

	// A project with a new skill and an edited copy of the existing one.
	projRoot = t.TempDir()
	os.MkdirAll(filepath.Join(projRoot, ".cursor", "skills", "new-thing"), 0o755)
	os.WriteFile(filepath.Join(projRoot, ".cursor", "skills", "new-thing", "SKILL.md"), []byte("new\n"), 0o644)
	os.MkdirAll(filepath.Join(projRoot, ".cursor", "skills", "existing"), 0o755)
	os.WriteFile(filepath.Join(projRoot, ".cursor", "skills", "existing", "SKILL.md"), []byte("local edit\n"), 0o644)
	rules := filepath.Join(projRoot, ".cursor", "rules")
	os.MkdirAll(rules, 0o755)
	os.WriteFile(filepath.Join(rules, "extra.mdc"), []byte("extra rule\n"), 0o644)

	part := config.PartitionFor(projRoot)
	if err := part.Ensure(); err != nil {
		t.Fatal(err)
	}
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

func TestPushDryRun(t *testing.T) {
	_, _, p := pushFixture(t)
	report, err := p.Push(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Added) != 2 { // skill "new-thing" + rule "extra"
		t.Fatalf("added: %v", report.Added)
	}
	if len(report.Edited) != 1 || report.Edited[0] != "existing" {
		t.Fatalf("edited: %v", report.Edited)
	}
}

func TestPushCreatesBranchAndPR(t *testing.T) {
	origin, _, p := pushFixture(t) // (origin, projRoot, pusher)
	fake := p.Repo.(*fakeRepo)
	report, err := p.Push(false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Branch != "aiw/Alice-Example" {
		t.Fatalf("branch: %q", report.Branch)
	}
	if report.PRURL == "" || report.ReusedPR {
		t.Fatalf("expected fresh PR, got %+v", report)
	}
	if len(fake.opened) != 1 || fake.opened[0] != report.Branch {
		t.Fatalf("opened: %v", fake.opened)
	}
	if content := branchFile(t, origin, report.Branch, "skills/new-thing/SKILL.md"); content != "new\n" {
		t.Fatalf("pushed skill content: %q", content)
	}
	if content := branchFile(t, origin, report.Branch, "rules/extra.md"); content != "extra rule\n" {
		t.Fatalf("pushed rule content: %q", content)
	}
	if content := branchFile(t, origin, report.Branch, "skills/existing/SKILL.md"); content != "local edit\n" {
		t.Fatalf("edited skill content: %q", content)
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

func TestPushNothingNew(t *testing.T) {
	// Hub already has the skill the project carries, with identical content.
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
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Fatalf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}
