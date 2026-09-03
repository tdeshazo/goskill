# Agent Skills conformance

`goskill validate` implements the strict Agent Skills conformance profile
pinned to [`agentskills/agentskills` revision
`69ef37e9424c0a7ea9dd2293b559e43ec8176379`](https://github.com/agentskills/agentskills/tree/69ef37e9424c0a7ea9dd2293b559e43ec8176379).
The revision is embedded in the binary and printed by `goskill --version`.
It is never resolved from `main` at runtime.

The command validates a skill directory's `SKILL.md` (or the reference
implementation-compatible lowercase `skill.md`) and emits deterministic,
structured `ASxxx` diagnostics. Diagnostics are ordered by path, line, column,
and rule code. P0 diagnostics are all errors; line and column are currently
zero.

## What strict validation guarantees

Strict validation checks only normative format requirements:

- required readable skill file and YAML frontmatter;
- allowed top-level frontmatter fields;
- required `name` and `description` fields;
- name length, Unicode lowercase name characters, hyphens, and parent-directory
  matching;
- description and compatibility field types and lengths;
- `metadata` as a string-to-string mapping; and
- `allowed-tools` as a string.

The stable rule catalog is exposed in `internal/skills/rules.go`. Scripts that
consume command output should use the bracketed rule code, not diagnostic text.

## What it does not guarantee

P0 does not lint prose, recommendations, portability, security, references, or
repository policy. In particular, duplicate skill names and missing, escaping,
or broken local Markdown links do not cause `goskill validate` to fail. Those
are candidates for a future lint profile, not Agent Skills format conformance.

## Relationship to `skills-ref`

The fixture corpus under `testdata/conformance` is tested locally by Go and in
CI against the pinned `skills-ref` reference project. The specification is the
authority when it differs from the demonstration reference implementation.

At the pinned revision, `skills-ref` does not reject an empty `compatibility`,
non-string `allowed-tools` values, or non-mapping/non-string `metadata` keys or
values. `goskill` rejects them because the specification defines compatibility
as a 1-500 character string, `allowed-tools` as a space-separated string, and
`metadata` as a map from string keys to string values. Empty string metadata
keys remain valid. The differential CI records only these intentional
disagreements; every other fixture outcome must agree with `skills-ref`.

When updating the pin, update `SpecRevision`, re-review the upstream
specification and `skills-ref` parser/validator, adjust fixtures and this
document, and keep the CI checkout immutable.
