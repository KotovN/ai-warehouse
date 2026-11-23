// Package doctor runs diagnostic checks over an aiwarehouse install.
package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/KotovN/ai-warehouse/internal/config"
	"github.com/KotovN/ai-warehouse/internal/hub"
	"github.com/KotovN/ai-warehouse/internal/tool"
)

// Check is one diagnostic result.
type Check struct {
	Name   string
	OK     bool
	Detail string
}

// Run performs all diagnostics for the given install; cfg may be zero.
func Run(root string, cfg config.LocalConfig, part config.Partition) []Check {
	var checks []Check
	add := func(name string, ok bool, format string, args ...any) {
		checks = append(checks, Check{Name: name, OK: ok, Detail: fmt.Sprintf(format, args...)})
	}

	// Local config.
	if cfg.Repo == "" {
		add("config", false, "not initialized: run aiwarehouse init <repo-url>")
	} else {
		add("config", true, "hub %s (%s scope)", cfg.Repo, cfg.Scope)
	}

	// Git binary (used for revision reads and user config lookups).
	if b, err := exec.Command("git", "version").CombinedOutput(); err != nil {
		add("git", false, "git binary unavailable: %s", strings.TrimSpace(string(b)))
	} else {
		add("git", true, "%s", strings.SplitN(strings.TrimSpace(string(b)), " ", 3)[2])
	}

	// Hub clone readability (zero res keeps CLI checks silent when unusable).
	res := tool.Resources{}
	if cfg.Repo == "" {
		add("hub clone", false, "skipped: no config")
	} else if _, err := os.Stat(part.Repo); os.IsNotExist(err) {
		add("hub clone", false, "clone missing at %s: run aiwarehouse init", part.Repo)
	} else if loaded, err := hub.BuildResources(part.Repo); err != nil {
		add("hub clone", false, "unreadable at %s: %v", part.Repo, err)
	} else {
		res = loaded
		add("hub clone", true, "%d skills, %d rules, %d mcp servers at %s",
			len(res.Skills), len(res.Rules), len(res.MCP.Servers), part.Repo)
	}

	// Per-tool health.
	for _, a := range tool.All(root) {
		if !a.Detect() {
			add(a.ID(), true, "not detected (skipped)")
			continue
		}
		inv, err := a.Inventory()
		if err != nil {
			add(a.ID(), false, "inventory failed: %v", err)
			continue
		}
		add(a.ID(), true, "%d skills, %d mcp servers", len(inv.Skills), len(inv.Servers))
		switch a.ID() {
		case "cursor", "codex": // hooks-capable tools
			if !inv.HooksSet {
				add(a.ID()+" hooks", false, "session-start hook missing: run aiwarehouse pull")
			} else {
				add(a.ID()+" hooks", true, "session-start hook installed")
			}
		case "vscode", "opencode":
			add(a.ID()+" hooks", true, "no agent hooks supported; run aiwarehouse pull manually")
		}
	}

	// CI token for push flows.
	if tok := githubTokenHint(); tok != "" {
		add("github token", true, "%s set", tok)
	} else {
		add("github token", true, "not set; pushing to GitHub requires GITHUB_TOKEN or GH_TOKEN")
	}

	// Team CLI tools (uses the zero-value res when the clone is unreadable).
	if missing := hub.CheckClis(res.Clis); len(missing) > 0 {
		add("team clis", false, "missing: %s (install commands in clis/clis.yaml)", strings.Join(missing, ", "))
	} else {
		add("team clis", true, "all registered clis present")
	}
	return checks
}

func githubTokenHint() string {
	for _, env := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if os.Getenv(env) != "" {
			return env
		}
	}
	return ""
}
