package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KotovN/ai-warehouse/internal/config"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func setupIsolatedEnv(t *testing.T) (home, hubDir, projDir string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Cleanup(func() { _ = config.RemoveAll(home) })

	hubDir = t.TempDir()
	t.Cleanup(func() { _ = config.RemoveAll(hubDir) })
	r, err := git.PlainInit(hubDir, false)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(hubDir, "warehouse.yaml"), []byte("team: demo\n"), 0o644)
	os.MkdirAll(filepath.Join(hubDir, "skills", "hello"), 0o755)
	os.WriteFile(filepath.Join(hubDir, "skills", "hello", "SKILL.md"), []byte("# Hello\n"), 0o644)
	os.MkdirAll(filepath.Join(hubDir, "rules"), 0o755)
	os.WriteFile(filepath.Join(hubDir, "rules", "style.md"), []byte("Be concise.\n"), 0o644)
	os.MkdirAll(filepath.Join(hubDir, "mcp"), 0o755)
	os.WriteFile(filepath.Join(hubDir, "mcp", "mcp.yaml"), []byte("servers:\n  fs:\n    command: npx\n"), 0o644)
	os.MkdirAll(filepath.Join(hubDir, "clis"), 0o755)
	os.WriteFile(filepath.Join(hubDir, "clis", "clis.yaml"), []byte("clis:\n  go:\n    check: go version\n    url: https://go.dev\n    install: winget install GoLang.Go\n  fakecli:\n    check: definitely-missing-cmd-xyz --version\n    url: https://example.test/fakecli\n    install:\n      windows: echo install-win\n      darwin: echo install-mac\n      linux: echo install-linux\n    uninstall:\n      windows: echo uninstall-win\n      darwin: echo uninstall-mac\n      linux: echo uninstall-linux\n"), 0o644)
	os.MkdirAll(filepath.Join(hubDir, "docs"), 0o755)
	os.WriteFile(filepath.Join(hubDir, "docs", "onboarding.md"), []byte("# Welcome\n"), 0o644)

	w, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Add("."); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit("seed hub", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@local", When: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}

	projDir = t.TempDir()
	return home, hubDir, projDir
}

func execCLI(t *testing.T, cwd, stdin string, args ...string) (string, string, error) {
	t.Helper()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWD) }()

	var outBuf, errBuf bytes.Buffer
	cmd := New()
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	err = cmd.Execute()
	return outBuf.String(), errBuf.String(), err
}

func TestEndToEndProjectFlow(t *testing.T) {
	_, hubDir, projDir := setupIsolatedEnv(t)

	// 1. init with explicit tools (including antigravity!)
	out, _, err := execCLI(t, projDir, "", "init", hubDir, "--tools", "cursor,antigravity")
	if err != nil {
		t.Fatalf("init failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "antigravity") || !strings.Contains(out, "cursor") {
		t.Fatalf("expected antigravity and cursor installed:\n%s", out)
	}

	// 2. pull --quiet (used by session-start hooks!) succeeds silently
	out, _, err = execCLI(t, projDir, "", "pull", "--quiet")
	if err != nil {
		t.Fatalf("pull --quiet failed: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("pull --quiet should produce no stdout, got:\n%s", out)
	}

	// 3. Running from a nested subdirectory resolves the project partition
	subDir := filepath.Join(projDir, "src", "pkg")
	os.MkdirAll(subDir, 0o755)
	out, _, err = execCLI(t, subDir, "", "status")
	if err != nil {
		t.Fatalf("status from subdir failed: %v", err)
	}
	if !strings.Contains(out, "scope:     project") {
		t.Fatalf("unexpected status:\n%s", out)
	}

	// 4. list rules & docs work
	out, _, err = execCLI(t, projDir, "", "list", "rules")
	if err != nil || !strings.Contains(out, "style") {
		t.Fatalf("list rules: err=%v out=%s", err, out)
	}
	out, _, err = execCLI(t, projDir, "", "list", "docs")
	if err != nil || !strings.Contains(out, "onboarding.md") {
		t.Fatalf("list docs: err=%v out=%s", err, out)
	}

	// 5. cli list & cli install --dry-run
	out, _, err = execCLI(t, projDir, "", "cli", "list")
	if err != nil || !strings.Contains(out, "fakecli") || !strings.Contains(out, "https://example.test/fakecli") {
		t.Fatalf("cli list: err=%v out=%s", err, out)
	}
	out, _, err = execCLI(t, projDir, "", "cli", "install", "--dry-run")
	if err != nil || !strings.Contains(out, "[dry-run] fakecli:") {
		t.Fatalf("cli install --dry-run: err=%v out=%s", err, out)
	}

	// 6. Selective push on GenericGit (local repo) exits 0 and pushes only selected skill
	os.MkdirAll(filepath.Join(projDir, ".cursor", "skills", "eli5"), 0o755)
	os.WriteFile(filepath.Join(projDir, ".cursor", "skills", "eli5", "SKILL.md"), []byte("simple english\n"), 0o644)
	os.MkdirAll(filepath.Join(projDir, ".cursor", "skills", "other"), 0o755)
	os.WriteFile(filepath.Join(projDir, ".cursor", "skills", "other", "SKILL.md"), []byte("other\n"), 0o644)

	out, _, err = execCLI(t, projDir, "", "push", "skill:eli5")
	if err != nil {
		t.Fatalf("selective push failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "added: eli5") || strings.Contains(out, "other") || !strings.Contains(out, "pushed branch") {
		t.Fatalf("unexpected push output:\n%s", out)
	}

	// 7. Uninstall --dry-run shows real entry count (>2) and uninstall cleans up
	out, _, err = execCLI(t, projDir, "", "uninstall", "--dry-run")
	if err != nil {
		t.Fatalf("uninstall --dry-run: %v", err)
	}
	if strings.Contains(out, "(2 managed entries)") {
		t.Fatalf("dry-run printed adapter count instead of entry count:\n%s", out)
	}
	out, _, err = execCLI(t, projDir, "", "uninstall")
	if err != nil {
		t.Fatalf("uninstall failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(projDir, ".cursor", "skills", "hello")); !os.IsNotExist(err) {
		t.Fatal("managed skill hello should be removed on uninstall")
	}
	// User-created eli5 skill must survive uninstall!
	if _, err := os.Stat(filepath.Join(projDir, ".cursor", "skills", "eli5", "SKILL.md")); err != nil {
		t.Fatalf("user-created skill should survive uninstall: %v", err)
	}
}

func TestUserScopeWorksFromOtherDirectory(t *testing.T) {
	home, hubDir, projDir := setupIsolatedEnv(t)

	out, _, err := execCLI(t, projDir, "", "init", hubDir, "--scope", "user", "--tools", "antigravity")
	if err != nil {
		t.Fatalf("user-scope init failed: %v\n%s", err, out)
	}
	// Installed under ~/.gemini/config/skills/hello/SKILL.md
	if _, err := os.Stat(filepath.Join(home, ".gemini", "config", "skills", "hello", "SKILL.md")); err != nil {
		t.Fatalf("antigravity user-scope skill missing: %v", err)
	}
	// Running status and pull from an unrelated project directory must find the user partition!
	otherDir := t.TempDir()
	out, _, err = execCLI(t, otherDir, "", "status")
	if err != nil {
		t.Fatalf("status from unrelated dir failed for user scope: %v\n%s", err, out)
	}
	if !strings.Contains(out, "scope:     user") {
		t.Fatalf("expected user scope in status:\n%s", out)
	}
	if _, _, err := execCLI(t, otherDir, "", "pull", "--quiet"); err != nil {
		t.Fatalf("pull --quiet from unrelated dir failed for user scope: %v", err)
	}
}

func TestInitRejectsUnknownTool(t *testing.T) {
	_, hubDir, projDir := setupIsolatedEnv(t)
	_, _, err := execCLI(t, projDir, "", "init", hubDir, "--tools", "cursr")
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("expected unknown tool error, got: %v", err)
	}
}

func TestCliInstallInteractivePushAndCliAdd(t *testing.T) {
	_, hubDir, projDir := setupIsolatedEnv(t)

	if _, _, err := execCLI(t, projDir, "", "init", hubDir, "--tools", "cursor"); err != nil {
		t.Fatal(err)
	}

	// Execute actual CLI install command with --yes (runs `echo install-...`)
	out, _, err := execCLI(t, projDir, "", "cli", "install", "fakecli", "--yes")
	if err != nil {
		t.Fatalf("cli install --yes failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Installing fakecli:") || !strings.Contains(out, "install-") {
		t.Fatalf("unexpected cli install output:\n%s", out)
	}

	// Interactive push (-i): add 2 skills, pick only #1 via stdin
	os.MkdirAll(filepath.Join(projDir, ".cursor", "skills", "alpha"), 0o755)
	os.WriteFile(filepath.Join(projDir, ".cursor", "skills", "alpha", "SKILL.md"), []byte("a\n"), 0o644)
	os.MkdirAll(filepath.Join(projDir, ".cursor", "skills", "beta"), 0o755)
	os.WriteFile(filepath.Join(projDir, ".cursor", "skills", "beta", "SKILL.md"), []byte("b\n"), 0o644)

	out, _, err = execCLI(t, projDir, "1\n", "push", "-i", "--dry-run")
	if err != nil {
		t.Fatalf("interactive push failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "added: alpha") || strings.Contains(out, "added: alpha, beta") {
		t.Fatalf("expected only alpha selected:\n%s", out)
	}

	// Register a new CLI via `aiw cli add` (with uninstall command)
	out, _, err = execCLI(t, projDir, "", "cli", "add", "hex",
		"--check", "hex --version",
		"--install-windows", "mix local.hex --force",
		"--install-darwin", "mix local.hex --force",
		"--uninstall", "mix archive.uninstall hex --force",
		"--url", "https://hex.pm",
	)
	if err != nil {
		t.Fatalf("cli add failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "added: cli:hex") || !strings.Contains(out, "pushed branch") {
		t.Fatalf("unexpected cli add output:\n%s", out)
	}
}

func TestSelectiveUninstallAndIgnoreFlow(t *testing.T) {
	_, hubDir, projDir := setupIsolatedEnv(t)

	if _, _, err := execCLI(t, projDir, "", "init", hubDir, "--tools", "cursor,codex,antigravity"); err != nil {
		t.Fatal(err)
	}

	// 1. Uninstall only skill:hello (without --ignore) -> removed from all tools, rule & mcp remain.
	out, _, err := execCLI(t, projDir, "", "uninstall", "skill:hello")
	if err != nil {
		t.Fatalf("selective uninstall skill:hello failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(projDir, ".cursor", "skills", "hello")); !os.IsNotExist(err) {
		t.Fatal("cursor skill hello should be removed")
	}
	if _, err := os.Stat(filepath.Join(projDir, ".agents", "skills", "hello")); !os.IsNotExist(err) {
		t.Fatal("antigravity skill hello should be removed")
	}
	if _, err := os.Stat(filepath.Join(projDir, ".cursor", "mcp.json")); err != nil {
		t.Fatalf("mcp.json should remain intact: %v", err)
	}

	// 2. Uninstall mcp:fs --ignore -> removes fs and prevents pull from restoring it.
	out, _, err = execCLI(t, projDir, "", "uninstall", "mcp:fs", "--ignore")
	if err != nil {
		t.Fatalf("uninstall mcp:fs --ignore failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "added to ignore: mcp:fs") {
		t.Fatalf("expected ignore confirmation:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(projDir, ".cursor", "mcp.json")); !os.IsNotExist(err) {
		t.Fatal("empty cursor mcp.json should be removed")
	}

	// Pull restores skill:hello (not ignored), but does NOT restore mcp:fs (ignored).
	if _, _, err := execCLI(t, projDir, "", "pull", "--no-prompt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(projDir, ".cursor", "skills", "hello", "SKILL.md")); err != nil {
		t.Fatalf("non-ignored skill:hello should be restored by pull: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projDir, ".cursor", "mcp.json")); !os.IsNotExist(err) {
		t.Fatal("ignored mcp:fs must not be restored by pull")
	}

	// 3. `aiw ignore add rule:style` prunes dir rule AND strips block-only AGENTS.md.
	out, _, err = execCLI(t, projDir, "", "ignore", "add", "rule:style")
	if err != nil {
		t.Fatalf("ignore add rule:style failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(projDir, ".cursor", "rules", "style.mdc")); !os.IsNotExist(err) {
		t.Fatal("ignored cursor rule should be pruned immediately")
	}
	if _, err := os.Stat(filepath.Join(projDir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("block-only AGENTS.md should be stripped when only rule is ignored")
	}

	// 4. Ignored personal local skill is hidden from status & push.
	os.MkdirAll(filepath.Join(projDir, ".cursor", "skills", "personal"), 0o755)
	os.WriteFile(filepath.Join(projDir, ".cursor", "skills", "personal", "SKILL.md"), []byte("private\n"), 0o644)
	if _, _, err := execCLI(t, projDir, "", "ignore", "add", "skill:personal"); err != nil {
		t.Fatal(err)
	}
	out, _, err = execCLI(t, projDir, "", "status")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "to share:  0 added, 0 edited") {
		t.Fatalf("ignored personal skill should not show in status:\n%s", out)
	}

	// 5. Uninstall cli:fakecli --yes --ignore runs uninstall command AND makes doctor pass!
	out, _, err = execCLI(t, projDir, "", "uninstall", "cli:fakecli", "--yes", "--ignore")
	if err != nil {
		t.Fatalf("uninstall cli:fakecli failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Uninstalling fakecli:") || !strings.Contains(out, "uninstall-") {
		t.Fatalf("expected CLI uninstall output:\n%s", out)
	}
	out, _, err = execCLI(t, projDir, "", "doctor")
	if err != nil {
		t.Fatalf("doctor should pass after ignoring fakecli: err=%v\n%s", err, out)
	}
	if !strings.Contains(out, "1 ignored") {
		t.Fatalf("doctor should mention ignored CLI:\n%s", out)
	}

	// 6. `aiw ignore list` and `aiw ignore remove`
	out, _, err = execCLI(t, projDir, "", "ignore", "list")
	if err != nil || !strings.Contains(out, "mcp:fs") || !strings.Contains(out, "cli:fakecli") {
		t.Fatalf("ignore list: err=%v out=%s", err, out)
	}
	out, _, err = execCLI(t, projDir, "", "ignore", "remove", "mcp:fs")
	if err != nil || !strings.Contains(out, "un-ignored: mcp:fs") {
		t.Fatalf("ignore remove: err=%v out=%s", err, out)
	}
	if _, _, err := execCLI(t, projDir, "", "pull", "--no-prompt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(projDir, ".cursor", "mcp.json")); err != nil {
		t.Fatalf("un-ignored mcp:fs should be restored on pull: %v", err)
	}
}

func TestPullInteractiveCliPromptChoices(t *testing.T) {
	_, hubDir, projDir := setupIsolatedEnv(t)
	if _, _, err := execCLI(t, projDir, "", "init", hubDir, "--tools", "cursor", "--no-prompt"); err != nil {
		t.Fatal(err)
	}

	// 1. Choose [s]kip
	out, _, err := execCLI(t, projDir, "s\n", "pull", "--install-clis")
	if err != nil {
		t.Fatalf("pull prompt skip failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Action: [i]nstall, [s]kip, [g] ignore") || !strings.Contains(out, "skipped fakecli") {
		t.Fatalf("unexpected skip output:\n%s", out)
	}

	// 2. Choose [i]nstall
	out, _, err = execCLI(t, projDir, "i\n", "pull", "--install-clis")
	if err != nil {
		t.Fatalf("pull prompt install failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Installing fakecli:") || !strings.Contains(out, "install-") {
		t.Fatalf("unexpected install output:\n%s", out)
	}

	// 3. Choose [g] ignore -> persisted, so subsequent pull does not prompt and doctor passes
	out, _, err = execCLI(t, projDir, "g\n", "pull", "--install-clis")
	if err != nil {
		t.Fatalf("pull prompt ignore failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ignored cli:fakecli") {
		t.Fatalf("unexpected ignore output:\n%s", out)
	}
	out, _, err = execCLI(t, projDir, "", "pull", "--install-clis")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Missing CLI: fakecli") {
		t.Fatalf("ignored CLI must not prompt again:\n%s", out)
	}
	out, _, err = execCLI(t, projDir, "", "cli", "list")
	if err != nil || !strings.Contains(out, "ignored") {
		t.Fatalf("cli list should show ignored status:\n%s", out)
	}
}
