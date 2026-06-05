# skills

Native Go CLI for the open agent skills ecosystem.

This rewrite supports three targets:

| Agent | `--agent` | Project path | Global path |
| --- | --- | --- | --- |
| Claude Code | `claude-code` | `.claude/skills/` | `$CLAUDE_CONFIG_DIR/skills` or `~/.claude/skills` |
| Codex | `codex` | `.agents/skills/` | `$CODEX_HOME/skills` or `~/.codex/skills` |
| Cursor | `cursor` | `.agents/skills/` | `~/.cursor/skills` |

Project installs keep a canonical copy in `.agents/skills/<skill>`. Codex and Cursor read that directory directly. Claude Code receives a symlink from `.claude/skills/<skill>` when the project has a `.claude/` directory, with copy fallback when symlinks are unavailable.

Global installs keep a canonical copy in `~/.agents/skills/<skill>` and link/copy into each selected agent's global path.

## Build

```bash
go build -o goskill ./cmd/goskill
```

## Python Wheel

The Python package builds the Go CLI into a platform-specific wheel and exposes
the same `goskill` command as a console script.

```bash
python -m build --wheel
pip install dist/skills_cli-*.whl
```

If the `build` module is not installed, setuptools can build the wheel directly:

```bash
python setup.py bdist_wheel
```

## Test

```bash
go test ./...
```

If your Go build cache is not writable in a sandboxed environment:

```bash
GOCACHE=/tmp/go-cache go test ./...
```

## Release

Use the Makefile to keep Go, Python, and tag versions synchronized:

```bash
make check-version
make set-version VERSION=0.2.1
make release VERSION=0.2.1
```

`make release` commits the version update, creates an annotated `v0.2.1` tag,
and pushes the current branch plus tag to `origin`.

## Commands

```bash
goskill add <source>
goskill list
goskill remove [skills...]
goskill find <query>
goskill validate <skills>
goskill check
goskill update [skills...]
goskill init [name]
goskill install
goskill experimental_sync
```

Aliases:

- `goskill a` for `add`
- `goskill ls` for `list`
- `goskill rm` or `goskill r` for `remove`
- `goskill i` or `goskill experimental_install` for `install`
- `goskill upgrade` for `update`

## Add Sources

```bash
goskill add vercel-labs/agent-skills
goskill add https://github.com/vercel-labs/agent-skills
goskill add https://github.com/vercel-labs/agent-skills/tree/main/skills/find-skills
goskill add vercel-labs/agent-skills --skill find-skills
goskill add https://gitlab.com/org/repo
goskill add git@github.com:vercel-labs/agent-skills.git
goskill add ./my-local-skills
goskill add https://example.com
```

Supported source types:

- Local paths
- GitHub shorthand and URLs
- GitLab URLs
- Generic git URLs via `git clone`
- Well-known skills endpoints
- GitHub blob-download fast path via `skills.sh`, with git clone fallback

## Add Options

| Option | Description |
| --- | --- |
| `-g`, `--global` | Install globally |
| `-a`, `--agent <agents...>` | Target `claude-code`, `codex`, `cursor`, or `*` |
| `-s`, `--skill <skills...>` | Install named skills, or `*` for all |
| `-l`, `--list` | List available skills without installing |
| `-y`, `--yes` | Non-interactive confirmation flag |
| `--copy` | Copy files instead of symlinking |
| `--all` | Shorthand for `--skill '*' --agent '*' -y` |
| `--full-depth` | Search all subdirectories even when a root `SKILL.md` exists |

When a source contains multiple skills and `--skill` is not supplied, `goskill add` prompts for a numbered selection. Use `--skill <name>` for scripted installs, `--skill '*'` to install all skills, or `-y` to accept all discovered skills non-interactively.

## List

```bash
goskill list
goskill list -g
goskill list -a claude-code
goskill list --json
```

## Remove

```bash
goskill remove my-skill
goskill remove --all -y
goskill remove my-skill --agent claude-code
goskill remove my-skill --global
```

## Lock Files

The Go CLI preserves the existing lock formats:

- Project lock: `skills-lock.json`
- Global lock: `$XDG_STATE_HOME/skills/.skill-lock.json`, falling back to `~/.agents/.skill-lock.json`

Project lock files remain timestamp-free and sorted for stable diffs. Global lock files keep the v3 fields used for update checks, including `skillFolderHash`.

## Environment

| Variable | Description |
| --- | --- |
| `CLAUDE_CONFIG_DIR` | Overrides Claude Code global config directory |
| `CODEX_HOME` | Overrides Codex global config directory |
| `XDG_STATE_HOME` | Overrides global lock-file base directory |
| `GITHUB_TOKEN`, `GH_TOKEN` | Used for GitHub tree API requests |
| `GOSKILL_NO_UPDATE_CHECK` | Set to `1` to disable release update warnings |
| `GOSKILL_UPDATE_REPO` | Overrides the GitHub repository checked for releases |
| `SKILLS_DOWNLOAD_URL` | Overrides the blob download API base |
| `SKILLS_API_URL` | Overrides the search API base |
