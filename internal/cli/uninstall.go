package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/KotovN/ai-warehouse/internal/config"
	"github.com/KotovN/ai-warehouse/internal/tool"
	"github.com/spf13/cobra"
)

// runUninstall removes only aiw-managed resources recorded in state.json;
// user-created content (blocks, extra files) is preserved.
func runUninstall(cmd *cobra.Command, dryRun bool) error {
	part, root, _, err := activeInstall()
	if err != nil {
		return err
	}
	st, err := config.LoadState(part.State)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(st.Managed) == 0 {
		fmt.Fprintln(out, "nothing managed to remove")
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
	var removedPaths []string
	remaining := map[string][]config.ManagedEntry{}
	var hadErr error
	for _, id := range ids {
		for _, e := range st.Managed[id] {
			done, err := tool.UndoManaged(e, dryRun)
			if err != nil {
				fmt.Fprintf(out, "%s%s (%s): %v\n", prefix, e.Path, e.Kind, err)
				remaining[id] = append(remaining[id], e)
				hadErr = err
				continue
			}
			if done {
				fmt.Fprintf(out, "%s%s (%s, %s)\n", prefix, e.Path, e.Kind, id)
				removed++
				removedPaths = append(removedPaths, e.Path)
			}
		}
	}
	if dryRun {
		fmt.Fprintf(out, "dry run — nothing changed (%d managed entries)\n", removed)
		return nil
	}
	if len(remaining) == 0 {
		st.Managed = nil
	} else {
		st.Managed = remaining
	}
	if err := config.SaveState(part.State, st); err != nil {
		return err
	}
	pruneEmptyParents(root, removedPaths, out)
	fmt.Fprintf(out, "removed %d managed entries; run aiw pull to re-sync\n", removed)
	return hadErr
}

// pruneEmptyParents removes empty directories created under root for removed managed files,
// stopping before root itself so user/project root directories are never touched.
func pruneEmptyParents(root string, paths []string, out interface{ Write([]byte) (int, error) }) {
	cleanRoot := filepath.Clean(root)
	seen := map[string]bool{}
	var dirs []string
	for _, p := range paths {
		for dir := filepath.Dir(filepath.Clean(p)); dir != cleanRoot && isUnder(dir, cleanRoot); dir = filepath.Dir(dir) {
			if seen[dir] {
				break
			}
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	// Deepest directories first so child removal enables parent removal.
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		// Keep top-level tool roots (direct children of root) only if non-empty;
		// os.Remove naturally fails on non-empty directories.
		if os.Remove(d) == nil {
			fmt.Fprintln(out, "removed empty dir", d)
		}
	}
}

func isUnder(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
