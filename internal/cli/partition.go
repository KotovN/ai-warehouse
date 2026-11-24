package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/KotovN/ai-warehouse/internal/config"
)

// initRoot returns the install root for a scope: home for user, cwd for project.
func initRoot(scope string) (string, error) {
	if scope == "user" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		return filepath.Clean(h), nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working dir: %w", err)
	}
	return filepath.Clean(wd), nil
}

// activeInstall locates the active ai-warehouse installation by walking up
// from the current working directory (project scope), then falling back to
// the user home partition (user scope).
func activeInstall() (config.Partition, string, config.LocalConfig, error) {
	wd, err := os.Getwd()
	if err != nil {
		return config.Partition{}, "", config.LocalConfig{}, fmt.Errorf("resolve working dir: %w", err)
	}
	wd = filepath.Clean(wd)

	// 1. Walk upward from cwd to find a project-scoped partition.
	for dir := wd; ; {
		part := config.PartitionFor(dir)
		cfg, err := config.Load(part.Config)
		if err != nil {
			return part, dir, cfg, err
		}
		if cfg.Repo != "" {
			if cfg.Scope == "user" {
				home, herr := initRoot("user")
				if herr == nil {
					return config.PartitionFor(home), home, cfg, nil
				}
			}
			return part, dir, cfg, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// 2. Fall back to user-scoped partition under $HOME.
	if home, err := initRoot("user"); err == nil {
		part := config.PartitionFor(home)
		cfg, err := config.Load(part.Config)
		if err != nil {
			return part, home, cfg, err
		}
		if cfg.Repo != "" {
			return part, home, cfg, nil
		}
	}

	return config.PartitionFor(wd), wd, config.LocalConfig{}, fmt.Errorf("not initialized: run aiw init <repo-url> first")
}

func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
