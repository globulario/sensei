# `SENSEI.md`: Repository-Local Proof Summary

## Decision

Sensei will use a generated `SENSEI.md` file as its repository-local human-readable report and signature.

The file is deliberately simple. It exists to tell maintainers what Sensei found, what it verified, what remains unresolved, and how to reproduce the result.

It is not a dashboard, configuration file, source of authority, or marketing page.

> Sensei was here. This is what it found. This is what it proved. This is how to verify it.

## Why this surface

A dedicated dashboard would introduce another service, deployment model, client, authentication surface, and interpretation layer before Sensei itself is finished.

Sensei already operates where the evidence lives: repositories, revisions, tasks, contracts, tests, receipts, and CI. A repository-local report is therefore the smallest useful presentation surface.

This direction keeps development focused on Sensei's core completion ability while still leaving behind an immediately useful artifact.

The report should be:

- accurate rather than decorative;
- reproducible rather than persuasive;
- useful rather than impressive;
- repository-local rather than service-dependent;
- simple enough to remain trustworthy.

## Role in a repository

`SENSEI.md` is the concise human-readable projection of the current Sensei report model.

It should answer these questions quickly:

1. Which repository revision does this report describe?
2. What is the current Sensei disposition?
3. What material findings or blockers remain?
4. What verification supports the disposition?
5. What recent task or change was evaluated?
6. Are behavioral-memory candidates awaiting review?
7. How can the result be reproduced?

Its presence also acts as Sensei's signature. A person browsing the repository can see that Sensei has inspected or governed it, but the file earns its place by being useful, not by advertising the product.

## Authority boundary

`SENSEI.md` is a generated projection. It must never become authoritative input.

```text
Sensei authoritative state and evidence
                  |
                  v
             report model
              /       \
             v         v
       SENSEI.md   .sensei/report.json
```

Sensei may generate, compare, and validate the report, but must not parse claims from `SENSEI.md` back into the graph, task state, admission decisions, or verification state.

This preserves one canonical derivation path and prevents Markdown presentation from becoming a second source of truth.

## Proposed repository layout

```text
SENSEI.md                         concise human-readable summary
.sensei/report.json               versioned machine-readable equivalent
.sensei/reports/<task-id>.md      optional detailed task reports
.sensei/receipts/                 authoritative execution evidence
```

The top-level report should remain small enough to understand in a few minutes. Detailed evidence belongs in receipts and task-specific reports.

## Minimum report content

### Identity

The report must identify the exact state it describes:

- repository identity;
- revision or head commit SHA;
- Sensei version;
- report schema version;
- generation evidence or timestamp policy;
- current disposition.

### Summary

A compact summary of meaningful state, such as:

- blocking findings;
- advisory findings;
- unresolved contradictions;
- known invariants, contracts, and failure modes when those counts have stable definitions;
- behavioral-memory candidates awaiting review.

The report must not invent aggregate quality or confidence scores merely to make the output look complete.

### Current work

When a task is active or was recently completed, report:

- task identity and concise title;
- terminal or current disposition;
- bounded scope;
- authority resolution;
- evaluation result;
- remaining blockers.

When there is no active task, say so directly.

### Important findings

Include only findings that materially affect maintainers. Do not dump the entire graph or every advisory observation into the top-level file.

### Verification

Present the evidence supporting the current disposition, for example:

- build result;
- repository test result;
- required-test satisfaction;
- contract evaluation;
- changed-file boundary;
- generated-artifact freshness;
- linked evidence receipts.

Prefer facts such as `Build: passed` and `Blocking findings: 0` over labels such as `Architecture health: excellent`.

### Behavioral memory

Show only high-value candidates or recently promoted principles that are relevant to the repository's current state. Include their status and evidence basis without reproducing the full knowledge graph.

### Reproduction

Include the exact commands needed to regenerate or validate the report.

```sh
sensei verify
sensei report
sensei report --check
```

## Proposed command surface

### `sensei report`

Generate or refresh `SENSEI.md` and `.sensei/report.json` from the same internal report model.

### `sensei report --check`

Fail when the committed report is stale, modified, incomplete, schema-obsolete, or inconsistent with the authoritative report inputs.

This mode is intended for CI.

### `sensei report --task <task-id>`

Generate a detailed task report under `.sensei/reports/<task-id>.md`.

### `sensei report --stdout`

Render the report without modifying the repository.

## Disposition and staleness

The report vocabulary must be explicitly defined. The first version should distinguish at least:

- `VERIFIED`;
- `BLOCKED`;
- `UNVERIFIED`;
- `INCOMPLETE`;
- `STALE`.

A report generated for an earlier revision must not continue to display an unqualified `VERIFIED` status against a newer revision.

Example:

```text
Disposition: STALE
Report revision: abc1234
Current revision: def5678
Run `sensei report` to refresh it.
```

## Determinism

Given identical authoritative inputs, report schema, Sensei version, repository revision, task state, and evidence, generated Markdown and JSON should be byte-identical.

Generation must use stable ordering. Volatile timestamps should either come from authoritative receipts or be excluded from deterministic comparison.

## Tone

The report should read like an engineering instrument.

Preferred:

```text
Revision: abc1234
Blocking findings: 0
Build: passed
Evaluation: passed
```

Avoid:

```text
Sensei supercharged this repository.
Architecture health is amazing.
This project is AI-ready.
```

The file should remain useful even when read without any interest in Sensei as a product.

## Example

```markdown
# Sensei Report

Repository: globulario/sensei
Revision: b80c3292
Sensei version: v1.x.x
Report schema: v1
Disposition: VERIFIED

## Summary

- Blocking findings: 0
- Advisory findings: 2
- Memory candidates awaiting review: 3

## Current Work

Task: Close governed synthesis execution
Disposition: Verified
Scope: 7 files
Authority: Resolved
Evaluation: Passed
Remaining blockers: None

## Important Findings

- One advisory duplicated derivation path remains.
- Two behavioral-memory candidates await review.

## Verification

- Build: passed
- Tests: passed
- Required tests: satisfied
- Changed-file boundary: satisfied
- Generated artifacts: current
- Evidence receipts: available

## Reproduce

```sh
sensei verify
sensei report --check
```
```

## Initial implementation boundary

The first implementation should remain deliberately narrow.

Required:

- one versioned report model;
- Markdown and JSON generated from that model;
- exact repository-revision binding;
- current disposition;
- concise blockers and findings;
- verification summary;
- reproduction commands;
- deterministic generation;
- stale-report detection;
- tests for generation, empty state, determinism, and staleness.

Not required:

- a web dashboard;
- hosted reporting;
- authentication;
- live task monitoring;
- graph visualization;
- cross-repository analytics;
- workflow building;
- chat interfaces;
- repository health scores;
- project-management features.

Future clients may consume `.sensei/report.json`, but they must remain projections of the same canonical report model rather than introduce a second interpretation path.

## Acceptance criteria

This design is implemented when:

1. `SENSEI.md` is documented and generated as a human-readable projection.
2. Sensei never consumes `SENSEI.md` as architectural or execution authority.
3. A versioned machine-readable report schema exists.
4. Markdown and JSON are generated from the same report model.
5. The report identifies the exact repository revision.
6. `sensei report --check` detects stale or modified output.
7. Identical authoritative inputs produce identical output.
8. The report presents material blockers, findings, verification, and reproduction steps.
9. Tests cover generation, determinism, staleness, and empty-state behavior.
10. The implementation introduces no web service, UI framework, or duplicated authority path.

## Product direction

Sensei remains self-sufficient through its CLI, repository artifacts, receipts, and CI integration.

A dashboard is not required for Sensei to finish or prove its work. `SENSEI.md` supplies the essential visible surface while keeping the project focused on governed completion.