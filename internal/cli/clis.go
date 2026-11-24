package cli

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/KotovN/ai-warehouse/internal/config"
	"github.com/KotovN/ai-warehouse/internal/hub"
	"github.com/KotovN/ai-warehouse/internal/provider"
	"github.com/spf13/cobra"
)

type cliAddOpts struct {
	check            string
	url              string
	installDefault   string
	installWindows   string
	installDarwin    string
	installLinux     string
	uninstallDefault string
	uninstallWindows string
	uninstallDarwin  string
	uninstallLinux   string
	dryRun           bool
}

// isInteractiveStdin reports whether cmd's input stream is an interactive terminal.
func isInteractiveStdin(cmd *cobra.Command) bool {
	f, ok := cmd.InOrStdin().(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func runCliList(cmd *cobra.Command, _ []string) error {
	part, _, cfg, err := activeInstall()
	if err != nil {
		return err
	}
	res, err := hub.BuildResources(part.Repo)
	if err != nil {
		return err
	}
	items := hub.InspectClisWithIgnore(res.Clis, runtime.GOOS, cfg.Ignore)
	if len(items) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "no CLIs registered in clis/clis.yaml")
		return nil
	}
	for _, c := range items {
		mark := "ok     "
		switch {
		case c.Ignored:
			mark = "ignored"
		case !c.Installed:
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
	part, _, cfg, err := activeInstall()
	if err != nil {
		return err
	}
	res, err := hub.BuildResources(part.Repo)
	if err != nil {
		return err
	}
	all := hub.InspectClisWithIgnore(res.Clis, runtime.GOOS, cfg.Ignore)
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
			if !c.Installed && !c.Ignored {
				targets = append(targets, c)
			}
		}
	}
	if len(targets) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "all required CLIs are already installed")
		return nil
	}
	return promptAndInstallClis(cmd, part, &cfg, targets, dryRun, autoYes)
}

// promptAndInstallClis walks through missing CLIs offering [i]nstall, [s]kip,
// or [g] ignore (persisting cli:<name> to config.yaml's ignore list).
func promptAndInstallClis(cmd *cobra.Command, part config.Partition, cfg *config.LocalConfig, targets []hub.CliStatus, dryRun, autoYes bool) error {
	reader := bufio.NewReader(cmd.InOrStdin())
	out := cmd.OutOrStdout()
	for _, c := range targets {
		if dryRun {
			if c.InstallCmd == "" {
				if c.URL != "" {
					fmt.Fprintf(out, "[dry-run] %s: no install command for %s — see %s\n", c.Name, runtime.GOOS, c.URL)
				} else {
					fmt.Fprintf(out, "[dry-run] %s: no install command for %s\n", c.Name, runtime.GOOS)
				}
			} else {
				fmt.Fprintf(out, "[dry-run] %s: %s\n", c.Name, c.InstallCmd)
			}
			continue
		}

		action := "skip"
		if autoYes {
			if c.InstallCmd != "" {
				action = "install"
			} else {
				if c.URL != "" {
					fmt.Fprintf(out, "%s: no install command for %s — download at %s\n", c.Name, runtime.GOOS, c.URL)
				} else {
					fmt.Fprintf(out, "%s: no install command configured for %s\n", c.Name, runtime.GOOS)
				}
				continue
			}
		} else {
			fmt.Fprintf(out, "\nMissing CLI: %s\n", c.Name)
			if c.Check != "" {
				fmt.Fprintf(out, "  check:   %s\n", c.Check)
			}
			if c.InstallCmd != "" {
				fmt.Fprintf(out, "  install: %s\n", c.InstallCmd)
			} else {
				fmt.Fprintf(out, "  install: (not configured for %s)\n", runtime.GOOS)
			}
			if c.URL != "" {
				fmt.Fprintf(out, "  url:     %s\n", c.URL)
			}
			if c.InstallCmd != "" {
				fmt.Fprint(out, "Action: [i]nstall, [s]kip, [g] ignore (don't ask again)? [s]: ")
			} else {
				fmt.Fprint(out, "Action: [s]kip, [g] ignore (don't ask again)? [s]: ")
			}
			line, _ := reader.ReadString('\n')
			ans := strings.ToLower(strings.TrimSpace(line))
			switch ans {
			case "i", "install", "y", "yes":
				if c.InstallCmd != "" {
					action = "install"
				} else {
					fmt.Fprintf(out, "%s: no install command for %s — skipping\n", c.Name, runtime.GOOS)
				}
			case "g", "ignore", "never":
				action = "ignore"
			default:
				action = "skip"
			}
		}

		switch action {
		case "install":
			fmt.Fprintf(out, "Installing %s: %s\n", c.Name, c.InstallCmd)
			if err := hub.InstallCli(c, out, cmd.ErrOrStderr()); err != nil {
				return err
			}
			if c.Check != "" && !hub.VerifyCli(c.Check) {
				fmt.Fprintf(out, "installed %s (if '%s' is not found yet, open a new terminal to refresh PATH)\n", c.Name, c.Check)
			} else {
				fmt.Fprintf(out, "installed %s\n", c.Name)
			}
		case "ignore":
			updated, _, err := config.AddIgnore(cfg.Ignore, "cli:"+c.Name)
			if err != nil {
				return err
			}
			cfg.Ignore = updated
			if err := config.Save(part.Config, *cfg); err != nil {
				return err
			}
			fmt.Fprintf(out, "ignored cli:%s (won't ask again; run 'aiw ignore remove cli:%s' to restore)\n", c.Name, c.Name)
		default:
			fmt.Fprintf(out, "skipped %s\n", c.Name)
		}
	}
	return nil
}

func buildCliInstall(def, win, darwin, linux string) config.CliInstall {
	inst := config.CliInstall{Default: strings.TrimSpace(def)}
	byOS := map[string]string{}
	if v := strings.TrimSpace(win); v != "" {
		byOS["windows"] = v
	}
	if v := strings.TrimSpace(darwin); v != "" {
		byOS["darwin"] = v
	}
	if v := strings.TrimSpace(linux); v != "" {
		byOS["linux"] = v
	}
	if len(byOS) > 0 {
		if inst.Default != "" {
			byOS["default"] = inst.Default
			inst.Default = ""
		}
		inst.ByOS = byOS
	}
	return inst
}

func runCliAdd(cmd *cobra.Command, name string, opts cliAddOpts) error {
	if strings.TrimSpace(opts.check) == "" {
		return fmt.Errorf("--check is required (e.g. --check \"%s --version\")", name)
	}
	inst := buildCliInstall(opts.installDefault, opts.installWindows, opts.installDarwin, opts.installLinux)
	uninst := buildCliInstall(opts.uninstallDefault, opts.uninstallWindows, opts.uninstallDarwin, opts.uninstallLinux)
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
		Ignore:    cfg.Ignore,
	}
	report, err := pusher.AddCli(name, config.CliTool{
		Check:     strings.TrimSpace(opts.check),
		URL:       strings.TrimSpace(opts.url),
		Install:   inst,
		Uninstall: uninst,
	}, opts.dryRun)
	return handlePushResult(cmd, report, err, opts.dryRun)
}
