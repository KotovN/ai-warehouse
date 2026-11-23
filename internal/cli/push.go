package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/KotovN/ai-warehouse/internal/config"
	"github.com/KotovN/ai-warehouse/internal/hub"
	"github.com/KotovN/ai-warehouse/internal/provider"
	"github.com/spf13/cobra"
)

// username derives a push identity: git config, then env, then "unknown".
func username() string {
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
func runPush(cmd *cobra.Command, dryRun bool) error {
	part, root, err := projectPartition()
	if err != nil {
		return err
	}
	cfg, err := config.Load(part.Config)
	if err != nil {
		return err
	}
	repo, err := provider.New(cfg.Repo)
	if err != nil {
		return err
	}
	reviewers := []string{}
	if w, err := config.LoadWarehouse(part.Repo); err == nil {
		reviewers = w.Reviewers
	}
	pusher := hub.Pusher{
		Root:      root,
		Partition: part,
		Repo:      repo,
		Username:  username(),
		Reviewers: reviewers,
	}
	report, err := pusher.Push(dryRun)
	if err != nil {
		if report.Branch != "" {
			return fmt.Errorf("pushed branch %s but: %w", report.Branch, err)
		}
		return err
	}
	if len(report.Added) == 0 && len(report.Edited) == 0 {
		fmt.Println("nothing to share: local tools match the hub")
		return nil
	}
	fmt.Println("added:", strings.Join(report.Added, ", "))
	fmt.Println("edited:", strings.Join(report.Edited, ", "))
	if report.ReusedPR {
		fmt.Println("updated existing pull request:", report.PRURL)
	} else if report.PRURL != "" {
		fmt.Println("opened pull request:", report.PRURL)
	} else if report.Branch != "" {
		fmt.Println("pushed branch", report.Branch, "- open the pull request on the host")
	} else {
		fmt.Println("(dry run) nothing pushed")
	}
	return nil
}
