package cli

import "github.com/spf13/cobra"

// New builds the aiwarehouse command tree.
func New() *cobra.Command {
	root := &cobra.Command{
		Use:          "aiwarehouse",
		Short:        "Sync team AI assets (skills, MCP, hooks) from a git hub repo into local AI tools",
		SilenceUsage: true,
	}
	root.AddCommand(newInit(), newPull(), newPush(), newList(), newStatus(), newDoctor(), newUninstall())
	return root
}

func newInit() *cobra.Command {
	var scope, tools string
	cmd := &cobra.Command{Use: "init [repo-url]", Short: "Create or attach a hub repo and set up local tools"}
	cmd.Flags().StringVar(&scope, "scope", "project", "install scope: project|user")
	cmd.Flags().StringVar(&tools, "tools", "", "comma-separated AI tools (cursor,codex,vscode,opencode), skips the picker")
	cmd.RunE = func(cmd *cobra.Command, args []string) error { return runInit(cmd, args, scope, tools) }
	return cmd
}

func newPull() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{Use: "pull", Short: "Sync hub resources into local AI tools"}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error { return runPull(cmd, dryRun) }
	return cmd
}

func newPush() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{Use: "push", Short: "Share local resources to the hub repo via pull request"}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be shared without pushing")
	cmd.RunE = func(c *cobra.Command, _ []string) error { return runPush(c, dryRun) }
	return cmd
}

func newList() *cobra.Command {
	return &cobra.Command{Use: "list [type]", Short: "Inventory skills/MCP/hooks across tools", Args: cobra.MaximumNArgs(1), RunE: runList}
}

func newStatus() *cobra.Command {
	return &cobra.Command{Use: "status", Short: "Show local vs hub resource diff", RunE: func(c *cobra.Command, _ []string) error { return runStatus(c) }}
}

func newDoctor() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{Use: "doctor", Short: "Diagnose configuration and tool issues"}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	cmd.RunE = func(c *cobra.Command, _ []string) error { return runDoctor(c, asJSON) }
	return cmd
}

func newUninstall() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{Use: "uninstall", Short: "Remove aiwarehouse-managed resources"}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be removed without writing")
	cmd.RunE = func(c *cobra.Command, _ []string) error { return runUninstall(c, dryRun) }
	return cmd
}
