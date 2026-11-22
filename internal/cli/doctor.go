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
	part, root, _ := projectPartition()
	cfg, err := config.Load(part.Config)
	if err != nil {
		cfg = config.LocalConfig{}
	}
	checks := doctor.Run(root, cfg, part)
	if asJSON {
		b, err := json.MarshalIndent(checks, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(b))
	} else {
		for _, c := range checks {
			mark := "ok  "
			if !c.OK {
				mark = "FAIL"
			}
			fmt.Printf("%-20s %s  %s\n", c.Name, mark, c.Detail)
		}
	}
	for _, c := range checks {
		if !c.OK {
			return fmt.Errorf("doctor found %d failing check(s)", countFailed(checks))
		}
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
	part, root, err := projectPartition()
	if err != nil {
		return err
	}
	cfg, err := config.Load(part.Config)
	if err != nil {
		return err
	}
	fmt.Printf("hub:       %s\n", cfg.Repo)
	fmt.Printf("tools:     %v\n", orAuto(cfg.Tools))
	if st, err := config.LoadState(part.State); err == nil {
		fmt.Printf("last pull: %s (hub @ %.8s)\n", or(st.LastPull, "never"), st.Revision)
		checks := doctor.Run(root, cfg, part)
		fmt.Printf("doctor:    %d/%d checks ok\n", len(checks)-countFailed(checks), len(checks))
	}
	added, edited, err := hub.DiffLocal(root, part.Repo)
	if err != nil {
		return err
	}
	fmt.Printf("to share:  %d added, %d edited (aiw push --dry-run for details)\n", len(added), len(edited))
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
