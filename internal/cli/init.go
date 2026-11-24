package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/KotovN/ai-warehouse/internal/config"
	"github.com/KotovN/ai-warehouse/internal/hub"
	"github.com/KotovN/ai-warehouse/internal/provider"
	"github.com/KotovN/ai-warehouse/internal/tool"
	"github.com/spf13/cobra"
)

func runInit(cmd *cobra.Command, args []string, scope, tools string, force bool) error {
	if len(args) == 0 {
		return errors.New("usage: aiw init <repo-url>")
	}
	if scope != "project" && scope != "user" {
		return fmt.Errorf("invalid scope %q: want project|user", scope)
	}
	root, err := initRoot(scope)
	if err != nil {
		return err
	}
	part := config.PartitionFor(root)
	if err := part.Ensure(); err != nil {
		return err
	}

	repoURL := strings.TrimSpace(args[0])
	needClone := false
	if _, err := os.Stat(part.Repo); os.IsNotExist(err) {
		needClone = true
	} else if force {
		needClone = true
	} else if existing := provider.RemoteURL(part.Repo); existing != "" && existing != repoURL {
		needClone = true
	}
	if needClone {
		if err := config.RemoveAll(part.Repo); err != nil {
			return fmt.Errorf("clean existing clone: %w", err)
		}
		repo, err := provider.New(repoURL)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Cloning", repoURL, "->", part.Repo)
		if err := repo.Clone(part.Repo); err != nil {
			return fmt.Errorf("clone: %w", err)
		}
	}

	selectedTools := splitCSV(tools)
	if len(selectedTools) > 0 {
		if err := validateTools(root, scope, part.Repo, selectedTools); err != nil {
			return err
		}
	}

	cfg := config.LocalConfig{Repo: repoURL, Scope: scope, Tools: selectedTools}
	if scope == "project" {
		cfg.ProjectRoot = root
	}
	if err := config.Save(part.Config, cfg); err != nil {
		return err
	}
	report, err := hub.Syncer{
		Root:      root,
		Scope:     scope,
		RepoPath:  part.Repo,
		Partition: part,
		Tools:     cfg.Tools,
	}.Pull(false)
	if err != nil {
		return err
	}
	printReport(cmd.OutOrStdout(), cmd.ErrOrStderr(), report)
	return nil
}

func validateTools(root, scope, repoPath string, requested []string) error {
	adapters, err := tool.LoadAdapters(root, scope, tool.DefaultExtraDirs(repoPath)...)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, a := range adapters {
		known[a.ID()] = true
	}
	var unknown []string
	for _, t := range requested {
		if !known[strings.ToLower(t)] {
			unknown = append(unknown, t)
		}
	}
	if len(unknown) > 0 {
		return fmt.Errorf("unknown tool(s): %s (available: %s)", strings.Join(unknown, ", "), strings.Join(tool.ValidIDs(adapters), ", "))
	}
	return nil
}

func runPull(cmd *cobra.Command, dryRun, quiet, installClis, autoYes bool) error {
	part, root, cfg, err := activeInstall()
	if err != nil {
		return err
	}
	report, err := hub.Syncer{
		Root:      root,
		Scope:     cfg.Scope,
		RepoPath:  part.Repo,
		Partition: part,
		Tools:     cfg.Tools,
	}.Pull(dryRun)
	if err != nil {
		return err
	}
	if !quiet {
		if dryRun {
			fmt.Fprintln(cmd.OutOrStdout(), "dry run — no changes written")
		}
		printReport(cmd.OutOrStdout(), cmd.ErrOrStderr(), report)
	}
	if installClis && !dryRun && len(report.MissingClis) > 0 {
		var missing []hub.CliStatus
		for _, c := range report.CliDetails {
			if !c.Installed {
				missing = append(missing, c)
			}
		}
		if err := installCliList(cmd, missing, false, autoYes); err != nil {
			return err
		}
	}
	return nil
}

func printReport(out, errOut io.Writer, r hub.Report) {
	for _, w := range r.Warnings {
		fmt.Fprintln(errOut, "warning:", w)
	}
	fmt.Fprintln(out, "installed:", strings.Join(r.Installed, ", "))
	if r.Pruned > 0 {
		fmt.Fprintf(out, "pruned: %d removed hub resource(s)\n", r.Pruned)
	}
	if len(r.Skipped) > 0 {
		fmt.Fprintln(out, "skipped (tool not detected):", strings.Join(r.Skipped, ", "))
	}
	if len(r.MissingClis) > 0 {
		fmt.Fprintln(out, "missing CLIs:")
		for _, c := range r.CliDetails {
			if c.Installed {
				continue
			}
			line := "  - " + c.Name
			if c.InstallCmd != "" {
				line += ": " + c.InstallCmd
			}
			if c.URL != "" {
				line += " (" + c.URL + ")"
			}
			fmt.Fprintln(out, line)
		}
		fmt.Fprintln(out, "  run 'aiw cli install' to install missing CLIs")
	}
}
