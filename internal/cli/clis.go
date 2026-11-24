package cli

import (
	"bufio"
	"fmt"
	"runtime"
	"strings"

	"github.com/KotovN/ai-warehouse/internal/config"
	"github.com/KotovN/ai-warehouse/internal/hub"
	"github.com/KotovN/ai-warehouse/internal/provider"
	"github.com/spf13/cobra"
)

type cliAddOpts struct {
	check          string
	url            string
	installDefault string
	installWindows string
	installDarwin  string
	installLinux   string
	dryRun         bool
}

func runCliList(cmd *cobra.Command, _ []string) error {
	part, _, _, err := activeInstall()
	if err != nil {
		return err
	}
	res, err := hub.BuildResources(part.Repo)
	if err != nil {
		return err
	}
	items := hub.InspectClis(res.Clis, runtime.GOOS)
	if len(items) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no CLIs registered in clis/clis.yaml")
		return nil
	}
	for _, c := range items {
		mark := "ok     "
		if !c.Installed {
			mark = "missing"
		}
		detail := c.InstallCmd
		if detail == "" {
			detail = "(no install command for " + runtime.GOOS + ")"
		}
		if c.URL != "" {
			detail += "  [" + c.URL + "]"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%-16s %s  %s\n", c.Name, mark, detail)
	}
	return nil
}

func runCliInstall(cmd *cobra.Command, args []string, dryRun, autoYes bool) error {
	part, _, _, err := activeInstall()
	if err != nil {
		return err
	}
	res, err := hub.BuildResources(part.Repo)
	if err != nil {
		return err
	}
	all := hub.InspectClis(res.Clis, runtime.GOOS)
	byName := map[string]hub.CliStatus{}
	for _, c := range all {
		byName[c.Name] = c
	}

	var targets []hub.CliStatus
	if len(args) > 0 {
		for _, name := range args {
			c, ok := byName[name]
			if !ok {
				return fmt.Errorf("unknown CLI %q in clis/clis.yaml", name)
			}
			targets = append(targets, c)
		}
	} else {
		for _, c := range all {
			if !c.Installed {
				targets = append(targets, c)
			}
		}
	}
	if len(targets) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "all registered CLIs are already installed")
		return nil
	}
	return installCliList(cmd, targets, dryRun, autoYes)
}

func installCliList(cmd *cobra.Command, targets []hub.CliStatus, dryRun, autoYes bool) error {
	reader := bufio.NewReader(cmd.InOrStdin())
	out := cmd.OutOrStdout()
	for _, c := range targets {
		if c.InstallCmd == "" {
			if c.URL != "" {
				fmt.Fprintf(out, "%s: no install command for %s — download at %s\n", c.Name, runtime.GOOS, c.URL)
			} else {
				fmt.Fprintf(out, "%s: no install command configured for %s\n", c.Name, runtime.GOOS)
			}
			continue
		}
		if dryRun {
			fmt.Fprintf(out, "[dry-run] %s: %s\n", c.Name, c.InstallCmd)
			continue
		}
		if !autoYes {
			fmt.Fprintf(out, "Install %s by running: %s\nProceed? [y/N]: ", c.Name, c.InstallCmd)
			line, _ := reader.ReadString('\n')
			ans := strings.ToLower(strings.TrimSpace(line))
			if ans != "y" && ans != "yes" {
				fmt.Fprintf(out, "skipped %s\n", c.Name)
				continue
			}
		}
		fmt.Fprintf(out, "Installing %s: %s\n", c.Name, c.InstallCmd)
		if err := hub.InstallCli(c, out, cmd.ErrOrStderr()); err != nil {
			return err
		}
		if c.Check != "" && !hub.VerifyCli(c.Check) {
			fmt.Fprintf(out, "installed %s (if '%s' is not found yet, open a new terminal to refresh PATH)\n", c.Name, c.Check)
		} else {
			fmt.Fprintf(out, "installed %s\n", c.Name)
		}
	}
	return nil
}

func runCliAdd(cmd *cobra.Command, name string, opts cliAddOpts) error {
	if strings.TrimSpace(opts.check) == "" {
		return fmt.Errorf("--check is required (e.g. --check \"%s --version\")", name)
	}
	inst := config.CliInstall{Default: strings.TrimSpace(opts.installDefault)}
	byOS := map[string]string{}
	if v := strings.TrimSpace(opts.installWindows); v != "" {
		byOS["windows"] = v
	}
	if v := strings.TrimSpace(opts.installDarwin); v != "" {
		byOS["darwin"] = v
	}
	if v := strings.TrimSpace(opts.installLinux); v != "" {
		byOS["linux"] = v
	}
	if len(byOS) > 0 {
		if inst.Default != "" {
			byOS["default"] = inst.Default
			inst.Default = ""
		}
		inst.ByOS = byOS
	}
	if inst.IsZero() && strings.TrimSpace(opts.url) == "" {
		return fmt.Errorf("specify at least one of --install, --install-windows/--install-darwin/--install-linux, or --url")
	}

	part, root, cfg, err := activeInstall()
	if err != nil {
		return err
	}
	repo, err := provider.New(cfg.Repo)
	if err != nil {
		return err
	}
	var reviewers []string
	if w, err := config.LoadWarehouse(part.Repo); err == nil {
		reviewers = w.Reviewers
	}
	pusher := hub.Pusher{
		Root:      root,
		Scope:     cfg.Scope,
		Partition: part,
		Repo:      repo,
		Username:  username(cfg),
		Reviewers: reviewers,
	}
	report, err := pusher.AddCli(name, config.CliTool{
		Check:   strings.TrimSpace(opts.check),
		URL:     strings.TrimSpace(opts.url),
		Install: inst,
	}, opts.dryRun)
	return handlePushResult(cmd, report, err, opts.dryRun)
}
