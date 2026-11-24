package doctor

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

func TestRunUninitialized(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	checks := Run(root, config.LocalConfig{}, config.PartitionFor(root))
	byName := map[string]Check{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	if byName["config"].OK {
		t.Fatal("uninitialized install should fail the config check")
	}
	if !byName["git"].OK {
		t.Fatalf("git check: %+v", byName["git"])
	}
}

func TestRunInitializedEmptyCloneDirFails(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	part := config.PartitionFor(root)
	if err := os.MkdirAll(part.Repo, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.LocalConfig{Repo: "https://github.com/org/repo", Scope: "project", ProjectRoot: root}
	checks := Run(root, cfg, part)
	names := map[string]Check{}
	for _, c := range checks {
		names[c.Name] = c
	}
	if !names["config"].OK {
		t.Fatalf("config check: %+v", names["config"])
	}
	// Clone dir exists but is empty (not a git clone) — hub clone check must fail.
	if names["hub clone"].OK {
		t.Fatal("empty non-git clone dir should not pass the hub clone check")
	}
}

func TestRunRespectsToolsFilter(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	// Both cursor and codex exist, but user only configured cursor.
	os.MkdirAll(filepath.Join(root, ".cursor"), 0o755)
	os.MkdirAll(filepath.Join(root, ".codex"), 0o755)

	part := config.PartitionFor(root)
	os.MkdirAll(filepath.Join(part.Repo, ".git"), 0o755)
	cfg := config.LocalConfig{Repo: "https://github.com/org/repo", Scope: "project", ProjectRoot: root, Tools: []string{"cursor"}}

	checks := Run(root, cfg, part)
	for _, c := range checks {
		if c.Name == "codex" || c.Name == "codex hooks" {
			t.Fatalf("excluded tool codex must not be checked, got %+v", c)
		}
	}
}

func TestJSONSerializable(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	checks := Run(root, config.LocalConfig{}, config.PartitionFor(root))
	for _, c := range checks {
		if c.Name == "" {
			t.Fatal("check name must be set")
		}
	}
}

func TestRunSkipsIgnoredClis(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	part := config.PartitionFor(root)
	os.MkdirAll(filepath.Join(part.Repo, ".git"), 0o755)
	os.MkdirAll(filepath.Join(part.Repo, "clis"), 0o755)
	os.WriteFile(filepath.Join(part.Repo, "clis", "clis.yaml"), []byte("clis:\n  nope:\n    check: definitely-missing-cmd-abc --version\n"), 0o644)

	cfg := config.LocalConfig{
		Repo:        "https://github.com/org/repo",
		Scope:       "project",
		ProjectRoot: root,
		Ignore:      []string{"cli:nope"},
	}
	checks := Run(root, cfg, part)
	for _, c := range checks {
		if c.Name == "team clis" && !c.OK {
			t.Fatalf("ignored CLI should not fail doctor: %+v", c)
		}
	}
}
