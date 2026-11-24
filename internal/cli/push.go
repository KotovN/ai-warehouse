package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/KotovN/ai-warehouse/internal/config"
	"github.com/KotovN/ai-warehouse/internal/hub"
	"github.com/KotovN/ai-warehouse/internal/provider"
	"github.com/spf13/cobra"
)

type pushFlags struct {
	dryRun      bool
	types       string
	fromTool    string
	interactive bool
}

// username derives a push identity: config.yaml, git config, env, then "unknown".
func username(cfg config.LocalConfig) string {
	if s := strings.TrimSpace(cfg.Username); s != "" {
		return s
	}
	if out, err := exec.Command("git", "config", "--get", "user.name").Output(); err == nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			return s
		}
	}
	for _, env := range []string{"USERNAME", "USER"} {
		if s := os.Getenv(env); s != "" {
			return s
		}
	}
	return "unknown"
}

// runPush shares local tool-dir resources to the hub repo via a pull request.
func runPush(cmd *cobra.Command, args []string, flags pushFlags) error {
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

	selected := append([]string(nil), args...)
	typeList := splitCSV(flags.types)

	if flags.interactive && len(selected) == 0 {
		picked, cancelled, err := promptSelectItems(cmd, root, cfg.Scope, part.Repo, flags.fromTool, typeList)
		if err != nil {
			return err
		}
		if cancelled {
			fmt.Fprintln(cmd.OutOrStdout(), "cancelled — nothing pushed")
			return nil
		}
		selected = picked
	}

	pusher := hub.Pusher{
		Root:      root,
		Scope:     cfg.Scope,
		Partition: part,
		Repo:      repo,
		Username:  username(cfg),
		Reviewers: reviewers,
	}
	report, err := pusher.PushWithOptions(hub.PushOptions{
		DryRun:   flags.dryRun,
		Select:   selected,
		Types:    typeList,
		FromTool: strings.TrimSpace(flags.fromTool),
	})
	return handlePushResult(cmd, report, err, flags.dryRun)
}

func promptSelectItems(cmd *cobra.Command, root, scope, clonePath, fromTool string, types []string) ([]string, bool, error) {
	items, err := hub.DiffLocalItems(root, scope, clonePath, fromTool)
	if err != nil {
		return nil, false, err
	}
	items, err = hub.FilterItems(items, nil, types)
	if err != nil {
		return nil, false, err
	}
	if len(items) == 0 {
		return nil, false, nil
	}
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Local changes available to share:")
	for i, it := range items {
		fmt.Fprintf(out, "  [%d] %-6s %-24s (from %s)\n", i+1, it.Status, it.ID(), it.Source)
	}
	fmt.Fprint(out, "Select numbers (e.g. 1,3), 'all', or empty to cancel: ")
	line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	ans := strings.TrimSpace(line)
	if ans == "" || strings.EqualFold(ans, "q") || strings.EqualFold(ans, "quit") {
		return nil, true, nil
	}
	if strings.EqualFold(ans, "all") || ans == "*" {
		ids := make([]string, 0, len(items))
		for _, it := range items {
			ids = append(ids, it.ID())
		}
		return ids, false, nil
	}
	var ids []string
	for _, tok := range strings.Split(ans, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		idx, err := strconv.Atoi(tok)
		if err != nil || idx < 1 || idx > len(items) {
			return nil, false, fmt.Errorf("invalid selection %q (want 1-%d or 'all')", tok, len(items))
		}
		ids = append(ids, items[idx-1].ID())
	}
	if len(ids) == 0 {
		return nil, true, nil
	}
	return ids, false, nil
}

func handlePushResult(cmd *cobra.Command, report hub.PushReport, err error, dryRun bool) error {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()
	for _, w := range report.Warnings {
		fmt.Fprintln(errOut, "warning:", w)
	}
	if err != nil {
		if errors.Is(err, provider.ErrNotIntegrated) && report.Branch != "" {
			printPushSummary(out, report)
			fmt.Fprintln(out, "pushed branch", report.Branch, "- open the pull request on the host")
			return nil
		}
		if report.PRURL != "" {
			printPushSummary(out, report)
			fmt.Fprintln(out, "opened pull request:", report.PRURL)
			return fmt.Errorf("pull request opened (%s) but: %w", report.PRURL, err)
		}
		if report.Branch != "" {
			return fmt.Errorf("pushed branch %s but: %w", report.Branch, err)
		}
		return err
	}
	if len(report.Added) == 0 && len(report.Edited) == 0 {
		fmt.Fprintln(out, "nothing to share: local tools match the hub")
		return nil
	}
	printPushSummary(out, report)
	if report.ReusedPR {
		fmt.Fprintln(out, "updated existing pull request:", report.PRURL)
	} else if report.PRURL != "" {
		fmt.Fprintln(out, "opened pull request:", report.PRURL)
	} else if dryRun {
		fmt.Fprintln(out, "(dry run) nothing pushed")
	} else if report.Branch != "" {
		fmt.Fprintln(out, "pushed branch", report.Branch, "- open the pull request on the host")
	}
	return nil
}

func printPushSummary(out interface{ Write([]byte) (int, error) }, report hub.PushReport) {
	if len(report.Added) > 0 {
		fmt.Fprintln(out, "added:", strings.Join(report.Added, ", "))
	}
	if len(report.Edited) > 0 {
		fmt.Fprintln(out, "edited:", strings.Join(report.Edited, ", "))
	}
}
