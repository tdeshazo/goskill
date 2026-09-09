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
| `recommended` | `spec` plus `GSxxx` official authoring guidance and bounded local-reference integrity checks. | `GS210` is a warning. `GS220` and `GS221` are errors and make the result invalid. |
| `portable` | `recommended` plus `GPxxx` cross-client interoperability checks. | `GS210` remains warning-only; `GS220`, `GS221`, and `GPxxx` portability failures are errors and exit nonzero. |

The initial guidance rule is `GS210`: a `SKILL.md` above 500 physical lines
warns with its actual line count. This is official guidance, not a normative
specification requirement: a 501-line skill is valid and exits zero with the
`recommended` profile.

Recommended profiles also inspect Markdown link and image destinations and
reference definitions. `GS220` reports a local target that does not exist.
`GS221` reports an absolute target, a relative target that lexically leaves the
skill directory, or a target that resolves through a symlink outside the skill
directory. These findings are errors because the author action is clear: keep
every local target present and inside the skill directory. The `spec` profile
does not inspect these references, so the default compatibility contract is
unchanged. External and opaque URL schemes, fragments, code spans, and fenced
code are ignored. Angle-bracket destinations, titles, spaces, URL encoding,
and query or fragment suffixes are supported.

The initial portability rule is `GP310`: `portable` requires the exact
uppercase filename `SKILL.md`. The `spec` profile continues to accept lowercase
`skill.md` for compatibility with the pinned reference parser, but that name is
not portable to case-sensitive client implementations. No broad linting,
security analysis, or speculative compatibility heuristics are included in
these profiles. Discussion [282](https://github.com/agentskills/agentskills/discussions/282)
provided design context for the local-reference checks; it is not treated as a
normative specification source.

## Portability corpus

Milestone 4 evidence is checked in under
[`testdata/portability/v1`](../testdata/portability/v1). The corpus is JSON
with `schema_version: "1"` and four required top-level fields:

- `schema_version` identifies the supported major schema (`"1"`).
- `name` is a stable lowercase corpus identifier.
- `clients` identifies each implementation with a stable `id`, display
  `name`, and implementation path. External clients also require a full
  40-character lowercase Git revision. The `goskill` client is marked
  `local_replay: true`: its expected outcome is certified by the current
  offline Go replay instead of an impossible future commit claim.
- `cases` contains stable case `id`, description, a relative `fixture` path,
  the `portable` profile, an `expected` outcome for every client, and
  external-client evidence. Each case's `evidence` identifies its client, an
  HTTPS immutable source URL bound to that implementation path and exact
  revision, the revision again, a reproducible `method`, and the
  SHA-256 `artifact_sha256` of the fixture directory (the same deterministic
  hash produced by `skills.FolderHash`).

Supported source forms are `https://host/owner/repository/tree/<sha>` and
`https://host/owner/repository/-/tree/<sha>`, each with an optional
subdirectory. Their host, repository, subdirectory, and revision must compose
to the declared implementation path and revision.

Expected outcomes contain `valid` and an ordered list of stable diagnostic
`code`/`severity` pairs. A valid outcome may include warnings; an error
diagnostic makes it invalid. Outcomes are recorded from each client's native
validation command; `profile` identifies the goskill replay policy. The loader accepts unknown JSON fields so a
backward-compatible schema addition does not break older readers, but it
rejects unsupported schema versions, duplicate IDs, unknown clients/rules,
inconsistent validity, incomplete expectations or external evidence,
mutable/unpinned or unrelated sources, and mismatched fixture hashes. Fixture
paths are relative and may not
escape the corpus root.

The Go tests load and validate `corpus.json`, then replay every `goskill`
expectation with `ValidateSkillDirectoryWithProfile` without network access or
an external client installation. The `skills-ref` outcomes are recorded with
the pinned Agent Skills revision and an explicit command method; they are
evidence for comparison, not a runtime dependency of `go test ./...`.

To update the corpus, add or change a fixture, run the pinned external-client
commands from each evidence record, record the complete immutable revisions
and fixture hashes, and run:

```bash
go test ./internal/skills -run TestPortabilityCorpusReplay
go test ./...
```

Review the source behavior and the resulting case-level diff together. A new
`GPxxx` rule requires a documented interoperability difference, at least one
reproducible case showing the difference, pinned client revisions, and an
explicit author action. A single surprising result, an unpinned release/tag,
or a network-only observation is not sufficient. Until that gate is met,
`GP310` remains the sole portability rule.

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

Profiles do not lint prose, perform security analysis, or enforce repository
policy. The `spec` profile does not inspect references. The `recommended` and
`portable` profiles inspect only the bounded Markdown destinations described
above; they do not perform broad link checking or crawl referenced content.
Duplicate skill names remain valid, and external URLs, fragments, and code are
not treated as local targets. Broad lint/security heuristics remain out of
scope.

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
