package hub

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/KotovN/ai-warehouse/internal/config"
)

func isolateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

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
	os.WriteFile(filepath.Join(clis, "clis.yaml"), []byte("clis:\n  nope-tool:\n    check: definitely-missing-cmd --version\n    url: https://example.test/nope\n    install:\n      windows: winget install nope\n      darwin: brew install nope\n      linux: apt install nope\n"), 0o644)
	return root
}

func TestSyncerPull(t *testing.T) {
	isolateHome(t)
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
	// No tool dirs exist in an isolated bare temp project, so everything is skipped.
	if len(report.Skipped) == 0 || len(report.Installed) != 0 {
		t.Fatalf("expected all tools skipped, got %+v", report)
	}
	if len(report.MissingClis) != 1 || report.MissingClis[0] != "nope-tool" {
		t.Fatalf("missing clis: %v", report.MissingClis)
	}
	if len(report.CliDetails) != 1 || report.CliDetails[0].URL != "https://example.test/nope" || report.CliDetails[0].InstallCmd == "" {
		t.Fatalf("cli details: %+v", report.CliDetails)
	}
}

func TestSyncerPullPrunesRemovedHubResources(t *testing.T) {
	isolateHome(t)
	projRoot := t.TempDir()
	part := config.PartitionFor(projRoot)
	if err := part.Ensure(); err != nil {
		t.Fatal(err)
	}
	repo := fixtureRepo(t)
	s := Syncer{Root: projRoot, RepoPath: repo, Partition: part, Tools: []string{"cursor"}}

	report, err := s.Pull(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Installed) != 1 || report.Installed[0] != "cursor" {
		t.Fatalf("installed: %v", report.Installed)
	}
	skillPath := filepath.Join(projRoot, ".cursor", "skills", "go-tips", "SKILL.md")
	rulePath := filepath.Join(projRoot, ".cursor", "rules", "code-review.mdc")
	mcpPath := filepath.Join(projRoot, ".cursor", "mcp.json")
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatalf("skill not installed: %v", err)
	}

	// Remove skill, rule, and MCP server from hub and pull again.
	os.RemoveAll(filepath.Join(repo, "skills", "go-tips"))
	os.Remove(filepath.Join(repo, "rules", "code-review.md"))
	os.Remove(filepath.Join(repo, "mcp", "mcp.yaml"))

	report2, err := s.Pull(false)
	if err != nil {
		t.Fatal(err)
	}
	if report2.Pruned != 3 {
		t.Fatalf("expected 3 pruned entries (skill, rule, mcp), got %+v", report2)
	}
	if _, err := os.Stat(filepath.Join(projRoot, ".cursor", "skills", "go-tips")); !os.IsNotExist(err) {
		t.Fatal("removed skill should have been pruned")
	}
	if _, err := os.Stat(rulePath); !os.IsNotExist(err) {
		t.Fatal("removed rule should have been pruned")
	}
	if _, err := os.Stat(mcpPath); !os.IsNotExist(err) {
		t.Fatal("empty mcp.json should have been removed after pruning last server")
	}
}

func TestBuildResourcesPropagatesBadYAML(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "mcp"), 0o755)
	os.WriteFile(filepath.Join(root, "mcp", "mcp.yaml"), []byte("servers: [broken"), 0o644)
	if _, err := BuildResources(root); err == nil {
		t.Fatal("expected error on malformed mcp.yaml")
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

func TestCheckClisMissingPresentAndWhitespace(t *testing.T) {
	clis := config.ClisFile{Clis: map[string]config.CliTool{
		"real":       {Check: "go version"},
		"bogus":      {Check: "definitely-missing-cmd --version"},
		"nocheck":    {},
		"whitespace": {Check: "   \t  "},
	}}
	missing := CheckClis(clis)
	if len(missing) != 1 || missing[0] != "bogus" {
		t.Fatalf("missing: %v", missing)
	}
}
