package cli

import (
	"fmt"

	"github.com/KotovN/ai-warehouse/internal/config"
	"github.com/KotovN/ai-warehouse/internal/hub"
	"github.com/KotovN/ai-warehouse/internal/tool"
	"github.com/spf13/cobra"
)

// runList prints an inventory across tools, optionally filtered by resource type.
func runList(cmd *cobra.Command, args []string) error {
	filter := ""
	if len(args) > 0 {
		filter = args[0]
		if !config.IsResourceType(filter) {
			return fmt.Errorf("unknown resource type %q, want one of: %v", filter, config.ResourceTypes)
		}
	}
	part, root, err := projectPartition()
	if err != nil {
		return err
	}
	for _, a := range tool.All(root) {
		inv, err := a.Inventory()
		if err != nil {
			return fmt.Errorf("%s inventory: %w", a.ID(), err)
		}
		if filter == "" || filter == "skills" {
			fmt.Printf("%-9s skills: %v\n", a.ID(), inv.Skills)
		}
		if filter == "" || filter == "mcp" {
			fmt.Printf("%-9s mcp servers: %v\n", a.ID(), inv.Servers)
		}
		if filter == "" || filter == "hooks" {
			fmt.Printf("%-9s session-start hook: %v\n", a.ID(), inv.HooksSet)
		}
	}
	res, err := hub.BuildResources(part.Repo)
	if err != nil {
		return err
	}
	if filter == "" || filter == "clis" {
		fmt.Printf("%-9s missing clis: %v\n", "cli", hub.CheckClis(res.Clis))
	}
	return nil
}

// projectPartition locates the active install and returns its partition and install root.
func projectPartition() (config.Partition, string, error) {
	root := initRoot("project")
	part := config.PartitionFor(root)
	cfg, err := config.Load(part.Config)
	if err != nil {
		return part, root, err
	}
	if cfg.Repo == "" {
		return part, root, fmt.Errorf("not initialized: run aiwarehouse init <repo-url> first")
	}
	if cfg.Scope == "user" {
		root = initRoot("user")
		part = config.PartitionFor(root)
	}
	return part, root, nil
}
