// Package doctor runs diagnostic checks over an aiwarehouse install.
package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/KotovN/ai-warehouse/internal/config"
	"github.com/KotovN/ai-warehouse/internal/hub"
	"github.com/KotovN/ai-warehouse/internal/tool"
)

// Check is one diagnostic result.
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// Run performs all diagnostics for the given install; cfg may be zero.
func Run(root string, cfg config.LocalConfig, part config.Partition) []Check {
	var checks []Check
	add := func(name string, ok bool, format string, args ...any) {
		checks = append(checks, Check{Name: name, OK: ok, Detail: fmt.Sprintf(format, args...)})
	}

	// Local config.
	if cfg.Repo == "" {
		add("config", false, "not initialized: run aiw init <repo-url>")
	} else {
		add("config", true, "hub %s (%s scope)", cfg.Repo, cfg.Scope)
	}

	// Git binary (used for revision reads and user config lookups).
	if b, err := exec.Command("git", "version").CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = err.Error()
		}
		add("git", false, "git binary unavailable: %s", msg)
	} else {
		ver := strings.TrimSpace(string(b))
		if parts := strings.SplitN(ver, " ", 3); len(parts) == 3 {
			ver = parts[2]
		}
		add("git", true, "%s", ver)
	}

	// Hub clone readability (zero res keeps CLI checks silent when unusable).
	res := tool.Resources{}
	if cfg.Repo == "" {
		add("hub clone", false, "skipped: no config")
	} else if _, err := os.Stat(part.Repo); os.IsNotExist(err) {
		add("hub clone", false, "clone missing at %s: run aiw init", part.Repo)
	} else if _, err := os.Stat(filepath.Join(part.Repo, ".git")); os.IsNotExist(err) {
		add("hub clone", false, "not a git clone at %s: run aiw init --force", part.Repo)
	} else if loaded, err := hub.BuildResources(part.Repo); err != nil {
		add("hub clone", false, "unreadable at %s: %v", part.Repo, err)
	} else {
		res = loaded
		add("hub clone", true, "%d skills, %d rules, %d mcp servers at %s",
			len(res.Skills), len(res.Rules), len(res.MCP.Servers), part.Repo)
	}

	// Per-tool health (respects cfg.Tools filter and custom specs).
	scope := cfg.Scope
	if scope == "" {
		scope = "project"
	}
	extraDirs := tool.DefaultExtraDirs("")
	if cfg.Repo != "" {
		extraDirs = tool.DefaultExtraDirs(part.Repo)
	}
	adapters, err := tool.LoadAdapters(root, scope, extraDirs...)
	if err != nil {
		add("tool specs", false, "failed to load tool specs: %v", err)
	} else {
		explicit := len(cfg.Tools) > 0
		for _, a := range adapters {
			if !toolEnabled(cfg.Tools, a.ID()) {
				continue
			}
			if !explicit && !a.Detect() {
				add(a.ID(), true, "not detected (skipped)")
				continue
			}
			inv, err := a.Inventory()
			if err != nil {
				add(a.ID(), false, "inventory failed: %v", err)
				continue
			}
			add(a.ID(), true, "%d skills, %d rules, %d mcp servers", len(inv.Skills), len(inv.Rules), len(inv.Servers))
			if a.SupportsHooks() {
				if !res.Warehouse.AutoPullEnabled() {
					add(a.ID()+" hooks", true, "autoPull disabled by warehouse.yaml")
				} else if !inv.HooksSet {
					add(a.ID()+" hooks", false, "session-start hook missing: run aiw pull")
				} else {
					add(a.ID()+" hooks", true, "session-start hook installed")
				}
			} else {
				add(a.ID()+" hooks", true, "no agent hooks supported; run aiw pull manually")
			}
		}
	}

	// CI token for push flows.
	if tok := githubTokenHint(); tok != "" {
		add("github token", true, "%s set", tok)
	} else {
		add("github token", true, "not set; pushing to GitHub requires GITHUB_TOKEN or GH_TOKEN")
	}

	// Team CLI tools (uses the zero-value res when the clone is unreadable).
	var missingDetails []string
	for _, st := range hub.InspectClis(res.Clis, runtime.GOOS) {
		if !st.Installed {
			hint := st.Name
			if st.InstallCmd != "" {
				hint += " (" + st.InstallCmd + ")"
			} else if st.URL != "" {
				hint += " (" + st.URL + ")"
			}
			missingDetails = append(missingDetails, hint)
		}
	}
	if len(missingDetails) > 0 {
		add("team clis", false, "missing: %s — run aiw cli install", strings.Join(missingDetails, "; "))
	} else {
		add("team clis", true, "all registered clis present")
	}
	return checks
}

func toolEnabled(configured []string, id string) bool {
	if len(configured) == 0 {
		return true
	}
	for _, t := range configured {
		if strings.EqualFold(strings.TrimSpace(t), id) {
			return true
		}
	}
	return false
}

func githubTokenHint() string {
	for _, env := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if os.Getenv(env) != "" {
			return env
		}
	}
	return ""
}
