package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/KotovN/ai-warehouse/internal/config"
	"github.com/KotovN/ai-warehouse/internal/hub"
	"github.com/KotovN/ai-warehouse/internal/tool"
	"github.com/spf13/cobra"
)

type uninstallFlags struct {
	dryRun    bool
	addIgnore bool
	autoYes   bool
}

// runUninstall removes all aiw-managed resources when no args are given, or
// specific skill:<n>, rule:<n>, mcp:<n>, cli:<n> items when args are provided.
func runUninstall(cmd *cobra.Command, args []string, flags uninstallFlags) error {
	part, root, cfg, err := activeInstall()
	if err != nil {
		return err
	}
	if len(args) == 0 {
		if flags.addIgnore {
			return fmt.Errorf("--ignore requires specific resource selectors (e.g. aiw uninstall skill:hello --ignore)")
		}
		return runUninstallAll(cmd, part, root, flags.dryRun)
	}
	return runUninstallSelected(cmd, part, root, cfg, args, flags)
}

func runUninstallAll(cmd *cobra.Command, part config.Partition, root string, dryRun bool) error {
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

func runUninstallSelected(cmd *cobra.Command, part config.Partition, root string, cfg config.LocalConfig, args []string, flags uninstallFlags) error {
	st, err := config.LoadState(part.State)
	if err != nil {
		return err
	}
	res, err := hub.BuildResources(part.Repo)
	if err != nil {
		return err
	}
	canonical, err := resolveUninstallSelectors(args, st, res)
	if err != nil {
		return err
	}
	var managedTargets, cliTargets []string
	for _, sel := range canonical {
		if name, ok := strings.CutPrefix(sel, "cli:"); ok {
			cliTargets = append(cliTargets, name)
		} else {
			managedTargets = append(managedTargets, sel)
		}
	}

	out := cmd.OutOrStdout()
	totalRemoved := 0
	if len(managedTargets) > 0 {
		n, err := uninstallManagedTargets(cmd, part, root, cfg, managedTargets, flags.dryRun)
		if err != nil {
			return err
		}
		totalRemoved += n
	}

	if len(cliTargets) > 0 {
		if err := uninstallCliTargets(cmd, res.Clis, cfg.Ignore, cliTargets, flags); err != nil {
			return err
		}
	}

	if flags.addIgnore {
		if flags.dryRun {
			fmt.Fprintln(out, "would add to ignore:", strings.Join(canonical, ", "))
		} else {
			updated, added, err := config.AddIgnore(cfg.Ignore, canonical...)
			if err != nil {
				return err
			}
			cfg.Ignore = updated
			if err := config.Save(part.Config, cfg); err != nil {
				return err
			}
			if len(added) > 0 {
				fmt.Fprintln(out, "added to ignore:", strings.Join(added, ", "))
			}
		}
	} else if !flags.dryRun && totalRemoved > 0 {
		fmt.Fprintf(out, "removed %d managed entry(ies) (note: next 'aiw pull' will restore them unless you pass --ignore)\n", totalRemoved)
	}
	return nil
}

func resolveUninstallSelectors(args []string, st config.State, res tool.Resources) ([]string, error) {
	// Collect all known canonical selectors from state + hub clis for bare-name resolution.
	knownSet := map[string]bool{}
	for _, entries := range st.Managed {
		for _, e := range entries {
			if r := e.CanonicalResource(); r != "" && strings.Contains(r, ":") {
				knownSet[r] = true
			}
		}
	}
	for _, r := range res.Rules {
		knownSet["rule:"+r.Name] = true
	}
	for name := range res.Clis.Clis {
		knownSet["cli:"+name] = true
	}

	var out []string
	seen := map[string]bool{}
	for _, raw := range args {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		var norm string
		if strings.Contains(raw, ":") {
			var err error
			norm, err = config.NormalizeSelector(raw)
			if err != nil {
				return nil, err
			}
		} else {
			var matches []string
			for k := range knownSet {
				if _, name, ok := strings.Cut(k, ":"); ok && name == raw {
					matches = append(matches, k)
				}
			}
			sort.Strings(matches)
			switch len(matches) {
			case 0:
				return nil, fmt.Errorf("no installed resource or CLI matches %q (use kind:name, e.g. skill:%s, mcp:%s, cli:%s)", raw, raw, raw, raw)
			case 1:
				norm = matches[0]
			default:
				return nil, fmt.Errorf("ambiguous selector %q matches %s; prefix with kind (e.g. %s)", raw, strings.Join(matches, ", "), matches[0])
			}
		}
		if !seen[norm] {
			seen[norm] = true
			out = append(out, norm)
		}
	}
	return out, nil
}

// uninstallManagedTargets removes only managed entries matching canonical
// selectors (skill:<n>, rule:<n>, mcp:<n>) and re-renders block rules if needed.
func uninstallManagedTargets(cmd *cobra.Command, part config.Partition, root string, cfg config.LocalConfig, targets []string, dryRun bool) (int, error) {
	st, err := config.LoadState(part.State)
	if err != nil {
		return 0, err
	}
	targetSet := map[string]bool{}
	var ruleTargets []string
	for _, t := range targets {
		targetSet[t] = true
		if strings.HasPrefix(t, "rule:") {
			ruleTargets = append(ruleTargets, t)
		}
	}

	out := cmd.OutOrStdout()
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
	var hadErr error
	for _, id := range ids {
		var kept []config.ManagedEntry
		for _, e := range st.Managed[id] {
			if !targetSet[e.CanonicalResource()] {
				kept = append(kept, e)
				continue
			}
			done, err := tool.UndoManaged(e, dryRun)
			if err != nil {
				fmt.Fprintf(out, "%s%s (%s): %v\n", prefix, e.Path, e.Kind, err)
				kept = append(kept, e)
				hadErr = err
				continue
			}
			if done {
				fmt.Fprintf(out, "%s%s (%s, %s)\n", prefix, e.Path, e.CanonicalResource(), id)
				removed++
				removedPaths = append(removedPaths, e.Path)
			}
		}
		if !dryRun {
			if len(kept) == 0 {
				delete(st.Managed, id)
			} else {
				st.Managed[id] = kept
			}
		}
	}

	// For block-mode rules (AGENTS.md, CLAUDE.md, copilot-instructions.md),
	// re-render the managed block without the removed/ignored rule(s).
	if len(ruleTargets) > 0 {
		exclude := append([]string(nil), cfg.Ignore...)
		exclude = append(exclude, ruleTargets...)
		n, err := hub.ReapplyRuleBlocks(part.Repo, exclude, &st, dryRun)
		if err != nil {
			return removed, err
		}
		if n > 0 {
			if dryRun {
				fmt.Fprintf(out, "would update %d managed rules block(s)\n", n)
			} else {
				fmt.Fprintf(out, "updated %d managed rules block(s)\n", n)
			}
			removed += n
		}
	}

	if dryRun {
		return removed, hadErr
	}
	if len(st.Managed) == 0 {
		st.Managed = nil
	}
	if err := config.SaveState(part.State, st); err != nil {
		return removed, err
	}
	pruneEmptyParents(root, removedPaths, out)
	return removed, hadErr
}

func uninstallCliTargets(cmd *cobra.Command, clis config.ClisFile, ignore []string, names []string, flags uninstallFlags) error {
	all := hub.InspectClisWithIgnore(clis, runtime.GOOS, ignore)
	byName := map[string]hub.CliStatus{}
	for _, c := range all {
		byName[c.Name] = c
	}
	reader := bufio.NewReader(cmd.InOrStdin())
	out := cmd.OutOrStdout()
	for _, name := range names {
		c, ok := byName[name]
		if !ok {
			if flags.addIgnore {
				fmt.Fprintf(out, "cli:%s not found in clis/clis.yaml (will still add to ignore)\n", name)
				continue
			}
			return fmt.Errorf("unknown CLI %q in clis/clis.yaml", name)
		}
		if c.UninstallCmd == "" {
			fmt.Fprintf(out, "%s: no uninstall command configured for %s in clis/clis.yaml\n", c.Name, runtime.GOOS)
			continue
		}
		if flags.dryRun {
			fmt.Fprintf(out, "[dry-run] uninstall %s: %s\n", c.Name, c.UninstallCmd)
			continue
		}
		if !flags.autoYes {
			fmt.Fprintf(out, "Uninstall %s by running: %s\nProceed? [y/N]: ", c.Name, c.UninstallCmd)
			line, _ := reader.ReadString('\n')
			ans := strings.ToLower(strings.TrimSpace(line))
			if ans != "y" && ans != "yes" {
				fmt.Fprintf(out, "skipped uninstalling %s\n", c.Name)
				continue
			}
		}
		fmt.Fprintf(out, "Uninstalling %s: %s\n", c.Name, c.UninstallCmd)
		if err := hub.UninstallCli(c, out, cmd.ErrOrStderr()); err != nil {
			return err
		}
		fmt.Fprintf(out, "uninstalled %s\n", c.Name)
	}
	return nil
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
