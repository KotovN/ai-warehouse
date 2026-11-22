package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KotovN/ai-warehouse/internal/config"
)

func fixtureResources(t *testing.T) Resources {
	t.Helper()
	skillDir := filepath.Join(t.TempDir(), "go-tips")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# go tips\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return Resources{
		Skills: []Skill{{Name: "go-tips", Dir: skillDir}},
		Rules:  []Rule{{Name: "code-review", Content: "Review before commit."}},
		MCP: config.MCPFile{Servers: map[string]config.MCPServer{
			"fs": {Command: "npx", Args: []string{"-y", "server-fs"}, Env: map[string]string{"A": "b"}},
		}},
		Hooks: config.HooksFile{},
		Clis:  config.ClisFile{},
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestCursorInstallAndHooks(t *testing.T) {
	c := &Cursor{Root: t.TempDir()}
	os.MkdirAll(c.dir(), 0o755)
	if !c.Detect() {
		t.Fatal("cursor should be detected")
	}
	res := fixtureResources(t)
	if _, err := c.Install(res); err != nil {
		t.Fatal(err)
	}
	if _, err := c.InjectHooks(); err != nil {
		t.Fatal(err)
	}
	// Idempotent second run.
	if _, err := c.InjectHooks(); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(c.skills(), "go-tips", "SKILL.md")); got != "# go tips\n" {
		t.Fatalf("skill content: %q", got)
	}
	if got := read(t, filepath.Join(c.rules(), "code-review.mdc")); got != "Review before commit." {
		t.Fatalf("rule content: %q", got)
	}
	inv, err := c.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Skills) != 1 || inv.Skills[0] != "go-tips" || len(inv.Servers) != 1 || inv.Servers[0] != "fs" || !inv.HooksSet {
		t.Fatalf("inventory: %+v", inv)
	}
}

func TestCodexTOMLPreservesExisting(t *testing.T) {
	c := &Codex{Root: t.TempDir()}
	os.MkdirAll(c.dir(), 0o755)
	if err := os.WriteFile(c.tomlPath(), []byte("model = \"o4\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Install(fixtureResources(t)); err != nil {
		t.Fatal(err)
	}
	got := read(t, c.tomlPath())
	if !contains(got, "model = \"o4\"") || !contains(got, "command = \"npx\"") {
		t.Fatalf("toml lost content:\n%s", got)
	}
	block := read(t, c.agentsMD())
	if !contains(block, blockBegin) || !contains(block, "Review before commit.") {
		t.Fatalf("agents block missing:\n%s", block)
	}
}

func TestBlockReplaceAndStrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(path, []byte("my notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := replaceBlock(path, "v1"); err != nil {
		t.Fatal(err)
	}
	if err := replaceBlock(path, "v2"); err != nil {
		t.Fatal(err)
	}
	got := read(t, path)
	if !contains(got, "my notes") || !contains(got, "v2") || contains(got, "v1") {
		t.Fatalf("block replace failed:\n%s", got)
	}
	empty, err := stripBlock(path)
	if err != nil {
		t.Fatal(err)
	}
	if empty {
		t.Fatal("file still has user content, must not be deleted")
	}
	if got := read(t, path); got != "my notes\n" {
		t.Fatalf("after strip: %q", got)
	}
}

func TestVSCodeMCPFormat(t *testing.T) {
	v := &VSCode{Root: t.TempDir()}
	os.MkdirAll(filepath.Join(v.Root, ".vscode"), 0o755)
	if _, err := v.Install(fixtureResources(t)); err != nil {
		t.Fatal(err)
	}
	if got := read(t, v.mcpPath()); !contains(got, "\"servers\"") || !contains(got, "\"fs\"") {
		t.Fatalf("vscode mcp format:\n%s", got)
	}
	if _, err := v.InjectHooks(); err != nil {
		t.Fatal(err)
	}
	if got := read(t, v.instructions()); !contains(got, "Review before commit.") {
		t.Fatalf("copilot instructions:\n%s", got)
	}
}

func TestOpenCodeMCPFormat(t *testing.T) {
	o := &OpenCode{Root: t.TempDir()}
	os.MkdirAll(o.dir(), 0o755)
	if _, err := o.Install(fixtureResources(t)); err != nil {
		t.Fatal(err)
	}
	got := read(t, o.configPath())
	if !contains(got, "\"type\": \"local\"") || !contains(got, "\"command\": [") {
		t.Fatalf("opencode mcp format:\n%s", got)
	}
	inv, err := o.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Servers) != 1 || inv.Servers[0] != "fs" {
		t.Fatalf("inventory: %+v", inv)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
