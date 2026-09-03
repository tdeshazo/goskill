# Agent Skills validation roadmap

## Status

The completed foundation at `bd73c86` makes goskill a production-grade Agent
Skills implementation and validation toolchain. It is not a competing
specification or a claim to be the canonical validator: the published [Agent
Skills specification](https://agentskills.io/specification) remains the
authority.

Completed:

- explicit `spec`, `recommended`, and `portable` validation profiles;
- stable `ASxxx` conformance, `GSxxx` guidance, and `GPxxx` portability rule
  namespaces;
- an immutable upstream revision, surfaced through `goskill spec` and
  `goskill validate --version-info`;
- deterministic structured diagnostics with text, JSON, and SARIF output;
- conformance fixtures and pinned differential CI against
  [`skills-ref`](https://github.com/agentskills/agentskills/blob/69ef37e9424c0a7ea9dd2293b559e43ec8176379/skills-ref/README.md).

The current additional rule surface is deliberately small: `recommended` adds
`GS210` (the 500-line guidance warning) to `spec`, and `portable` adds `GP310`
(the exact uppercase `SKILL.md` filename requirement) on top of `recommended`.
Local-reference checks are intentionally absent; if adopted, they belong in
`recommended`, not in the normative `spec` profile.

## Remaining milestones

### 1. Source locations and rule metadata

**Outcome:** every applicable diagnostic has accurate source location data and
machine consumers can discover the stable rule catalog and its provenance.

- [ ] Record precise line and column locations for frontmatter and file-level
      findings without losing deterministic diagnostic ordering.
- [ ] Extend the existing stable rule catalog, which already records code,
      summary, and severity, with explicit profile and authoritative-source or
      rationale metadata.
- [ ] Carry the new metadata consistently through discovery interfaces, text,
      JSON, and SARIF.

This milestone comes first: explanations, machine schemas, and differential
reports need trustworthy locations and rule identity. Do not recreate the
frontmatter requirements already represented by `AS002` and `AS003` as `GP`
rules; [Vercel issue 1282](https://github.com/vercel-labs/skills/issues/1282)
instead demonstrates the need for clear diagnostics when malformed input is
rejected.

### 2. Rules discovery and explanation

**Outcome:** an author or CI user can identify what each emitted code means,
which profile enables it, and the evidence behind it.

- [ ] Add `goskill rules` for stable catalog discovery and
      `goskill explain <code>` for single-rule explanations.
- [ ] Link each rule to its specification text, official guidance, or explicit
      portability evidence.
- [ ] Keep explanation output derived from the rule catalog so it cannot drift
      from diagnostics.

### 3. Recommended guidance

**Outcome:** narrowly scoped, evidence-backed authoring guidance is available
without redefining the Agent Skills specification.

- [ ] Add guidance only when it has an authoritative basis and a clear author
      action.
- [ ] Consider local-reference checks here, informed by [Agent Skills
      discussion 282](https://github.com/agentskills/agentskills/discussions/282),
      with path handling and false-positive behavior defined before
      implementation.
- [ ] Preserve the boundary: guidance is not normative conformance and
      security analysis is a separate future audit.

### 4. Portable, versioned corpus

**Outcome:** portability rules are justified by a reproducible corpus rather
than assumptions about clients.

- [ ] Define a versioned corpus format with expected outcomes and provenance.
- [ ] Capture reproducible evidence across relevant client implementations.
- [ ] Add `GPxxx` rules only for documented interoperability differences;
      keep `GP310` as the sole portability rule until then.

### 5. JSON and SARIF compatibility contract

**Outcome:** integrations can safely consume reports across goskill releases.

- [ ] Document JSON and SARIF metadata, schema evolution, and compatibility
      expectations.
- [ ] Add machine-output fixtures for locations, rule metadata, profiles, and
      error/warning summaries.
- [ ] Version any published schemas deliberately rather than changing fields
      implicitly.

### 6. Differential reporting

**Outcome:** changes relative to the pinned reference are reviewable and
intentional.

- [ ] Report fixture-level agreement and explicitly named, justified
      differences from `skills-ref`.
- [ ] Make a pin update require re-review of both the specification and the
      reference behavior.
- [ ] Keep the specification authoritative when its requirements differ from
      the reference implementation.

### 7. CI controls and reusable action

**Outcome:** repositories can adopt validation with clear policy controls and
repeatable CI behavior.

- [ ] Provide documented CI controls for profile, report format, and failure
      policy.
- [ ] Provide a reusable GitHub Action after the report and schema contracts
      are stable.
- [ ] Keep pinned-reference differential checks immutable and reproducible.

## Decisions to resolve before expanding scope

- Whether any future recommended findings should be errors or remain warnings;
  `GS210` is currently warning-only.
- How guidance should measure tokens, if token-based guidance is added.
- What reproducible client behavior is sufficient portability evidence.
- Which JSON and SARIF schema-compatibility guarantees goskill will make.

## Non-goals

- Publishing an alternative Agent Skills specification or presenting goskill as
  the canonical validator.
- Adding broad lint, repository policy, link checking, or security analysis to
  the normative profile.
- Treating reference-parser behavior as more authoritative than the published
  specification.
