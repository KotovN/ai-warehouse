package tool

import (
	"os"
	"path/filepath"
	"testing"
)

// TestUninstallHelpers runs a full install -> uninstall roundtrip for Cursor
// using the cleanup helpers, asserting user content survives.
func TestUninstallHelpers(t *testing.T) {
	c := &Cursor{Root: t.TempDir()}
	os.MkdirAll(c.dir(), 0o755)
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
	mergeJSON(c.mcpPath(), func(m map[string]any) error {
		childMap(m, "mcpServers")["other"] = map[string]any{"command": "other"}
		return nil
	})

	for _, e := range managed {
		switch e.Kind {
		case "file":
			if err := os.RemoveAll(e.Path); err != nil {
				t.Fatal(err)
			}
		case "hook":
			if err := RemoveHookEntry(e.Path); err != nil {
				t.Fatal(err)
			}
		case "mcp":
			if err := RemoveServerKey(e.Path, e.Key); err != nil {
				t.Fatal(err)
			}
		}
	}

	// Skill dir gone, hooks file deleted (only our entry), foreign server kept.
	if _, err := os.Stat(filepath.Join(c.skills(), "go-tips")); !os.IsNotExist(err) {
		t.Fatal("managed skill dir should be removed")
	}
	if _, err := os.Stat(c.hooksPath()); !os.IsNotExist(err) {
		t.Fatal("hooks file with only our entry should be deleted")
	}
	m, err := readJSONMap(c.mcpPath())
	if err != nil {
		t.Fatal(err) // file kept because the foreign server survives
	}
	if got := keysOf(childMap(m, "mcpServers")); len(got) != 1 || got[0] != "other" {
		t.Fatalf("foreign server lost: %v", got)
	}
}

func TestRemoveServerKeyTOML(t *testing.T) {
	c := &Codex{Root: t.TempDir()}
	os.MkdirAll(c.dir(), 0o755)
	if err := os.WriteFile(c.tomlPath(), []byte("model = \"o4\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Install(fixtureResources(t)); err != nil {
		t.Fatal(err)
	}
	if err := RemoveServerKey(c.tomlPath(), "fs"); err != nil {
		t.Fatal(err)
	}
	got := read(t, c.tomlPath())
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
