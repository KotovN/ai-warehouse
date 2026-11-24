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

// CliStatus reports the presence and OS-specific install metadata of a team CLI.
type CliStatus struct {
	Name       string
	Installed  bool
	Check      string
	InstallCmd string
	URL        string
}

// InspectClis checks every registered CLI in sorted order and resolves the
// install command for goos (e.g. runtime.GOOS).
func InspectClis(clis config.ClisFile, goos string) []CliStatus {
	names := sortedCliKeys(clis.Clis)
	out := make([]CliStatus, 0, len(names))
	for _, name := range names {
		c := clis.Clis[name]
		installed := true
		if strings.TrimSpace(c.Check) != "" {
			installed = VerifyCli(c.Check)
		}
		out = append(out, CliStatus{
			Name:       name,
			Installed:  installed,
			Check:      strings.TrimSpace(c.Check),
			InstallCmd: c.Install.ForOS(goos),
			URL:        strings.TrimSpace(c.URL),
		})
	}
	return out
}

// CheckClis runs each registered CLI's check command, returning names not found.
func CheckClis(clis config.ClisFile) []string {
	var missing []string
	for _, st := range InspectClis(clis, runtime.GOOS) {
		if !st.Installed {
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
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", cmdStr)
	} else {
		cmd = exec.Command("sh", "-c", cmdStr)
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install %s (%q): %w", item.Name, cmdStr, err)
	}
	return nil
}

func sortedCliKeys(m map[string]config.CliTool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
