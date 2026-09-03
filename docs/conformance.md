# Agent Skills conformance

`goskill validate` implements the strict Agent Skills conformance profile
pinned to [`agentskills/agentskills` revision
`69ef37e9424c0a7ea9dd2293b559e43ec8176379`](https://github.com/agentskills/agentskills/tree/69ef37e9424c0a7ea9dd2293b559e43ec8176379).
The revision is embedded in the binary and printed by `goskill --version`.
It is never resolved from `main` at runtime. Agent Skills currently publishes
no formal numbered or semantic specification version, tags, or releases, so
goskill identifies its conformance contract by this immutable Git revision.
Run `goskill spec` to inspect the versioning status, revision, and exact source
snapshot (alongside the canonical published URL); `goskill spec --revision`
prints only the SHA for scripts.

The command validates a skill directory's `SKILL.md` (or the reference
implementation-compatible lowercase `skill.md`) and emits deterministic,
structured diagnostics. Diagnostics are ordered by path, line, column, and
rule code. Lines and columns are 1-based. Field findings point at the matching
YAML value; unexpected top-level fields point at their key. Missing fields,
missing/unreadable files, filename checks, line-count guidance, and malformed
YAML without a usable node use the deterministic `1:1` file fallback (a YAML
parser-reported malformed line uses column `1`).

For the default `spec` profile, goskill makes this bounded contract: Given the Agent Skills specification revision embedded in this binary, this directory conforms to every normative rule we implement from that specification. This does not claim that goskill implements every possible upstream rule, and it does not imply a numbered upstream specification version.

`goskill validate --version-info` reports the build's validation identity
without resolving a source or accessing the network:

```text
validator: goskill 0.2.4
spec: agentskills/agentskills@69ef37e
profile: spec
```

The actual build version may differ. `--version-info` may be combined only
with `--profile`; it accepts no sources or JSON/SARIF output options.

## Validation profiles

`goskill validate <source>` is exactly equivalent to
`goskill validate --profile spec <source>`. Profiles are layered and stable:

| Profile | Rules | Severity and exit status |
| --- | --- | --- |
| `spec` | Normative `ASxxx` Agent Skills requirements. | All findings are errors; any finding makes the result invalid and exits nonzero. |
| `recommended` | `spec` plus `GSxxx` official authoring guidance. | Guidance findings are warnings. Warnings do not make a result invalid and exit zero. |
| `portable` | `recommended` plus `GPxxx` cross-client interoperability checks. | `GSxxx` remains warning-only; `GPxxx` portability failures are errors and exit nonzero. |

The initial guidance rule is `GS210`: a `SKILL.md` above 500 physical lines
warns with its actual line count. This is official guidance, not a normative
specification requirement: a 501-line skill is valid and exits zero with the
`recommended` profile.

The initial portability rule is `GP310`: `portable` requires the exact
uppercase filename `SKILL.md`. The `spec` profile continues to accept lowercase
`skill.md` for compatibility with the pinned reference parser, but that name is
not portable to case-sensitive client implementations. No broad linting,
security analysis, or speculative compatibility heuristics are included in
these profiles.

## Machine-readable output

`goskill validate` retains text output by default. Use one of these formats for
automation:

```bash
goskill validate --format json ./my-skill
goskill validate --json ./my-skill
goskill validate --format sarif ./my-skill > conformance.sarif
goskill validate --sarif ./my-skill > conformance.sarif
goskill validate --profile recommended --json ./my-skill
goskill validate --profile portable --sarif ./my-skill > portability.sarif
```

`--profile` accepts `spec`, `recommended`, or `portable`; it can be combined in
either order with `--format`. `--format` accepts `text`, `json`, or `sarif`; it
cannot be combined with the `--json` or `--sarif` aliases. JSON reports use
schema version `1` and include the active profile, validity, error and warning
counts, pinned specification metadata, the active profile's complete `rules`
catalog, files, and the complete sorted diagnostic list. Each catalog rule has
its stable code, summary, severity, enabling profile, and an authoritative
source and/or rationale. SARIF output is SARIF 2.1.0, publishes the same
catalog as driver rules, uses the source as `helpUri` when one exists, and
exposes profile/rationale in `goskill_*` rule properties. Text output adds the
diagnostic location and rule profile plus the available source or rationale
after its stable bracketed code.
Every diagnostic with a known path has a physical SARIF artifact location and
a source region.

Both machine formats are deterministic and ANSI-free. A conformance failure
still exits nonzero, but its complete JSON/SARIF document is the only stdout
content. Invalid format flags, invalid sources, and other operational failures
produce normal command errors instead of a partial report.

## Rule catalog discovery

`goskill rules` lists every stable validation rule in the catalog order used by
the validator. `goskill explain <code>` shows one rule's summary, severity,
minimum enabling profile, and its authoritative source or portability
rationale. Command input is normalized by trimming whitespace and accepting
lowercase rule codes; the output always uses the stable uppercase code.

Both commands accept `--json`. `goskill rules --json` emits the complete ordered
array of catalog rules, and `goskill explain --json <code>` emits that one rule.
These outputs are deterministic and ANSI-free. Invalid command shapes and
unknown codes fail without producing partial JSON.

## What the `spec` profile guarantees

Strict validation checks only normative format requirements:

- required readable skill file and YAML frontmatter;
- allowed top-level frontmatter fields;
- required `name` and `description` fields;
- name length, Unicode lowercase name characters, hyphens, and parent-directory
  matching;
- description and compatibility field types and lengths;
- `metadata` as a string-to-string mapping; and
- `allowed-tools` as a string.

The stable rule catalog is exposed through `skills.Rules`,
`skills.RulesForProfile`, `goskill rules`, and `goskill explain`. Scripts
should use the stable rule code rather than diagnostic text.

## What profiles do not guarantee

Profiles do not lint prose, perform security analysis, inspect references, or
enforce repository policy. In particular, duplicate skill names and missing,
escaping, or broken local Markdown links do not cause `goskill validate` to
fail. `recommended` contains only documented official guidance, and `portable`
contains only narrowly defensible cross-client rules; broad lint/security
heuristics remain out of scope.

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
