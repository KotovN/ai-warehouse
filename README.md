# ai-warehouse

**Make every team's AI setup shared and self-updating.** One git repo holds the team's skills, rules, MCP servers, CLI tools, and hooks; `aiwarehouse` distributes them into every member's AI tools and syncs local improvements back via pull requests.

Go reimplementation of the ideas behind [teamai-cli](https://github.com/Tencent/teamai-cli), scoped to core sync.

## Quick start

```bash
go install github.com/KotovN/ai-warehouse/cmd/aiwarehouse@latest   # or build from source
```

**Admin:** create a git repo (or let `aiw` create one on GitHub with `GITHUB_TOKEN` set), add resources, push:

```
warehouse.yaml            # team config: name, reviewers
skills/<name>/SKILL.md    # agent skills
rules/*.md                # team conventions
mcp/mcp.yaml              # shared MCP servers
clis/clis.yaml            # team CLI tools (check/install commands)
hooks/hooks.yaml          # lifecycle hooks
docs/                     # shared docs
```

Example `mcp/mcp.yaml`:

```yaml
servers:
  fs:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem"]
  search:
    url: https://mcp.example.com/sse
```

Example `clis/clis.yaml`:

```yaml
clis:
  gh:
    check: gh --version
    install: winget install GitHub.cli
```

**Members:** in any project directory:

```bash
aiw init https://github.com/your-org/your-repo            # attach existing repo
aiw init https://github.com/your-org/your-repo --scope user --tools cursor,codex
```

This clones the hub into `~/.ai-warehouse/projects/<slug>/team-repo` (business repos stay clean), installs skills/rules/MCP into the tools you chose, and injects a session-start hook that runs `aiw pull` automatically — every session starts with the team's latest setup.

## Supported tools

| Tool | Skills | Rules | MCP | Session hooks |
|---|---|---|---|---|
| Cursor | `.cursor/skills/` | `.cursor/rules/*.mdc` | `.cursor/mcp.json` | ✅ `hooks.json` |
| Codex | `.codex/skills/` | `AGENTS.md` block | `config.toml` | ✅ `hooks.json` |
| VS Code / Copilot | — | `.github/copilot-instructions.md` block | `.vscode/mcp.json` | — (manual `aiw pull`) |
| OpenCode | `.opencode/skills/` | `AGENTS.md` block | `opencode.json` | — (manual `aiw pull`) |

## Commands

| Command | What it does |
|---|---|
| `aiw init <repo-url>` | Attach a hub repo, clone, install, inject hooks. `--scope project\|user`, `--tools cursor,codex,vscode,opencode`, `--force` |
| `aiw pull` | Hub → local tools. `--dry-run`, `--quiet` (for hooks) |
| `aiw push` | Local additions/edits → branch `aiw/<user>` → PR (reuses your open PR). `--dry-run` |
| `aiw list [type]` | Inventory across tools; `type` = skills, mcp, clis, hooks, rules, docs |
| `aiw status` | Hub, last pull, doctor summary, to-share counts |
| `aiw doctor` | Diagnostics with fix hints; `--json` for CI; exit 1 on failure |
| `aiw uninstall` | Remove managed resources only; `--dry-run` |

Push flow: your changes are committed to `aiw/<username>` and force-pushed, so an existing PR from you is updated instead of duplicated. On non-API hosts (GitLab, etc.) the branch is pushed and you open the PR by hand. Secrets never go into the hub — `env` sharing is deliberately out of scope in v1.

## How state is tracked

Everything aiwarehouse writes is recorded in `~/.ai-warehouse/projects/<slug>/state.json` (`managed` entries: files, markdown blocks, MCP keys, hook entries). `uninstall` removes exactly those, prunes empty dirs, and preserves anything you created yourself.

## Development

```bash
go test ./...        # unit tests, no network needed
go build ./cmd/aiwarehouse
```

## Roadmap

- Interactive (bubbletea) `init` picker: create-vs-attach, tool multi-select
- GitHub repo creation via `aiw init` when the repo doesn't exist
- Claude Code adapter
- Rules/docs distribution polish
