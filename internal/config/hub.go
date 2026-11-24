package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadWarehouse reads warehouse.yaml from the hub repo root.
func LoadWarehouse(repoPath string) (Warehouse, error) {
	return loadOne[Warehouse](filepath.Join(repoPath, "warehouse.yaml"))
}

// LoadMCP reads mcp/mcp.yaml from the hub repo root.
func LoadMCP(repoPath string) (MCPFile, error) {
	return loadOne[MCPFile](filepath.Join(repoPath, "mcp", "mcp.yaml"))
}

// SaveMCP writes mcp/mcp.yaml into the hub repo root.
func SaveMCP(repoPath string, m MCPFile) error {
	return saveOne(filepath.Join(repoPath, "mcp", "mcp.yaml"), m)
}

// LoadHooks reads hooks/hooks.yaml from the hub repo root.
func LoadHooks(repoPath string) (HooksFile, error) {
	return loadOne[HooksFile](filepath.Join(repoPath, "hooks", "hooks.yaml"))
}

// LoadClis reads clis/clis.yaml from the hub repo root.
func LoadClis(repoPath string) (ClisFile, error) {
	return loadOne[ClisFile](filepath.Join(repoPath, "clis", "clis.yaml"))
}

// SaveClis writes clis/clis.yaml into the hub repo root.
func SaveClis(repoPath string, c ClisFile) error {
	return saveOne(filepath.Join(repoPath, "clis", "clis.yaml"), c)
}

// SkillDirs lists skill directory names under skills/ in the hub repo.
func SkillDirs(repoPath string) []string {
	entries, err := os.ReadDir(filepath.Join(repoPath, "skills"))
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

func loadOne[T any](path string) (T, error) {
	var v T
	b, err := os.ReadFile(path)
	if err != nil {
		return v, err
	}
	if err := yaml.Unmarshal(b, &v); err != nil {
		return v, err
	}
	return v, nil
}

func saveOne(path string, v any) error {
	b, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
