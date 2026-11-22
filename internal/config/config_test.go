package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPartitionFor(t *testing.T) {
	p := PartitionFor(t.TempDir())
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	if p.Config == "" || p.State == "" || p.Repo == "" {
		t.Fatal("partition paths must be populated")
	}
	if _, err := os.Stat(p.Root); err != nil {
		t.Fatalf("root not created: %v", err)
	}
}

func TestLocalConfigRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	in := LocalConfig{Repo: "https://github.com/org/repo", Username: "alice", Scope: "project", Tools: []string{"cursor"}}
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("got %+v, want %+v", out, in)
	}
}

func TestLoadMissingIsZero(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("missing config should not error: %v", err)
	}
	if cfg.Repo != "" {
		t.Fatal("expected zero config")
	}
}

func TestLoadWarehouse(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "warehouse.yaml"), []byte("team: acme\nautoPull: true\n"), 0o644)
	w, err := LoadWarehouse(root)
	if err != nil {
		t.Fatal(err)
	}
	if w.Team != "acme" || !w.AutoPull {
		t.Fatalf("got %+v", w)
	}
}

func TestLoadMCP(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "mcp"), 0o755)
	os.WriteFile(filepath.Join(root, "mcp", "mcp.yaml"), []byte("servers:\n  fs:\n    command: npx\n"), 0o644)
	m, err := LoadMCP(root)
	if err != nil {
		t.Fatal(err)
	}
	if m.Servers["fs"].Command != "npx" {
		t.Fatalf("got %+v", m)
	}
}

func TestLoadClis(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "clis"), 0o755)
	os.WriteFile(filepath.Join(root, "clis", "clis.yaml"), []byte("clis:\n  gh:\n    check: gh --version\n    install: winget install GitHub.cli\n"), 0o644)
	c, err := LoadClis(root)
	if err != nil {
		t.Fatal(err)
	}
	if c.Clis["gh"].Check != "gh --version" || c.Clis["gh"].Install != "winget install GitHub.cli" {
		t.Fatalf("got %+v", c)
	}
}

func TestSkillDirs(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "skills", "go-tips"), 0o755)
	os.WriteFile(filepath.Join(root, "skills", "loose.md"), nil, 0o644)
	got := SkillDirs(root)
	if len(got) != 1 || got[0] != "go-tips" {
		t.Fatalf("got %v", got)
	}
}
