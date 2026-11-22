package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/KotovN/ai-warehouse/internal/config"
	"github.com/KotovN/ai-warehouse/internal/tool"
	"github.com/spf13/cobra"
)

// runUninstall removes only aiw-managed resources recorded in state.json;
// user-created content (blocks, extra files) is preserved.
func runUninstall(cmd *cobra.Command, dryRun bool) error {
	part, root, err := projectPartition()
	if err != nil {
		return err
	}
	st, err := config.LoadState(part.State)
	if err != nil {
		return err
	}
	if len(st.Managed) == 0 {
		fmt.Println("nothing managed to remove")
		return nil
	}
	prefix := ""
	if dryRun {
		prefix = "would remove "
	}
	ids := make([]string, 0, len(st.Managed))
	for id := range st.Managed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	removed := 0
	for _, id := range ids {
		for _, e := range st.Managed[id] {
			done, err := undoManaged(e, dryRun)
			if err != nil {
				fmt.Printf("%s%s (%s): %v\n", prefix, e.Path, e.Kind, err)
				continue
			}
			if done {
				fmt.Printf("%s%s (%s, %s)\n", prefix, e.Path, e.Kind, id)
				removed++
			}
		}
	}
	if dryRun {
		fmt.Printf("dry run — nothing changed (%d managed entries)\n", len(st.Managed))
		return nil
	}
	st.Managed = nil
	if err := config.SaveState(part.State, st); err != nil {
		return err
	}
	pruneEmptyToolDirs(root)
	fmt.Printf("removed %d managed entries; run aiwarehouse pull to re-sync\n", removed)
	return nil
}

// pruneEmptyToolDirs removes empty subdirectories inside the known tool dirs.
func pruneEmptyToolDirs(root string) {
	for _, dir := range []string{".cursor", ".codex", ".vscode", ".opencode"} {
		base := filepath.Join(root, dir)
		var dirs []string
		filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() && path != base {
				dirs = append(dirs, path)
			}
			return nil
		})
		sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) }) // deepest first
		for _, d := range dirs {
			if os.Remove(d) == nil { // Remove fails on non-empty dirs, which is what we want
				fmt.Println("removed empty dir", d)
			}
		}
	}
}

// undoManaged removes one managed entry; reports whether it acted.
func undoManaged(e config.ManagedEntry, dryRun bool) (bool, error) {
	if _, err := os.Stat(e.Path); os.IsNotExist(err) {
		return false, nil // already gone
	}
	if dryRun {
		return true, nil
	}
	switch e.Kind {
	case "file":
		return true, os.RemoveAll(e.Path)
	case "block":
		_, err := tool.StripBlock(e.Path)
		return true, err
	case "hook":
		return true, tool.RemoveHookEntry(e.Path)
	case "mcp":
		return true, tool.RemoveServerKey(e.Path, e.Key)
	default:
		return false, fmt.Errorf("unknown kind %q", e.Kind)
	}
}
