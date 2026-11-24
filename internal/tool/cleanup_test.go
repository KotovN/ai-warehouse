package tool

import (
	"os"
	"path/filepath"
	"testing"
)

// TestUninstallHelpers runs a full install -> uninstall roundtrip for Cursor
// using UndoManaged, asserting user content survives.
func TestUninstallHelpers(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".cursor"), 0o755)
	c := adapterByID(t, root, "project", "cursor")
	res := fixtureResources(t)
	managed, err := c.Install(res)
	if err != nil {
		t.Fatal(err)
	}
	hooks, err := c.InjectHooks()
	if err != nil {
		t.Fatal(err)
	}
	managed = append(managed, hooks...)

	// A foreign entry next to ours must survive uninstall.
	mcpPath := filepath.Join(root, ".cursor", "mcp.json")
	if err := mergeJSON(mcpPath, func(m map[string]any) error {
		childMap(m, "mcpServers")["other"] = map[string]any{"command": "other"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	for _, e := range managed {
		if _, err := UndoManaged(e, false); err != nil {
			t.Fatal(err)
		}
	}

	// Skill dir gone, hooks file deleted (only our entry), foreign server kept.
	if _, err := os.Stat(filepath.Join(root, ".cursor", "skills", "go-tips")); !os.IsNotExist(err) {
		t.Fatal("managed skill dir should be removed")
	}
	if _, err := os.Stat(filepath.Join(root, ".cursor", "hooks.json")); !os.IsNotExist(err) {
		t.Fatal("hooks file with only our entry should be deleted")
	}
	m, err := readJSONMap(mcpPath)
	if err != nil {
		t.Fatal(err) // file kept because the foreign server survives
	}
	if got := keysOf(childMap(m, "mcpServers")); len(got) != 1 || got[0] != "other" {
		t.Fatalf("foreign server lost: %v", got)
	}
}

func TestRemoveHookEntryMatchesMovedBinary(t *testing.T) {
	hooksPath := filepath.Join(t.TempDir(), "hooks.json")
	if err := writeJSON(hooksPath, map[string]any{
		"hooks": map[string]any{
			"sessionStart": []any{
				map[string]any{"command": `"/old/Cellar/aiw/0.1.0/bin/aiw" pull --quiet`},
				map[string]any{"command": "echo keep-me"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveHookEntry(hooksPath); err != nil {
		t.Fatal(err)
	}
	got := read(t, hooksPath)
	if contains(got, "pull --quiet") || !contains(got, "keep-me") {
		t.Fatalf("unexpected hooks.json after removal:\n%s", got)
	}
}

func TestRemoveServerKeyTOML(t *testing.T) {
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
	for _, name := range []string{"fs", "search"} {
		if err := RemoveServerKey(tomlPath, name); err != nil {
			t.Fatal(err)
		}
	}
	got := read(t, tomlPath)
	if contains(got, "npx") || contains(got, "mcp_servers") {
		t.Fatalf("server not removed:\n%s", got)
	}
	if !contains(got, "o4") {
		t.Fatalf("user toml content lost:\n%s", got)
	}
}

func TestStripBlockExported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := replaceBlock(path, "team rules"); err != nil {
		t.Fatal(err)
	}
	empty, err := StripBlock(path)
	if err != nil {
		t.Fatal(err)
	}
	if !empty {
		t.Fatal("block-only file should be deleted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file should be gone")
	}
}
