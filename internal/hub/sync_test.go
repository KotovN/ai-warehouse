package hub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KotovN/ai-warehouse/internal/config"
)

// fixtureRepo creates a hub-clone-like directory with resources.
func fixtureRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	skill := filepath.Join(root, "skills", "go-tips")
	os.MkdirAll(skill, 0o755)
	os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("# go tips\n"), 0o644)
	rules := filepath.Join(root, "rules")
	os.MkdirAll(rules, 0o755)
	os.WriteFile(filepath.Join(rules, "code-review.md"), []byte("Review before commit.\n"), 0o644)
	mcp := filepath.Join(root, "mcp")
	os.MkdirAll(mcp, 0o755)
	os.WriteFile(filepath.Join(mcp, "mcp.yaml"), []byte("servers:\n  fs:\n    command: npx\n"), 0o644)
	clis := filepath.Join(root, "clis")
	os.MkdirAll(clis, 0o755)
	// "definitely-missing-cmd" is guaranteed absent on any machine.
	os.WriteFile(filepath.Join(clis, "clis.yaml"), []byte("clis:\n  nope-tool:\n    check: definitely-missing-cmd --version\n"), 0o644)
	return root
}

func TestSyncerPull(t *testing.T) {
	projRoot := t.TempDir()
	part := config.PartitionFor(projRoot)
	if err := part.Ensure(); err != nil {
		t.Fatal(err)
	}
	s := Syncer{Root: projRoot, RepoPath: fixtureRepo(t), Partition: part}

	report, err := s.Pull(false)
	if err != nil {
		t.Fatal(err)
	}
	// No tool dirs exist in a bare temp project, so everything is skipped.
	if len(report.Skipped) == 0 {
		t.Fatalf("expected skipped tools, got %+v", report)
	}
	if len(report.MissingClis) != 1 || report.MissingClis[0] != "nope-tool" {
		t.Fatalf("missing clis: %v", report.MissingClis)
	}
}

func TestSyncerPullWithCursor(t *testing.T) {
	projRoot := t.TempDir()
	part := config.PartitionFor(projRoot)
	if err := part.Ensure(); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(projRoot, ".cursor"), 0o755)
	s := Syncer{Root: projRoot, RepoPath: fixtureRepo(t), Partition: part, Tools: []string{"cursor"}}

	report, err := s.Pull(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Installed) != 1 || report.Installed[0] != "cursor" {
		t.Fatalf("installed: %v", report.Installed)
	}
	// Skill landed, hooks injected, state records managed entries.
	if _, err := os.Stat(filepath.Join(projRoot, ".cursor", "skills", "go-tips", "SKILL.md")); err != nil {
		t.Fatalf("skill not installed: %v", err)
	}
	hooksPath := filepath.Join(projRoot, ".cursor", "hooks.json")
	if _, err := os.Stat(hooksPath); err != nil {
		t.Fatalf("hooks not injected: %v", err)
	}
	st, err := config.LoadState(part.State)
	if err != nil {
		t.Fatal(err)
	}
	entries := st.Managed["cursor"]
	if len(entries) == 0 {
		t.Fatal("no managed entries recorded")
	}
	var sawFile bool
	for _, e := range entries {
		switch e.Kind {
		case "file":
			sawFile = true
		case "mcp":
			if e.Key != "fs" {
				t.Fatalf("mcp entry key: %q", e.Key)
			}
		}
	}
	if !sawFile {
		t.Fatalf("expected a file entry, got %+v", entries)
	}
	if st.Revision != "" && len(st.Revision) != 40 {
		t.Fatalf("revision: %q", st.Revision)
	}
}

func TestBuildResourcesRulesSorted(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "rules"), 0o755)
	os.WriteFile(filepath.Join(root, "rules", "zeta.md"), []byte("z"), 0o644)
	os.WriteFile(filepath.Join(root, "rules", "alpha.md"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(root, "rules", "skip.txt"), []byte("x"), 0o644)
	res, err := BuildResources(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rules) != 2 || res.Rules[0].Name != "alpha" || res.Rules[1].Name != "zeta" {
		t.Fatalf("rules: %+v", res.Rules)
	}
}

func TestCheckClisMissingAndPresent(t *testing.T) {
	// Use the go binary as a CLI that always exists, and a bogus one.
	clis := config.ClisFile{Clis: map[string]config.CliTool{
		"real":    {Check: "go version"},
		"bogus":   {Check: "definitely-missing-cmd --version"},
		"nocheck": {},
	}}
	missing := CheckClis(clis)
	if len(missing) != 1 || missing[0] != "bogus" {
		t.Fatalf("missing: %v", missing)
	}
}

func TestCheckClisCommandFields(t *testing.T) {
	// check strings are split into fields, e.g. "go version" works
	clis := config.ClisFile{Clis: map[string]config.CliTool{"go": {Check: "go version"}}}
	if m := CheckClis(clis); len(m) != 0 {
		t.Fatalf("go should be present, missing: %v", m)
	}
	if !strings.Contains("", "") {
		t.Fatal("sanity")
	}
}
