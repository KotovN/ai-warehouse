package hub

import (
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"github.com/KotovN/ai-warehouse/internal/config"
)

// CliStatus reports the presence and OS-specific install/uninstall metadata of a team CLI.
type CliStatus struct {
	Name         string
	Installed    bool
	Ignored      bool
	Check        string
	InstallCmd   string
	UninstallCmd string
	URL          string
}

// InspectClis checks every registered CLI in sorted order and resolves the
// install/uninstall commands for goos (e.g. runtime.GOOS).
func InspectClis(clis config.ClisFile, goos string) []CliStatus {
	return InspectClisWithIgnore(clis, goos, nil)
}

// InspectClisWithIgnore checks every registered CLI and marks those listed in ignore.
func InspectClisWithIgnore(clis config.ClisFile, goos string, ignore []string) []CliStatus {
	names := sortedCliKeys(clis.Clis)
	out := make([]CliStatus, 0, len(names))
	for _, name := range names {
		c := clis.Clis[name]
		ignored := config.IsIgnored(ignore, "cli", name)
		installed := true
		if !ignored && strings.TrimSpace(c.Check) != "" {
			installed = VerifyCli(c.Check)
		}
		out = append(out, CliStatus{
			Name:         name,
			Installed:    installed,
			Ignored:      ignored,
			Check:        strings.TrimSpace(c.Check),
			InstallCmd:   c.Install.ForOS(goos),
			UninstallCmd: c.Uninstall.ForOS(goos),
			URL:          strings.TrimSpace(c.URL),
		})
	}
	return out
}

// CheckClis runs each registered CLI's check command, returning names not found.
func CheckClis(clis config.ClisFile) []string {
	var missing []string
	for _, st := range InspectClis(clis, runtime.GOOS) {
		if !st.Installed && !st.Ignored {
			missing = append(missing, st.Name)
		}
	}
	return missing
}

// VerifyCli executes a CLI check command and reports whether it exits 0.
func VerifyCli(check string) bool {
	fields := strings.Fields(check)
	if len(fields) == 0 {
		return true
	}
	return exec.Command(fields[0], fields[1:]...).Run() == nil
}

// InstallCli runs item's resolved install command using the platform shell,
// streaming output to stdout/stderr.
func InstallCli(item CliStatus, stdout, stderr io.Writer) error {
	cmdStr := strings.TrimSpace(item.InstallCmd)
	if cmdStr == "" {
		if item.URL != "" {
			return fmt.Errorf("no install command configured for %s on %s; see %s", item.Name, runtime.GOOS, item.URL)
		}
		return fmt.Errorf("no install command configured for %s on %s", item.Name, runtime.GOOS)
	}
	if err := runShellCmd(cmdStr, stdout, stderr); err != nil {
		return fmt.Errorf("install %s (%q): %w", item.Name, cmdStr, err)
	}
	return nil
}

// UninstallCli runs item's resolved uninstall command using the platform shell.
func UninstallCli(item CliStatus, stdout, stderr io.Writer) error {
	cmdStr := strings.TrimSpace(item.UninstallCmd)
	if cmdStr == "" {
		return fmt.Errorf("no uninstall command configured for %s on %s in clis/clis.yaml", item.Name, runtime.GOOS)
	}
	if err := runShellCmd(cmdStr, stdout, stderr); err != nil {
		return fmt.Errorf("uninstall %s (%q): %w", item.Name, cmdStr, err)
	}
	return nil
}

func runShellCmd(cmdStr string, stdout, stderr io.Writer) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", cmdStr)
	} else {
		cmd = exec.Command("sh", "-c", cmdStr)
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func sortedCliKeys(m map[string]config.CliTool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
