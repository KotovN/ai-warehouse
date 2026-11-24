package config

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Partition holds the paths of one project's machine-data directory.
type Partition struct {
	Root     string // e.g. ~/.ai-warehouse/projects/my-project-3f2a
	Repo     string // clone of the hub repo
	Worktree string // disposable fresh clone used by push
	Config   string // config.yaml path
	State    string // state.json path
}

// Home returns the machine data home (~/.ai-warehouse).
func Home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ".ai-warehouse"
	}
	return filepath.Join(h, ".ai-warehouse")
}

// UserToolsDir returns the directory for user-defined custom tool specs (~/.ai-warehouse/tools).
func UserToolsDir() string {
	return filepath.Join(Home(), "tools")
}

// ProjectSlug derives a stable partition directory name from a project root.
func ProjectSlug(projectRoot string) string {
	clean := filepath.Clean(projectRoot)
	h := sha1.Sum([]byte(clean))
	base := filepath.Base(clean)
	if base == "" || base == "." || base == string(filepath.Separator) || base == "/" || strings.ContainsAny(base, `/\:`) {
		base = "root"
	}
	return fmt.Sprintf("%s-%s", base, hex.EncodeToString(h[:4]))
}

// PartitionFor computes (but does not create) the partition paths for a project root.
func PartitionFor(projectRoot string) Partition {
	root := filepath.Join(Home(), "projects", ProjectSlug(projectRoot))
	return Partition{
		Root:     root,
		Repo:     filepath.Join(root, "team-repo"),
		Worktree: filepath.Join(root, "knowledge-wt"),
		Config:   filepath.Join(root, "config.yaml"),
		State:    filepath.Join(root, "state.json"),
	}
}

// Ensure creates the partition directory (the clone dir is created on demand).
func (p Partition) Ensure() error {
	return os.MkdirAll(p.Root, 0o755)
}

// RemoveAll deletes path and any children, clearing read-only bits first so
// directories containing git object/pack files (0444) remove cleanly on Windows.
func RemoveAll(path string) error {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return nil
	}
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			_ = os.Chmod(p, 0o755)
		} else {
			_ = os.Chmod(p, 0o644)
		}
		return nil
	})
	return os.RemoveAll(path)
}
