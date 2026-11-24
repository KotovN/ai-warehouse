package tool

import (
	"encoding/json"
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
			"fs":     {Command: "npx", Args: []string{"-y", "server-fs"}},
			"search": {URL: "https://mcp.example.com/sse"},
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

func adapterByID(t *testing.T, root, scope, id string, extraDirs ...string) Adapter {
	t.Helper()
	adapters, err := LoadAdapters(root, scope, extraDirs...)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range adapters {
		if a.ID() == id {
			return a
		}
	}
	t.Fatalf("adapter %q not found", id)
	return nil
}

func TestCursorInstallNoNullJSONAndHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".cursor"), 0o755)
	c := adapterByID(t, root, "project", "cursor")
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
	if got := read(t, filepath.Join(root, ".cursor", "skills", "go-tips", "SKILL.md")); got != "# go tips\n" {
		t.Fatalf("skill content: %q", got)
	}
	if got := read(t, filepath.Join(root, ".cursor", "rules", "code-review.mdc")); got != "Review before commit." {
		t.Fatalf("rule content: %q", got)
	}
	mcpJSON := read(t, filepath.Join(root, ".cursor", "mcp.json"))
	if strings.Contains(mcpJSON, "null") {
		t.Fatalf("mcp.json must not contain null fields:\n%s", mcpJSON)
	}
	inv, err := c.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.Skills) != 1 || inv.Skills[0] != "go-tips" || len(inv.Rules) != 1 || inv.Rules[0] != "code-review" || len(inv.Servers) != 2 || !inv.HooksSet {
		t.Fatalf("inventory: %+v", inv)
	}
}

func TestCodexTOMLPreservesExisting(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".codex"), 0o755)
	tomlPath := filepath.Join(root, ".codex", "config.toml")
	if err := os.WriteFile(tomlPath, []byte("model = \"o4\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := adapterByID(t, root, "project", "codex")
	if _, err := c.Install(fixtureResources(t)); err != nil {
		t.Fatal(err)
	}
	got := read(t, tomlPath)
	if !contains(got, "model = \"o4\"") || !contains(got, "command = \"npx\"") {
		t.Fatalf("toml lost content:\n%s", got)
	}
	block := read(t, filepath.Join(root, "AGENTS.md"))
	if !contains(block, blockBegin) || !contains(block, "Review before commit.") {
		t.Fatalf("agents block missing:\n%s", block)
	}
}

func TestBlockReplaceAndStripCRLF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(path, []byte("my notes\r\n"), 0o644); err != nil {
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
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".vscode"), 0o755)
	v := adapterByID(t, root, "project", "vscode")
	if v.SupportsHooks() {
		t.Fatal("vscode should not support hooks")
	}
	if _, err := v.Install(fixtureResources(t)); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, ".vscode", "mcp.json"))
	if !contains(got, "\"servers\"") || !contains(got, "\"fs\"") || contains(got, "null") {
		t.Fatalf("vscode mcp format:\n%s", got)
	}
	if got := read(t, filepath.Join(root, ".github", "copilot-instructions.md")); !contains(got, "Review before commit.") {
		t.Fatalf("copilot instructions:\n%s", got)
	}
}

func TestOpenCodeMCPFormatAndUserScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".opencode"), 0o755)
	o := adapterByID(t, root, "project", "opencode")
	if _, err := o.Install(fixtureResources(t)); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(root, "opencode.json"))
	if !contains(got, "\"type\": \"local\"") || !contains(got, "\"command\": [") || contains(got, "null") {
		t.Fatalf("opencode mcp format:\n%s", got)
	}
	assets, err := o.ReadLocal()
	if err != nil {
		t.Fatal(err)
	}
	if assets.MCP["fs"].Command != "npx" || len(assets.MCP["fs"].Args) != 2 {
		t.Fatalf("ReadLocal MCP roundtrip: %+v", assets.MCP["fs"])
	}

	// User scope writes to ~/.config/opencode/opencode.json
	ou := adapterByID(t, home, "user", "opencode")
	if _, err := ou.Install(fixtureResources(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "opencode", "opencode.json")); err != nil {
		t.Fatalf("user-scope opencode.json missing: %v", err)
	}
}

func TestClaudeNestedHooksAndAntigravity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".claude"), 0o755)
	os.MkdirAll(filepath.Join(root, ".agents"), 0o755)

	cl := adapterByID(t, root, "project", "claude")
	if _, err := cl.Install(fixtureResources(t)); err != nil {
		t.Fatal(err)
	}
	entries, err := cl.InjectHooks()
	if err != nil {
		t.Fatal(err)
	}
	inv, err := cl.Inventory()
	if err != nil {
		t.Fatal(err)
	}
	if !inv.HooksSet || len(inv.Skills) != 1 || len(inv.Servers) != 2 {
		t.Fatalf("claude inventory: %+v", inv)
	}
	// Verify nested hook shape in .claude/settings.json
	var settings map[string]any
	if err := json.Unmarshal([]byte(read(t, filepath.Join(root, ".claude", "settings.json"))), &settings); err != nil {
		t.Fatal(err)
	}
	if hooks, ok := settings["hooks"].(map[string]any); !ok || hooks["SessionStart"] == nil {
		t.Fatalf("claude hooks missing SessionStart: %+v", settings)
	}
	// Undo removes nested hook cleanly
	if _, err := UndoManaged(entries[0], false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatal("empty claude settings.json should be removed on hook undo")
	}

	ag := adapterByID(t, root, "project", "antigravity")
	if _, err := ag.Install(fixtureResources(t)); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(root, ".agents", "rules", "code-review.md")); got != "Review before commit." {
		t.Fatalf("antigravity rule: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, ".agents", "mcp_config.json")); err != nil {
		t.Fatalf("antigravity mcp_config.json missing: %v", err)
	}
}

func TestCustomToolSpecFromDirectory(t *testing.T) {
	specsDir := t.TempDir()
	customYAML := `id: windsurf
detect:
  - .windsurf
project:
  skills: .windsurf/skills
  rules:
    mode: dir
    path: .windsurf/rules
    ext: .md
  mcp:
    path: .windsurf/mcp.json
    format: json-standard
    key: mcpServers
`
	if err := os.WriteFile(filepath.Join(specsDir, "windsurf.yaml"), []byte(customYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".windsurf"), 0o755)
	ws := adapterByID(t, root, "project", "windsurf", specsDir)
	if !ws.Detect() {
		t.Fatal("custom windsurf adapter should detect .windsurf")
	}
	if _, err := ws.Install(fixtureResources(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".windsurf", "skills", "go-tips", "SKILL.md")); err != nil {
		t.Fatalf("windsurf skill missing: %v", err)
	}
}

func TestSkillInstallPrunesStaleFilesInsideSkillDir(t *testing.T) {
	root := t.TempDir()
	staleFile := filepath.Join(root, ".cursor", "skills", "go-tips", "old.md")
	os.MkdirAll(filepath.Dir(staleFile), 0o755)
	os.WriteFile(staleFile, []byte("stale"), 0o644)

	c := adapterByID(t, root, "project", "cursor")
	if _, err := c.Install(fixtureResources(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Fatal("stale file inside skill dir should be removed on re-install")
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
