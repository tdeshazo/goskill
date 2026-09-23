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
  hash produced by `skills.FolderHash`). Fixture files are checked out with LF
  line endings so this byte hash is stable on Windows.

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

### JSON report contract (v1)

The published [v1 JSON Schema](../schemas/validation-report.v1.schema.json)
describes `goskill validate --format json`. Its `schema_version` is the string
`"1"`. This is the report format version, separate from the goskill build
version and the pinned Agent Skills specification revision. Consumers should
check `schema_version` before reading the report and ignore unknown object
fields within a supported version.

| Field | Meaning |
| --- | --- |
| `profile` | Active `spec`, `recommended`, or `portable` policy. |
| `valid` | `true` exactly when the report has no error diagnostics. Warnings do not make it false. |
| `summary` | `skills` counts file entries; `diagnostics` is `errors + warnings` across those files. |
| `specification` | Upstream versioning status, immutable Git revision, canonical URL, and pinned source URL. |
| `rules` | The complete catalog enabled by the selected profile, including rules that did not fire. Each rule has a stable code, summary, severity, minimum enabling profile, and a `source` URL or `rationale` (or both). |
| `files` | File reports with `path`, `valid`, and a `diagnostics` array. A file is valid when it has no errors. |
| `diagnostics` | The complete report-wide diagnostic list. Each entry has `code`, `severity`, `message`, `path`, `line`, and `column`. |

`files` and `diagnostics` are arrays even when empty. File reports are ordered
by path; diagnostics are ordered by path, line, column, and code. The
report-wide diagnostics repeat the entries in `files[].diagnostics` so clients
can choose either view. Local paths are absolute; remote-source paths use a
stable source URL rather than a temporary clone path. Locations are 1-based;
file-level findings use `1:1` as described above.

### SARIF mapping and compatibility

SARIF output uses the [SARIF 2.1.0
schema](https://docs.oasis-open.org/sarif/sarif/v2.1.0/errata01/os/schemas/sarif-schema-2.1.0.json).
The SARIF `version` and `$schema` identify that standard. Goskill's own report
contract version appears as `goskill_validation_schema_version` in both
`runs[0].properties` and `runs[0].tool.driver.properties`; it is `"1"` for
the current format. Those property bags also carry `goskill_profile`,
`goskill_summary`, and `goskill_specification` with the same meanings as JSON.

The active rule catalog is in `runs[0].tool.driver.rules`, with the stable code
as `id`, severity as `defaultConfiguration.level`, authoritative `source` as
`helpUri` when available, and minimum profile and rationale in
`goskill_profile` and `goskill_rationale` properties. Each diagnostic becomes a
SARIF result with the code as `ruleId`, severity as `level`, a physical artifact
URI and 1-based source region, and the full JSON diagnostic fields in
`result.properties`. Local artifact URIs use `file://`; remote artifact URIs
retain their source URL. `runs[0].artifacts` is sorted by URI and
`runs[0].results` follows diagnostic order.

Within v1, existing required fields, their types, profile and severity values,
the validity/count meanings, location convention, and rule code identities
remain stable. Optional fields and new rule codes may be added; consumers
should ignore unknown fields and codes. Diagnostic messages, rule summaries,
evidence URLs, the pinned upstream revision, and which rules fire for an input
may change without a report schema version change. Scripts should select by
rule code and severity rather than parse message text.

Any breaking shape or meaning change requires a new versioned schema file, a
new report version marker in JSON and SARIF, and new fixtures. The v1 schema
file remains available for consumers pinned to v1. Additive v1 fields require
an intentional schema and fixture update in the same change. The checked-in
[machine-output fixtures](../testdata/validation-output/v1) cover all three
profiles, precise and fallback locations, source and rationale rule metadata,
valid warning-only output, and mixed error/warning summaries. They are checked
out with LF line endings on every platform. After reviewing
a contract change, regenerate them with
`UPDATE_VALIDATION_FIXTURES=1 go test ./internal/commands -run TestValidationMachineOutputFixtures`.

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

The [differential manifest](../testdata/conformance/differential.json) records
both reviewed sources, their Git object IDs, the fixture count, and every
justified difference with its exact specification section. CI prints the
goskill and `skills-ref` validity result for every
fixture, followed by agreement and difference counts. At this pin, 22 fixtures
agree and seven intentionally differ. The comparison fails if a declared
difference disappears, an unlisted difference appears, a fixture is added or
removed without review, a goskill diagnostic changes, or the binary and
checkout use different revisions.

The seven differences cover a boolean `description`, empty or boolean
`compatibility`, non-mapping `metadata`, numeric metadata keys or values, and
a sequence `allowed-tools`. The pinned reference uses StrictYAML, which turns
simple scalar booleans into strings, and omits some of the specification's
metadata and `allowed-tools` checks. goskill follows the pinned specification's
field requirements for all seven cases. Empty string metadata keys remain
valid. Each manifest entry names its fixture, goskill rule, expected outcomes,
and reason; it is an explicit review decision, not a blanket exception.

To reproduce the differential check locally, check out the manifest's exact
Agent Skills revision, run `uv sync --frozen --project` on its `skills-ref`
directory, and activate that environment. Then build goskill and run:

```bash
go build -o /tmp/goskill-differential .
python -m unittest scripts.test_conformance_diff
python scripts/conformance_diff.py \
  --goskill /tmp/goskill-differential \
  --reference-checkout /path/to/pinned/agentskills
```

When updating the upstream pin, review the new
`docs/specification.mdx` and `skills-ref/src/skills_ref` behavior separately.
Update `SpecRevision`, the immutable CI checkout ref, both revision and review
records in the differential manifest, and the specification blob and reference
source-tree Git object IDs. Replay every fixture against both implementations;
update named differences, fixture expectations, rule metadata, and this policy
as needed. CI verifies the review records and source object IDs against the
checkout and rejects an incomplete pin update. The recorded review notes
explain why goskill follows the specification where the reference differs.
