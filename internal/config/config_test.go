package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func isolateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func TestPartitionFor(t *testing.T) {
	isolateHome(t)
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

func TestProjectSlugCleanAndRoot(t *testing.T) {
	a := ProjectSlug(filepath.Join("tmp", "myproj"))
	b := ProjectSlug(filepath.Join("tmp", "myproj") + string(filepath.Separator))
	if a != b {
		t.Fatalf("trailing separator changed slug: %q vs %q", a, b)
	}
	rootSlug := ProjectSlug(string(filepath.Separator))
	if strings.ContainsAny(rootSlug, `/\:`) || !strings.HasPrefix(rootSlug, "root-") {
		t.Fatalf("unexpected root slug: %q", rootSlug)
	}
}

func TestRemoveAllReadOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	objDir := filepath.Join(dir, ".git", "objects", "aa")
	if err := os.MkdirAll(objDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(objDir, "bbcc")
	if err := os.WriteFile(f, []byte("obj"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAll(dir); err != nil {
		t.Fatalf("RemoveAll failed on read-only file: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("directory should be deleted")
	}
}

func TestStateJSONAndLegacyYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	in := State{
		LastPull: "2026-11-23T00:00:00Z",
		Revision: "abc123",
		Managed: map[string][]ManagedEntry{
			"cursor": {{Path: "/tmp/a", Kind: "mcp", Key: "fs", Section: "mcpServers"}},
		},
	}
	if err := SaveState(path, in); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) {
		t.Fatalf("state.json must be valid JSON, got:\n%s", raw)
	}
	out, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(out, in) {
		t.Fatalf("got %+v, want %+v", out, in)
	}

	// Legacy v1.0.0 YAML state.json still loads cleanly.
	legacy := filepath.Join(t.TempDir(), "legacy-state.json")
	if err := os.WriteFile(legacy, []byte("lastPull: 2026-01-01T00:00:00Z\nrevision: deadbeef\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gotLegacy, err := LoadState(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if gotLegacy.Revision != "deadbeef" {
		t.Fatalf("legacy load: %+v", gotLegacy)
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
	os.WriteFile(filepath.Join(root, "warehouse.yaml"), []byte("team: acme\nautoPull: false\n"), 0o644)
	w, err := LoadWarehouse(root)
	if err != nil {
		t.Fatal(err)
	}
	if w.Team != "acme" || w.AutoPullEnabled() {
		t.Fatalf("got %+v", w)
	}
	// Default (omitted) autoPull is true.
	if !(Warehouse{Team: "acme"}).AutoPullEnabled() {
		t.Fatal("omitted autoPull should default to true")
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

func TestLoadClisStringAndPerOS(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "clis"), 0o755)
	content := `clis:
  gh:
    check: gh --version
    install: winget install GitHub.cli
  hex:
    check: hex --version
    url: https://hex.pm
    install:
      windows: mix local.hex --force
      macos: brew install hex
      linux: sh install.sh
`
	os.WriteFile(filepath.Join(root, "clis", "clis.yaml"), []byte(content), 0o644)
	c, err := LoadClis(root)
	if err != nil {
		t.Fatal(err)
	}
	if c.Clis["gh"].Install.ForOS("windows") != "winget install GitHub.cli" {
		t.Fatalf("gh install: %+v", c.Clis["gh"])
	}
	if c.Clis["hex"].URL != "https://hex.pm" || c.Clis["hex"].Install.ForOS("darwin") != "brew install hex" || c.Clis["hex"].Install.ForOS("windows") != "mix local.hex --force" {
		t.Fatalf("hex cli: %+v", c.Clis["hex"])
	}
	// Round-trip SaveClis
	outRoot := t.TempDir()
	if err := SaveClis(outRoot, c); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadClis(outRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded, c) {
		t.Fatalf("roundtrip mismatch:\ngot  %+v\nwant %+v", reloaded, c)
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
