package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/KotovN/ai-warehouse/internal/config"
	"github.com/KotovN/ai-warehouse/internal/hub"
	"github.com/KotovN/ai-warehouse/internal/provider"
	"github.com/spf13/cobra"
)

// initRoot returns the install root for a scope: home for user, cwd for project.
func initRoot(scope string) string {
	if scope == "user" {
		h, _ := os.UserHomeDir()
		return h
	}
	wd, _ := os.Getwd()
	return wd
}

func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func runInit(cmd *cobra.Command, args []string, scope, tools string) error {
	if len(args) == 0 {
		return errors.New("usage: aiwarehouse init <repo-url> (interactive create flow comes later)")
	}
	if scope != "project" && scope != "user" {
		return fmt.Errorf("invalid scope %q: want project|user", scope)
	}
	root := initRoot(scope)
	part := config.PartitionFor(root)
	if err := part.Ensure(); err != nil {
		return err
	}
	if _, err := os.Stat(part.Repo); os.IsNotExist(err) {
		repo, err := provider.New(args[0])
		if err != nil {
			return err
		}
		fmt.Println("Cloning", args[0], "->", part.Repo)
		if err := repo.Clone(part.Repo); err != nil {
			return fmt.Errorf("clone: %w", err)
		}
	}
	cfg := config.LocalConfig{Repo: args[0], Scope: scope, Tools: splitCSV(tools)}
	if scope == "project" {
		cfg.ProjectRoot = root
	}
	if err := config.Save(part.Config, cfg); err != nil {
		return err
	}
	report, err := hub.Syncer{Root: root, RepoPath: part.Repo, Partition: part, Tools: cfg.Tools}.Pull(false)
	if err != nil {
		return err
	}
	printReport(report)
	return nil
}

func runPull(cmd *cobra.Command, dryRun bool) error {
	root := initRoot("project")
	part := config.PartitionFor(root)
	cfg, err := config.Load(part.Config)
	if err != nil {
		return err
	}
	if cfg.Repo == "" {
		return errors.New("not initialized: run aiwarehouse init <repo-url> first")
	}
	if cfg.Scope == "user" {
		root = initRoot("user")
		part = config.PartitionFor(root)
	}
	report, err := hub.Syncer{Root: root, RepoPath: part.Repo, Partition: part, Tools: cfg.Tools}.Pull(dryRun)
	if err != nil {
		return err
	}
	if dryRun {
		fmt.Println("dry run — no changes written")
	}
	printReport(report)
	return nil
}

func printReport(r hub.Report) {
	fmt.Println("installed:", strings.Join(r.Installed, ", "))
	if len(r.Skipped) > 0 {
		fmt.Println("skipped (tool not detected):", strings.Join(r.Skipped, ", "))
	}
	if len(r.MissingClis) > 0 {
		fmt.Println("missing CLIs:", strings.Join(r.MissingClis, ", "), "(see clis/clis.yaml for install commands)")
	}
}
