package cli

import "github.com/spf13/cobra"

// New builds the aiw command tree.
func New() *cobra.Command {
	root := &cobra.Command{
		Use:          "aiw",
		Aliases:      []string{"aiwarehouse"},
		Short:        "Sync team AI assets (skills, rules, MCP, CLIs, hooks) from a git hub repo into local AI tools",
		SilenceUsage: true,
	}
	root.AddCommand(
		newInit(),
		newPull(),
		newPush(),
		newCli(),
		newList(),
		newStatus(),
		newDoctor(),
		newUninstall(),
	)
	return root
}

func newInit() *cobra.Command {
	var scope, tools string
	var force bool
	cmd := &cobra.Command{Use: "init [repo-url]", Short: "Attach a hub repo and set up local tools"}
	cmd.Flags().StringVar(&scope, "scope", "project", "install scope: project|user")
	cmd.Flags().StringVar(&tools, "tools", "", "comma-separated AI tools (e.g. cursor,codex,vscode,opencode,claude,antigravity)")
	cmd.Flags().BoolVar(&force, "force", false, "re-clone the hub repo even if already cloned")
	cmd.RunE = func(cmd *cobra.Command, args []string) error { return runInit(cmd, args, scope, tools, force) }
	return cmd
}

func newPull() *cobra.Command {
	var dryRun, quiet, installClis, autoYes bool
	cmd := &cobra.Command{Use: "pull", Short: "Sync hub resources into local AI tools"}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing")
	cmd.Flags().BoolVarP(&quiet, "quiet", "q", false, "suppress output on success (used by session-start hooks)")
	cmd.Flags().BoolVar(&installClis, "install-clis", false, "prompt to install missing team CLIs after pulling")
	cmd.Flags().BoolVarP(&autoYes, "yes", "y", false, "skip confirmation prompts when installing CLIs")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error { return runPull(cmd, dryRun, quiet, installClis, autoYes) }
	return cmd
}

func newPush() *cobra.Command {
	var dryRun, interactive bool
	var types, fromTool string
	cmd := &cobra.Command{
		Use:   "push [items...]",
		Short: "Share local resources to the hub repo via pull request",
		Long:  "Share local skills, rules, and MCP servers to the hub repo via PR.\nPass specific items (e.g. skill:eli5 rule:style mcp:fs or eli5), filter with --type, or use -i to pick interactively.",
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be shared without pushing")
	cmd.Flags().StringVar(&types, "type", "", "comma-separated resource types to push (skills,rules,mcp)")
	cmd.Flags().StringVar(&fromTool, "from", "", "scan changes only from a specific tool ID (e.g. cursor, antigravity)")
	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "interactively pick which local changes to push")
	cmd.RunE = func(c *cobra.Command, args []string) error {
		return runPush(c, args, pushFlags{dryRun: dryRun, types: types, fromTool: fromTool, interactive: interactive})
	}
	return cmd
}

func newCli() *cobra.Command {
	cmd := &cobra.Command{Use: "cli", Short: "Manage and install team CLI tools from clis/clis.yaml"}
	cmd.AddCommand(newCliList(), newCliInstall(), newCliAdd())
	return cmd
}

func newCliList() *cobra.Command {
	return &cobra.Command{Use: "list", Short: "List registered team CLIs, status, and install commands", RunE: runCliList}
}

func newCliInstall() *cobra.Command {
	var dryRun, autoYes bool
	cmd := &cobra.Command{Use: "install [name...]", Short: "Install missing (or specified) team CLIs"}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show commands that would run without executing them")
	cmd.Flags().BoolVarP(&autoYes, "yes", "y", false, "skip confirmation prompts")
	cmd.RunE = func(c *cobra.Command, args []string) error { return runCliInstall(c, args, dryRun, autoYes) }
	return cmd
}

func newCliAdd() *cobra.Command {
	var opts cliAddOpts
	cmd := &cobra.Command{Use: "add <name>", Short: "Register a team CLI in clis/clis.yaml and share via PR", Args: cobra.ExactArgs(1)}
	cmd.Flags().StringVar(&opts.check, "check", "", "command to verify presence (e.g. \"hex --version\")")
	cmd.Flags().StringVar(&opts.url, "url", "", "optional download or documentation URL")
	cmd.Flags().StringVar(&opts.installDefault, "install", "", "single cross-platform install command")
	cmd.Flags().StringVar(&opts.installWindows, "install-windows", "", "Windows install command")
	cmd.Flags().StringVar(&opts.installDarwin, "install-darwin", "", "macOS install command")
	cmd.Flags().StringVar(&opts.installLinux, "install-linux", "", "Linux install command")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "preview without pushing")
	cmd.RunE = func(c *cobra.Command, args []string) error { return runCliAdd(c, args[0], opts) }
	return cmd
}

func newList() *cobra.Command {
	return &cobra.Command{Use: "list [type]", Short: "Inventory skills/rules/MCP/hooks/CLIs/docs across tools", Args: cobra.MaximumNArgs(1), RunE: runList}
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
	cmd := &cobra.Command{Use: "uninstall", Short: "Remove aiw-managed resources"}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be removed without writing")
	cmd.RunE = func(c *cobra.Command, _ []string) error { return runUninstall(c, dryRun) }
	return cmd
}
