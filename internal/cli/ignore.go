package cli

import (
	"fmt"
	"strings"

	"github.com/KotovN/ai-warehouse/internal/config"
	"github.com/spf13/cobra"
)

func runIgnoreList(cmd *cobra.Command, _ []string) error {
	_, _, cfg, err := activeInstall()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(cfg.Ignore) == 0 {
		fmt.Fprintln(out, "no ignored resources")
		return nil
	}
	for _, item := range cfg.Ignore {
		fmt.Fprintln(out, item)
	}
	return nil
}

func runIgnoreAdd(cmd *cobra.Command, args []string) error {
	part, root, cfg, err := activeInstall()
	if err != nil {
		return err
	}
	updated, added, err := config.AddIgnore(cfg.Ignore, args...)
	if err != nil {
		return err
	}
	cfg.Ignore = updated
	if err := config.Save(part.Config, cfg); err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(added) == 0 {
		fmt.Fprintln(out, "already ignored:", strings.Join(args, ", "))
	} else {
		fmt.Fprintln(out, "ignored:", strings.Join(added, ", "))
	}
	// Immediately prune any matching managed local files/keys/blocks offline.
	var managedTargets []string
	for _, raw := range args {
		norm, _ := config.NormalizeSelector(raw)
		if !strings.HasPrefix(norm, "cli:") {
			managedTargets = append(managedTargets, norm)
		}
	}
	if len(managedTargets) > 0 {
		pruned, err := uninstallManagedTargets(cmd, part, root, cfg, managedTargets, false)
		if err != nil {
			return err
		}
		if pruned > 0 {
			fmt.Fprintf(out, "pruned %d local managed entry(ies)\n", pruned)
		}
	}
	return nil
}

func runIgnoreRemove(cmd *cobra.Command, args []string) error {
	part, _, cfg, err := activeInstall()
	if err != nil {
		return err
	}
	updated, removed, err := config.RemoveIgnore(cfg.Ignore, args...)
	if err != nil {
		return err
	}
	cfg.Ignore = updated
	if err := config.Save(part.Config, cfg); err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(removed) == 0 {
		fmt.Fprintln(out, "none of those items were ignored")
		return nil
	}
	fmt.Fprintln(out, "un-ignored:", strings.Join(removed, ", "))
	fmt.Fprintln(out, "run 'aiw pull' to re-sync un-ignored resources")
	return nil
}
