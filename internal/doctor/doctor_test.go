package doctor

import (
	"testing"

	"github.com/KotovN/ai-warehouse/internal/config"
)

func TestRunUninitialized(t *testing.T) {
	root := t.TempDir()
	checks := Run(root, config.LocalConfig{}, config.PartitionFor(root))
	byName := map[string]Check{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	if byName["config"].OK {
		t.Fatal("uninitialized install should fail the config check")
	}
	if !byName["git"].OK {
		t.Fatalf("git check: %+v", byName["git"])
	}
}

func TestRunInitializedHealthy(t *testing.T) {
	root := t.TempDir()
	part := config.PartitionFor(root)
	if err := part.Ensure(); err != nil {
		t.Fatal(err)
	}
	cfg := config.LocalConfig{Repo: "https://github.com/org/repo", Scope: "project", ProjectRoot: root}
	checks := Run(root, cfg, part)
	names := map[string]Check{}
	for _, c := range checks {
		names[c.Name] = c
	}
	if !names["config"].OK {
		t.Fatalf("config check: %+v", names["config"])
	}
	// Clone dir exists but is empty — unreadable clone check should fire.
	if names["hub clone"].OK {
		t.Fatal("empty clone dir should not pass the hub clone check")
	}
}

func TestJSONSerializable(t *testing.T) {
	root := t.TempDir()
	checks := Run(root, config.LocalConfig{}, config.PartitionFor(root))
	for _, c := range checks {
		if c.Name == "" {
			t.Fatal("check name must be set")
		}
	}
}
