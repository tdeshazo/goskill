# AGENTS.md

Guidance for coding agents working on the Go `goskill` CLI.

## Project Overview

`goskill` is a native Go CLI for installing agent skills. The current implementation intentionally supports only Claude Code, Codex, and Cursor targets.

## Commands

| Command | Description |
| --- | --- |
| `goskill` | Show banner with available commands |
| `goskill add <source>` | Install goskill from git repos, URLs, well-known endpoints, or local paths |
| `goskill install` | Restore project skills from `skills-lock.json` |
| `goskill experimental_sync` | Sync skills from `node_modules` into agent dirs |
| `goskill list` | List installed skills |
| `goskill remove [skills...]` | Remove installed skills |
| `goskill find <query>` | Search skills through the skills API |
| `goskill check` | Check locked skills for updates |
| `goskill update [skills...]` | Update locked skills |
| `goskill init [name]` | Create a new `SKILL.md` template |

Aliases: `a`, `ls`, `rm`, `r`, `i`, `experimental_install`, and `upgrade`.

## Architecture

```text
cmd/goskill/main.go          # CLI entrypoint
internal/commands/          # Command routing and command implementations
internal/agents/            # Claude Code, Codex, Cursor target definitions
internal/source/            # Source string parsing
internal/skills/            # SKILL.md parsing, discovery, hashing, sanitization
internal/installer/         # Canonical install, symlink/copy, list, remove
internal/lockfile/          # Project/global lock-file compatibility
internal/github/            # Git clone, GitHub tree API, skills.sh blob downloads
internal/wellknown/         # /.well-known/agent-skills support
internal/terminal/          # Terminal output sanitization
```

## Development

```bash
# Build
go build -o goskill ./cmd/goskill

# Run locally
go run ./cmd/goskill add vercel-labs/agent-skills --list
go run ./cmd/goskill experimental_sync
go run ./cmd/goskill check
go run ./cmd/goskill update
go run ./cmd/goskill init my-skill

# Run tests
go test ./...
```

If the default Go build cache is not writable, use:

```bash
GOCACHE=/tmp/go-cache go test ./...
```

## Agent Targets

Only these agents are valid:

- `claude-code`
- `codex`
- `cursor`

`--agent '*'` expands to all three.

## Compatibility

Keep these compatibility guarantees unless the user explicitly asks for a breaking change:

- Project installs use `.agents/skills/<skill>` as the canonical path.
- Claude Code project installs symlink from `.claude/skills/<skill>` when `.claude/` exists.
- Project lock file remains `skills-lock.json` with version `1`.
- Global lock file remains `$XDG_STATE_HOME/skills/.skill-lock.json` or `~/.agents/.skill-lock.json` with version `3`.
- Skill names and paths must be sanitized to prevent traversal.

## Code Style

Use standard Go formatting.

```bash
gofmt -w cmd internal
go test ./...
```
