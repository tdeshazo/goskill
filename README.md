# skills

Native Go CLI for the open agent skills ecosystem.

Built-in agent definitions support three targets. They can be extended or
overridden with declarative user definitions:

| Agent | `--agent` | Project path | Global path |
| --- | --- | --- | --- |
| Claude Code | `claude-code` | `.claude/skills/` | `$CLAUDE_CONFIG_DIR/skills` or `~/.claude/skills` |
| Codex | `codex` | `.agents/skills/` | `$CODEX_HOME/skills` or `~/.codex/skills` |
| Cursor | `cursor` | `.agents/skills/` | `~/.cursor/skills` |

Project installs keep a canonical copy in `.agents/skills/<skill>`. Codex and Cursor read that directory directly. Claude Code receives a symlink from `.claude/skills/<skill>` when the project has a `.claude/` directory, with copy fallback when symlinks are unavailable.

Global installs keep a canonical copy in `~/.agents/skills/<skill>` and link/copy into each selected agent's global path.

## Agent registry

Agent definitions are versioned YAML. Built-ins for Claude Code, Codex, and
Cursor are embedded in the executable. User files in
`goskill/agents/*.yaml` below the platform user-config directory are loaded in
lexical filename order and override a built-in with the same `id`. On Linux,
the default directory is `$XDG_CONFIG_HOME/goskill/agents` (or
`~/.config/goskill/agents`); macOS and Windows use Go's platform
`UserConfigDir` locations. Two user files may not declare the same id.

```yaml
version: 1
agents:
  - id: opencode
    display_name: OpenCode
    project_skills_dir: .opencode/skills
    global_skills_dir: ~/.opencode/skills
    detect_paths:
      - ~/.opencode
      - "{project}/.opencode"
    command: opencode
```

All fields other than `id`, `display_name`, `project_skills_dir`, and
`global_skills_dir` are optional. Paths are expanded directly without a shell.

| Field | Type / default | Constraint and behavior |
| --- | --- | --- |
| `id` | required string | Lowercase letters, digits, and hyphens; identifies the agent. |
| `display_name` | required string | Human-readable agent name. |
| `project_skills_dir` | required relative path | Must remain below the project directory. |
| `global_skills_dir` | required absolute or `~/` path | Used for global installs unless `global_config_env` is set. |
| `global_config_env` | optional environment-variable name | When non-empty, its configured directory overrides `global_skills_dir` for global installs. |
| `global_config_suffix` | optional relative path, default empty | Appended to the directory from `global_config_env`; it must remain relative. |
| `detect_paths` | optional list, default empty | Each path is absolute, `~/`, `{project}`, or `{project}/...`; existing paths identify installed agents. |
| `universal_project` | boolean, default `false` | Uses the canonical `.agents/skills` project location instead of `project_skills_dir`. |
| `project_guard_dir` | optional relative path, default empty | Makes the project target apply only when this project directory exists. |
| `command` | optional string | Required by `goskill use --agent <id>` to launch the agent. |

User definitions take precedence over built-ins with the same `id`; duplicate
user `id` values are rejected.

```bash
goskill agent list
goskill agent show opencode
goskill agent validate ~/.config/goskill/agents/opencode.yaml
goskill add owner/repo --agent opencode
```

## Build

```bash
go install github.com/tdeshazo/goskill@latest
```

To build the current checkout instead:

```bash
go build -o goskill .
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
go vet ./...
```

If your Go build cache is not writable in a sandboxed environment:

```bash
GOCACHE=/tmp/go-cache go test ./...
```

`go vet ./...` is the project's reproducible lint gate and runs in CI. We do
not currently claim `staticcheck` as a release gate: released staticcheck
versions can lag the Go toolchain's export-data format. Run it locally only
when the installed staticcheck version supports this project's Go version.

## Release

Use the Makefile to keep Go, Python, and tag versions synchronized:

```bash
make lint
make check-version
make set-version VERSION=0.2.1
make release VERSION=0.2.1
```

`make lint` runs `go vet ./...`, the release lint gate. `make release` commits
the version update, creates an annotated `v0.2.1` tag, and pushes the current
branch plus tag to `origin`.

## Commands

```bash
goskill add <source>
goskill use <source>[@<skill>]
goskill list
goskill remove [skills...]
goskill find <query>
goskill find --deep <query> # Also search GitHub's long tail
goskill find --refresh <query> # Refresh static catalogs before searching
goskill find --verified --sort popular --provider skillmd <query>
goskill find --json <query>
goskill find --providers # Show built-in and configured optional registries
goskill agent list
goskill validate <skills>
goskill check
goskill update [skills...]
goskill init [name]
goskill install
goskill sync
```

Aliases:

- `goskill a` for `add`
- `goskill ls` for `list`
- `goskill rm` or `goskill r` for `remove`
- `goskill i` for `install`
- `goskill upgrade` for `update`
- `experimental_install` and `experimental_sync` remain accepted as legacy aliases

## Find options

`goskill find` searches all enabled registries concurrently, deduplicates
equivalent source skills, then filters and ranks the normalized results.

| Option | Description |
| --- | --- |
| `--deep` | Opt in to GitHub long-tail discovery. |
| `--refresh` | Refresh static catalogs before searching. |
| `--verified` | Keep results with a positive verification or audit signal. |
| `--provider <name>` | Keep results contributed by one provider, including merged provenance. |
| `--sort relevance\|popular\|newest` | Choose deterministic relevance (default), adoption, or freshness ordering. |
| `--json` | Write normalized results and provider statuses as ANSI-free JSON. |
| `--providers` | Show provider capabilities and configured availability without searching. |

When a provider fails but other providers succeed, ordinary results remain
available. Human-readable output calls out provider failures only when they may
explain an otherwise empty result set; JSON includes per-provider status.

## Federated find

The default `find` search uses these providers concurrently:

| Provider | Default behavior | Data and cost |
| --- | --- | --- |
| skills.sh | Enabled | Live registry search; prefers its richer optional-auth endpoint and falls back to the compatibility endpoint. |
| SkillMD | Enabled unless `GOSKILL_DISABLE_SKILLMD=1` | Live public registry search. |
| TrueFoundry awesome-skills-registry | Enabled unless `GOSKILL_DISABLE_TRUEFOUNDRY_CATALOG=1` | Cached static catalog searched locally. |
| GitHub | Only with `--deep` | Bounded code search plus validation of a small number of `SKILL.md` candidates. |
| Configured well-known endpoints | Only when configured | Explicit domains only; no query-driven domain crawling. |

`--deep` is opt-in because GitHub code search has tighter rate limits and a
higher request cost. GitHub uses `GITHUB_TOKEN` or `GH_TOKEN` when available;
search still works without one when GitHub permits the request. skills.sh uses
`SKILLS_API_TOKEN` or `VERCEL_OIDC_TOKEN` only for its richer endpoint, then
tries its existing unauthenticated compatibility endpoint. SkillMD search is
public. No credential is printed in results, JSON statuses, or errors.

TrueFoundry catalog snapshots are stored under the platform cache directory
(`GOSKILL_CATALOG_CACHE_DIR` overrides it) and are fresh for 24 hours by
default (`GOSKILL_CATALOG_TTL`). Use `--refresh` to fetch now. If refresh
fails, a valid stale snapshot remains searchable, so offline and transient
outage behavior does not discard cached results. GitHub keeps identical deep
queries in a per-process cache.

The compact renderer shows source, provider provenance, and only available
signals such as installs, stars, rating, trust, and update date. `--json` is
for automation: it emits full normalized results and provider outcomes without
ANSI escape sequences. See [the search architecture](docs/search-architecture.md)
for the ranking and extension contract.

### Optional enterprise registries

Optional registries are never discovered from a search term. Configure each
explicitly in the platform user configuration directory returned by
`os.UserConfigDir`, in `goskill/providers.json` (for example,
`$XDG_CONFIG_HOME/goskill/providers.json` on Unix), or set
`GOSKILL_PROVIDER_CONFIG` to a JSON file, or `GOSKILL_PROVIDER_CONFIG_JSON` to
the JSON itself). The first supported adapter is a bounded Agent Skills
well-known index; it requests only that configured endpoint's
`.well-known/agent-skills/index.json` or compatibility `.well-known/skills/index.json`.

```json
{
  "providers": [
    {
      "name": "acme-skills",
      "kind": "well-known",
      "endpoint": "https://skills.acme.example",
      "enabled": true,
      "visibility": "private",
      "auth_required": true,
      "credential_env": "ACME_SKILLS_TOKEN"
    }
  ]
}
```

Private provider kinds declare `credential_env` instead of a token. Inline
credentials (`token`, `secret`, `password`, or `authorization`) are rejected;
credentials are obtained only through environment or future credential
resolvers. Unknown, disabled, unavailable, or unauthenticated optional
providers do not prevent other registries from returning search results.

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
| `-a`, `--agent <agents...>` | Target a configured agent id, or `*` |
| `-s`, `--skill <skills...>` | Install named skills, or `*` for all |
| `-l`, `--list` | List available skills without installing |
| `-y`, `--yes` | Non-interactive confirmation flag |
| `--copy` | Copy files instead of symlinking |
| `--all` | Shorthand for `--skill '*' --agent '*' -y` |
| `--full-depth` | Search all subdirectories even when a root `SKILL.md` exists |

When a source contains multiple skills and `--skill` is not supplied, `goskill add` prompts for a numbered selection. Use `--skill <name>` for scripted installs, `--skill '*'` to install all skills, or `-y` to accept all discovered skills non-interactively.

## Use Without Installing

`goskill use` resolves a source like `goskill add`, materializes exactly one
skill in a temporary directory, and turns its `SKILL.md` into a prompt for a
single agent invocation. It does not install the skill or update a lock file.

```bash
goskill use vercel-labs/agent-skills@web-design-guidelines | claude
goskill use vercel-labs/agent-skills --skill web-design-guidelines
goskill use vercel-labs/agent-skills@web-design-guidelines --agent codex
```

Without `--agent`, stdout contains only the generated prompt. The option
`--agent claude-code` launches `claude` and `--agent codex` launches `codex`;
custom agents with a `command` are launched the same way, with the prompt
passed as the initial interactive request. If a source contains
multiple skills, select one with the `@skill` suffix or `--skill`; `use` never
chooses one arbitrarily.

| Option | Description |
| --- | --- |
| `-s`, `--skill <skill>` | Select exactly one skill |
| `-a`, `--agent <agent>` | Launch one configured agent with a `command` interactively |
| `--full-depth` | Search all nested directories, as with `add` |
| `-h`, `--help` | Show command help |

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
| `GITHUB_TOKEN`, `GH_TOKEN` | Authenticate GitHub code search for `find --deep`, GitHub tree/blob API requests, and release update checks |
| `GOSKILL_NO_UPDATE_CHECK` | Set to `1` to disable release update warnings |
| `GOSKILL_UPDATE_REPO` | Overrides the GitHub repository checked for releases |
| `SKILLS_DOWNLOAD_URL` | Overrides the blob download API base |
| `SKILLS_API_URL` | Overrides the skills.sh search API base |
| `SKILLS_RICH_SEARCH_URL` | Overrides the preferred skills.sh v1 search endpoint |
| `SKILLS_SEARCH_URL` | Overrides the compatibility skills.sh search endpoint |
| `SKILLS_API_TOKEN` | Optional bearer token for the richer skills.sh endpoint |
| `VERCEL_OIDC_TOKEN` | Fallback optional bearer token for the richer skills.sh endpoint |
| `SKILLMD_API` | Overrides the SkillMD API base (default `https://api.skillmd.com`) |
| `SKILLMD_SEARCH_URL` | Overrides the SkillMD public search endpoint |
| `GOSKILL_DISABLE_SKILLMD` | Set to `1` to omit SkillMD from `goskill find` |
| `GITHUB_API_URL`, `RAW_GITHUB_URL` | Override GitHub deep-search API and raw-content endpoints |
| `TRUEFOUNDRY_CATALOG_URL` | Overrides the TrueFoundry awesome-skills-registry JSON export |
| `GOSKILL_CATALOG_CACHE_DIR` | Overrides the user cache directory for static catalogs |
| `GOSKILL_CATALOG_TTL` | Static catalog refresh interval (default `24h`) |
| `GOSKILL_DISABLE_TRUEFOUNDRY_CATALOG` | Set to `1` to omit the TrueFoundry catalog from `goskill find` |
| `GOSKILL_PROVIDER_CONFIG` | Overrides the optional-provider JSON config file path |
| `GOSKILL_PROVIDER_CONFIG_JSON` | Supplies optional-provider JSON configuration directly |
