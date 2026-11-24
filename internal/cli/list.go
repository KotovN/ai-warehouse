package cli

import (
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

// runList prints an inventory across tools, optionally filtered by resource type.
func runList(cmd *cobra.Command, args []string) error {
	filter := ""
	if len(args) > 0 {
		filter = args[0]
		if !config.IsResourceType(filter) {
			return fmt.Errorf("unknown resource type %q, want one of: %v", filter, config.ResourceTypes)
		}
	}
	part, root, cfg, err := activeInstall()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	adapters, err := tool.LoadAdapters(root, cfg.Scope, tool.DefaultExtraDirs(part.Repo)...)
	if err != nil {
		return err
	}
	for _, a := range adapters {
		if !isToolSelected(cfg.Tools, a.ID()) {
			continue
		}
		if len(cfg.Tools) == 0 && !a.Detect() {
			continue
		}
		inv, err := a.Inventory()
		if err != nil {
			return fmt.Errorf("%s inventory: %w", a.ID(), err)
		}
		if filter == "" || filter == "skills" {
			fmt.Fprintf(out, "%-12s skills: %v\n", a.ID(), inv.Skills)
		}
		if filter == "" || filter == "rules" {
			fmt.Fprintf(out, "%-12s rules: %v\n", a.ID(), inv.Rules)
		}
		if filter == "" || filter == "mcp" {
			fmt.Fprintf(out, "%-12s mcp servers: %v\n", a.ID(), inv.Servers)
		}
		if filter == "" || filter == "hooks" {
			fmt.Fprintf(out, "%-12s session-start hook: %v\n", a.ID(), inv.HooksSet)
		}
	}
	res, err := hub.BuildResources(part.Repo)
	if err != nil {
		return err
	}
	if filter == "" || filter == "clis" {
		statuses := hub.InspectClisWithIgnore(res.Clis, runtime.GOOS, cfg.Ignore)
		var missing []string
		ignoredCount := 0
		for _, s := range statuses {
			if s.Ignored {
				ignoredCount++
				continue
			}
			if !s.Installed {
				missing = append(missing, s.Name)
			}
		}
		if ignoredCount > 0 {
			fmt.Fprintf(out, "%-12s missing clis: %v (%d total, %d ignored)\n", "cli", missing, len(statuses), ignoredCount)
		} else {
			fmt.Fprintf(out, "%-12s missing clis: %v (%d total)\n", "cli", missing, len(statuses))
		}
	}
	if filter == "" || filter == "docs" {
		fmt.Fprintf(out, "%-12s hub docs: %v\n", "docs", listDocs(part.Repo))
	}
	return nil
}

func isToolSelected(configured []string, id string) bool {
	if len(configured) == 0 {
		return true
	}
	for _, t := range configured {
		if strings.EqualFold(strings.TrimSpace(t), id) {
			return true
		}
	}
	return false
}

func listDocs(repoPath string) []string {
	entries, err := os.ReadDir(filepath.Join(repoPath, "docs"))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}
