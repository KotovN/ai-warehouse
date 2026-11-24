# ai-warehouse

**Make every team's AI setup shared and self-updating.** One git repo holds the team's skills, rules, MCP servers, CLI tools, custom IDE definitions, and hooks; `aiw` (`aiwarehouse`) distributes them into every member's AI tools and syncs local improvements back via pull requests.

## Quick start

```bash
go install github.com/KotovN/ai-warehouse/cmd/aiwarehouse@latest   # or download `aiw` from Releases
```

**Admin:** create a git repo, add resources, push:

```text
warehouse.yaml            # team config: name, reviewers, autoPull
skills/<name>/SKILL.md    # agent skills
rules/*.md                # team conventions
mcp/mcp.yaml              # shared MCP servers
clis/clis.yaml            # team CLI tools (check, url, per-OS install commands)
tools/<id>.yaml           # optional custom IDE/tool specs (or preset overrides)
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

Example `clis/clis.yaml` (supports a single string or per-OS `windows`/`darwin`/`linux` map for both `install` and optional `uninstall`, plus `url`):

```yaml
clis:
  gh:
    check: gh --version
    url: https://cli.github.com/
    install:
      windows: winget install --id GitHub.cli -e
      darwin: brew install gh
      linux: sudo apt install gh
    uninstall:
      windows: winget uninstall --id GitHub.cli
      darwin: brew uninstall gh
  hex:
    check: hex --version
    url: https://hex.pm/docs/usage
    install: mix local.hex --force
    uninstall: mix archive.uninstall hex --force
```

**Members:** in any project directory:

```bash
aiw init https://github.com/your-org/your-repo            # attach existing repo
aiw init https://github.com/your-org/your-repo --scope user --tools cursor,antigravity
```

This clones the hub into `~/.ai-warehouse/projects/<slug>/team-repo` (business repos stay clean), installs skills/rules/MCP into the tools you chose, prunes resources deleted from the hub on subsequent pulls, and injects a session-start hook (`aiw pull --quiet`) when supported and `autoPull` is enabled.

## Supported tools (declarative `ToolSpec`)

All tools are driven by declarative YAML specs rather than hardcoded IDE adapters. Six built-in presets are embedded:

| Tool | Skills | Rules | MCP | Session hooks |
|---|---|---|---|---|
| **Cursor** (`cursor`) | `.cursor/skills/` | `.cursor/rules/*.mdc` | `.cursor/mcp.json` | ✅ `hooks.json` |
| **Codex** (`codex`) | `.codex/skills/` | `AGENTS.md` block | `.codex/config.toml` | ✅ `hooks.json` |
| **VS Code / Copilot** (`vscode`) | — | `.github/copilot-instructions.md` block | `.vscode/mcp.json` | — (manual `aiw pull`) |
| **OpenCode** (`opencode`) | `.opencode/skills/` | `AGENTS.md` block | `opencode.json` | — (manual `aiw pull`) |
| **Claude Code** (`claude`) | `.claude/skills/` | `CLAUDE.md` block | `.mcp.json` | ✅ `.claude/settings.json` |
| **Antigravity** (`antigravity`) | `.agents/skills/` | `.agents/rules/*.md` | `.agents/mcp_config.json` | ✅ `.agents/hooks.json` |

### Adding any other IDE without code changes

Drop a YAML spec into `<hub-repo>/tools/<id>.yaml` (shared with the whole team) or `~/.ai-warehouse/tools/<id>.yaml` (machine-local):

```yaml
id: windsurf
detect:
  - .windsurf
  - ~/.codeium/windsurf
project:
  skills: .windsurf/skills
  rules:
    mode: dir            # "dir" (one file per rule) or "block" (managed markdown block)
    path: .windsurf/rules
    ext: .md
  mcp:
    path: .windsurf/mcp.json
    format: json-standard # json-standard | json-opencode | toml
    key: mcpServers
```

## Commands

| Command | What it does |
|---|---|
| `aiw init <repo-url>` | Attach a hub repo, clone, install, inject hooks. `--scope project\|user`, `--tools <ids>`, `--force`, `--no-prompt`, `--yes` |
| `aiw pull` | Hub → local tools (prunes deleted/ignored assets; interactively prompts `[i]nstall / [s]kip / [g] ignore` for missing CLIs in a TTY). `--dry-run`, `--quiet`/`-q`, `--no-prompt`, `--install-clis`, `--yes`/`-y` |
| `aiw push [items...]` | Share local skills, rules, or MCP servers → branch `aiw/<user>` → PR. Supports `skill:<name>`, `rule:<name>`, `mcp:<name>`, `--type`, `--from`, `-i`/`--interactive`, `--dry-run` |
| `aiw cli list` | Show registered team CLIs, presence/ignored status, OS install command, and URL |
| `aiw cli install [name...]` | Install missing (or specific) CLIs (`[i]nstall / [s]kip / [g] ignore`, `--yes`, `--dry-run`) |
| `aiw cli add <name>` | Register a CLI (`--check`, `--install*`, `--uninstall*`, `--url`) and share via PR |
| `aiw ignore add\|remove\|list` | Ignore specific `skill:<n>`, `rule:<n>`, `mcp:<n>`, or `cli:<n>` on this machine (immediately prunes ignored local managed entries) |
| `aiw list [type]` | Inventory across active tools; `type` = `skills`, `rules`, `mcp`, `hooks`, `clis`, `docs` |
| `aiw status` | Hub, scope, ignored list, last pull, doctor summary, and concrete to-share items |
| `aiw doctor` | Diagnostics with fix hints (skips ignored CLIs); `--json` for CI; exit 1 on failure |
| `aiw uninstall [items...]` | Remove all managed resources, or concrete `skill:<n>`, `rule:<n>`, `mcp:<n>`, `cli:<n>` items (`--ignore`, `--yes`, `--dry-run`) |

Push flow: selected changes are committed to `aiw/<username>` and force-pushed, updating your open PR instead of opening duplicates. On non-API hosts (GitLab, etc.) the branch is pushed cleanly and you open the PR by hand. **Secrets never go into the hub** — `env` and `headers` are automatically stripped when sharing local MCP servers.

## How state is tracked

Everything `aiw` writes is recorded as JSON in `~/.ai-warehouse/projects/<slug>/state.json` (`managed` entries: files, markdown blocks, MCP keys, hook entries). Both `pull` (when hub items are deleted) and `uninstall` revert only those managed entries while preserving user-created content.

## Development

```bash
go vet ./...
go test -v ./...     # fully isolated unit & CLI tests, no network needed
go build -o aiw ./cmd/aiwarehouse
```
