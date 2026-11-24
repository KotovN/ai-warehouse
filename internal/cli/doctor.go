package cli

import (
	"encoding/json"
	"fmt"

	"github.com/KotovN/ai-warehouse/internal/config"
	"github.com/KotovN/ai-warehouse/internal/doctor"
	"github.com/KotovN/ai-warehouse/internal/hub"
	"github.com/spf13/cobra"
)

// runDoctor prints diagnostic checks; exits non-zero when any check fails.
func runDoctor(cmd *cobra.Command, asJSON bool) error {
	part, root, cfg, _ := activeInstall()
	checks := doctor.Run(root, cfg, part)
	out := cmd.OutOrStdout()
	if asJSON {
		b, err := json.MarshalIndent(checks, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(b))
	} else {
		for _, c := range checks {
			mark := "ok  "
			if !c.OK {
				mark = "FAIL"
			}
			fmt.Fprintf(out, "%-20s %s  %s\n", c.Name, mark, c.Detail)
		}
	}
	if failed := countFailed(checks); failed > 0 {
		return fmt.Errorf("doctor found %d failing check(s)", failed)
	}
	return nil
}

func countFailed(checks []doctor.Check) int {
	n := 0
	for _, c := range checks {
		if !c.OK {
			n++
		}
	}
	return n
}

// runStatus shows local vs hub diff and sync state.
func runStatus(cmd *cobra.Command) error {
	part, root, cfg, err := activeInstall()
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "hub:       %s\n", cfg.Repo)
	fmt.Fprintf(out, "scope:     %s (%s)\n", or(cfg.Scope, "project"), root)
	fmt.Fprintf(out, "tools:     %v\n", orAuto(cfg.Tools))
	if len(cfg.Ignore) > 0 {
		fmt.Fprintf(out, "ignored:   %v\n", cfg.Ignore)
	}
	if st, err := config.LoadState(part.State); err == nil {
		rev := or(st.Revision, "none")
		if len(rev) > 8 {
			rev = rev[:8]
		}
		fmt.Fprintf(out, "last pull: %s (hub @ %s)\n", or(st.LastPull, "never"), rev)
	} else {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: unable to read state.json: %v\n", err)
	}
	checks := doctor.Run(root, cfg, part)
	fmt.Fprintf(out, "doctor:    %d/%d checks ok\n", len(checks)-countFailed(checks), len(checks))
	items, err := hub.DiffLocalWithIgnore(root, cfg.Scope, part.Repo, "", cfg.Ignore)
	if err != nil {
		return err
	}
	var added, edited []string
	for _, it := range items {
		if it.Status == "added" {
			added = append(added, it.ID())
		} else {
			edited = append(edited, it.ID())
		}
	}
	fmt.Fprintf(out, "to share:  %d added, %d edited (aiw push --dry-run for details)\n", len(added), len(edited))
	if len(added) > 0 {
		fmt.Fprintf(out, "  added:   %v\n", added)
	}
	if len(edited) > 0 {
		fmt.Fprintf(out, "  edited:  %v\n", edited)
	}
	return nil
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func orAuto(tools []string) string {
	if len(tools) == 0 {
		return "auto (all detected)"
	}
	return fmt.Sprintf("%v", tools)
}
