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

func runInit(cmd *cobra.Command, args []string, scope, tools string, force, noPrompt, autoYes bool) error {
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

	// Preserve existing machine-local settings (like Ignore/Username) when re-initializing.
	prevCfg, _ := config.Load(part.Config)

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

	cfg := config.LocalConfig{
		Repo:     repoURL,
		Username: prevCfg.Username,
		Scope:    scope,
		Tools:    selectedTools,
		Ignore:   prevCfg.Ignore,
	}
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
		Ignore:    cfg.Ignore,
	}.Pull(false)
	if err != nil {
		return err
	}
	missing := missingNonIgnoredClis(report.CliDetails)
	shouldPrompt := !noPrompt && len(missing) > 0 && (autoYes || isInteractiveStdin(cmd))
	printReport(cmd.OutOrStdout(), cmd.ErrOrStderr(), report, !shouldPrompt)
	if shouldPrompt {
		if err := promptAndInstallClis(cmd, part, &cfg, missing, false, autoYes); err != nil {
			return err
		}
	}
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

func runPull(cmd *cobra.Command, dryRun, quiet, installClis, noPrompt, autoYes bool) error {
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
		Ignore:    cfg.Ignore,
	}.Pull(dryRun)
	if err != nil {
		return err
	}
	missing := missingNonIgnoredClis(report.CliDetails)
	shouldPrompt := !dryRun && !quiet && !noPrompt && len(missing) > 0 && (installClis || autoYes || isInteractiveStdin(cmd))
	if !quiet {
		if dryRun {
			fmt.Fprintln(cmd.OutOrStdout(), "dry run — no changes written")
		}
		printReport(cmd.OutOrStdout(), cmd.ErrOrStderr(), report, !shouldPrompt)
	}
	if shouldPrompt {
		if err := promptAndInstallClis(cmd, part, &cfg, missing, false, autoYes); err != nil {
			return err
		}
	}
	return nil
}

func missingNonIgnoredClis(details []hub.CliStatus) []hub.CliStatus {
	var missing []hub.CliStatus
	for _, c := range details {
		if !c.Installed && !c.Ignored {
			missing = append(missing, c)
		}
	}
	return missing
}

func printReport(out, errOut io.Writer, r hub.Report, showMissingClis bool) {
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
	if showMissingClis && len(r.MissingClis) > 0 {
		fmt.Fprintln(out, "missing CLIs:")
		for _, c := range r.CliDetails {
			if c.Installed || c.Ignored {
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
		fmt.Fprintln(out, "  run 'aiw cli install' to install missing CLIs (or 'aiw ignore add cli:<name>')")
	}
}
