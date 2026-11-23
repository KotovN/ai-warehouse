package config

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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

// ProjectSlug derives a stable partition directory name from a project root.
func ProjectSlug(projectRoot string) string {
	h := sha1.Sum([]byte(projectRoot))
	return fmt.Sprintf("%s-%s", filepath.Base(projectRoot), hex.EncodeToString(h[:4]))
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
