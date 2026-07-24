# Semantic Protection Coverage Bootstrap

Status: implementation contract for architect review  
Base revision: `1d098cd4923fac25ba445bbf1dedf3072f0d5058`  
Target repository: `globulario/sensei`

## 1. Problem

Sensei currently has two different protection stories:

1. Sensei's own repository has a populated `docs/awareness/high_risk_files.yaml` that protects its authority surface.
2. A newly initialized or bootstrapped repository receives the embedded starter template whose `files:` list is empty.

The Claude pre-edit hook reads only that manual YAML list and exits successfully when the file is absent or empty. Meanwhile `sensei bootstrap` may already discover contracts, schemas, authority surfaces, invariant candidates, tests, and source anchors, but those observations do not populate or otherwise contribute to the file-protection decision.

This creates a bootstrap gap:

```text
Sensei installed
+ hooks installed
+ architecture-sensitive files exist
+ manual high-risk registry empty
= no file-level briefing enforcement
```

An empty manual list is configuration absence, not evidence that the repository has no load-bearing files.

## 2. Mission

Make protected-file classification a deterministic repository property instead of a handwritten directory checklist.

The effective protected set must be the union of:

1. explicit manual protection;
2. unconditional Sensei/governed-source protection;
3. deterministic structural contract and invariant protection;
4. direct graph-derived protection relationships when a valid current graph is available.

The result must be inspectable, fail-honest, deterministic, usable by hooks without a running MCP service, and incapable of promoting candidate architecture into authoritative truth.

## 3. Core laws

### 3.1 Manual silence is not safety

`docs/awareness/high_risk_files.yaml` remains a supported additive manual registry, but an absent or empty `files:` list must never mean "nothing is protected" by itself.

The manual file is an override/seed surface, not the sole protection authority.

### 3.2 Direct contract and invariant definitions are protected

A repository file must be protected when deterministic evidence shows that it directly defines a contract, invariant, authority rule, mutation path, forbidden fix, required proof obligation, or a machine-readable contract schema.

At minimum this includes:

- supported authored governed sources under `docs/awareness/`, excluding `generated/` and `candidates/` as authority;
- `docs/awareness/high_risk_files.yaml` itself;
- supported API/schema sources discovered by existing deterministic scanners, including protobuf, OpenAPI, and JSON Schema contract surfaces;
- source files carrying supported Sensei contract/invariant annotations;
- source files that the existing deterministic extraction pass identifies as contract, authority-surface, or invariant candidates.

Candidate-derived protection is procedural caution only. It does not make the candidate true, governed, promoted, or authoritative.

### 3.3 Governed relationships extend protection

When a valid, current repository graph or equivalent typed governed model is available, direct relationships must extend protection to files that:

- realize or enforce a governed contract;
- are constrained by an invariant;
- validate or serialize a governed contract;
- are explicitly named by `protects.files`;
- implement an allowed mutation or observation path;
- contain a required test or proof named by a governed rule.

Only explicit direct relationships are authorized in this contract. Do not perform unbounded transitive graph expansion.

### 3.4 Protection is not architectural authority

A file may be classified as protected because observed structure suggests that mistakes would be expensive. This classification must not:

- promote a candidate;
- create a governed contract or invariant;
- certify correctness;
- infer an owner or legal mutation path;
- turn generated graph content into repository authority;
- rewrite authored awareness sources.

Protection answers only: "must an agent consult Sensei before editing this file?"

### 3.5 Empty and incomplete coverage are explicit states

Protection coverage must expose a closed status vocabulary:

- `COMPLETE`: all supported protection inputs were evaluated successfully and at least one effective protected path exists, or the repository was conclusively scanned and no supported protection signal exists;
- `PARTIAL`: usable protection exists, but one or more supported inputs were unavailable, stale, invalid, or not yet evaluated;
- `DEGRADED`: contracts/invariants/governed sources or contract-like structural signals exist but the effective protected set cannot be established safely;
- `EMPTY`: no protection signal was observed after a successful bounded scan; this is not a low-risk verdict and must be presented as an awareness gap.

Do not collapse `EMPTY`, `PARTIAL`, or `DEGRADED` into "no high-risk files."

### 3.6 One owner decides protection

Introduce or designate one pure Go owner for:

- path normalization;
- manual-registry parsing;
- deterministic structural derivation;
- direct governed-relation derivation;
- deduplication and reason preservation;
- coverage status;
- per-file classification.

Bootstrap, CLI, preflight, hooks, audit, and future editor surfaces must consume that owner. They must not maintain separate path-matching tables or reimplement the decision in shell.

The package location may follow the existing ownership model after inspection, but there must be one canonical implementation. A thin wrapper around an existing suitable owner is preferred over duplication.

### 3.7 Matching is path-safe

Protection matching must:

- normalize repository-relative slash-separated paths;
- reject or mark paths outside the repository;
- handle directory prefixes by path-segment boundary, not raw string accident;
- avoid `src/auth/` matching `src/authorization/` unless explicitly intended;
- fail safely on malformed paths and symlink escapes;
- preserve exact file identity for explicit files.

### 3.8 Manual protection is additive in this slice

Do not add an ungoverned exclusion mechanism in this repair. A manual entry may add protection but may not remove deterministic or governed protection.

A future exclusion/exception design requires its own authority, justification, expiry, and audit contract.

## 4. Effective protection model

For each protected path, retain ordered reasons rather than a boolean only.

Suggested typed shape, adaptable to existing conventions:

```go
type ProtectionOrigin string

const (
    OriginManual              ProtectionOrigin = "manual"
    OriginGovernedSource      ProtectionOrigin = "governed_source"
    OriginStructuralContract  ProtectionOrigin = "structural_contract"
    OriginCandidateSignal     ProtectionOrigin = "candidate_signal"
    OriginGovernedRelation    ProtectionOrigin = "governed_relation"
)

type ProtectionReason struct {
    Origin       ProtectionOrigin
    Kind         string
    Source       string
    KnowledgeRef string
    Provisional  bool
}

type ProtectedPath struct {
    Path    string
    Reasons []ProtectionReason
}

type ProtectionCoverage struct {
    Status         ProtectionCoverageStatus
    ProtectedPaths []ProtectedPath
    ManualCount    int
    DerivedCount   int
    Gaps           []string
}
```

Names may change to match repository conventions. Semantics may not.

Ordering must be deterministic:

1. normalized path ascending;
2. reason origin in a fixed vocabulary order;
3. reason kind/source/reference ascending within an origin.

## 5. Offline protection snapshot

The pre-edit hook must not require a running MCP server. Produce a deterministic repository-local protection snapshot from the canonical owner.

Preferred location:

```text
.sensei/project/protection-coverage.yaml
```

The snapshot is derived state, not governed source truth. It must include:

- schema/version token;
- repository-relative protected paths and reasons;
- coverage status and gaps;
- identities/digests of the evaluated manual registry, supported governed sources, and structural inputs;
- generation identity sufficient to detect staleness;
- no timestamps in semantic identity or deterministic comparisons.

Publication must be atomic: write and validate a temporary complete snapshot, then replace the prior snapshot. A failed derivation must preserve the prior valid snapshot and report it as stale rather than publish partial truth.

If an existing project-artifact publication owner already covers this artifact family, extend that owner rather than creating a second publication mechanism.

## 6. CLI surface

Add one typed CLI projection over the canonical owner. Exact command naming may follow repository conventions; the behavior must support:

```text
sensei protection-status [--path <repo>] [--json]
sensei protection-check --path <repo> --file <repo-relative-file> [--json]
```

`protection-status` reports:

- closed coverage status;
- effective protected-path count;
- manual and derived counts;
- current/stale snapshot state;
- gaps and their causes.

`protection-check` reports:

- whether the exact file is protected;
- all protection reasons;
- whether any reason is provisional;
- global coverage status;
- whether briefing is required;
- typed failure when the file is outside the repository or protection cannot be evaluated.

Text output must never say or imply that an unprotected file is architecturally safe. Use "not classified as protected by current coverage" rather than "low risk."

## 7. `sensei init` behavior

Update initialization so a fresh repository is not presented as fully configured merely because hook files exist.

Requirements:

1. Keep `docs/awareness/high_risk_files.yaml` backward compatible with `files:`.
2. Rewrite its embedded comments to explain that entries are additive manual protection and that Sensei also derives protection.
3. Remove the instruction that the user must populate this file before Sensei can protect anything.
4. Initialize the unconditional governed-source baseline and protection snapshot when possible.
5. If the repository has not yet been bootstrapped, report protection coverage as `PARTIAL` or `EMPTY`, never silently complete.
6. Re-running init remains idempotent and never overwrites a user-modified manual registry.

## 8. `sensei bootstrap` behavior

After deterministic extraction has identified contract/schema/authority/invariant signals, bootstrap must derive and atomically publish the effective protection snapshot.

Requirements:

- protection derivation participates in normal write, dry-run, and `--check` behavior;
- `--check` reports a stale/missing protection snapshot;
- curated generated corpora remain non-destructive;
- candidate source files may become provisionally protected without candidate promotion;
- the bootstrap report includes:
  - protection coverage status;
  - effective protected path count;
  - manual/derived/provisional counts;
  - meaningful gaps;
- next actions distinguish "review candidate architecture" from "repair protection coverage";
- a repository with contract/invariant signals and zero effective protection is a bootstrap failure or explicit `DEGRADED` result, not success.

Do not hand-edit the target repository's manual list to simulate derivation.

## 9. Hook behavior

Replace the shell hook's direct YAML grep and prefix decision with a call to the typed local protection owner through the CLI.

The hook must:

1. resolve the repository root safely;
2. ask the canonical classifier about the exact file;
3. require a briefing marker for `protected` or provisionally protected files;
4. block with a useful diagnostic when classification is `DEGRADED`, stale beyond the accepted contract, malformed, or outside the repository;
5. avoid claiming that absence from protection means safety;
6. preserve existing session-bound briefing marker behavior;
7. fail visibly if the Sensei binary/classifier is unavailable rather than treating the failure as an empty registry.

The shell script may remain a transport adapter, but it may not own protection semantics.

## 10. Preflight and risk-classification integration

The preflight risk classifier currently has static high-risk path handling. Integrate effective semantic protection as an additional structural signal without weakening existing fail-closed coverage behavior.

Requirements:

- a protected path with no direct anchors remains `UNKNOWN_IMPACT`/degraded, not low risk;
- a protected path with applicable anchors is at least architecture-sensitive unless a stricter category applies;
- protection reasons are surfaced in the response/blind spots;
- static legacy prefixes may remain only as an explicitly documented compatibility baseline and must not compete with the canonical owner;
- no preflight response may infer that an unclassified file is safe from an empty manual list.

## 11. Audit and diagnostics

Add an audit finding for protection coverage:

- `PASS`: coverage is complete and the effective set is non-empty when supported signals exist;
- `WARN`: `PARTIAL` or `EMPTY` with no confirmed contract/invariant signals;
- `FAIL`: `DEGRADED`, stale/invalid snapshot in enforce mode, or supported contract/invariant signals with zero effective protection.

The exact existing finding vocabulary may be reused, but the distinctions above must survive.

## 12. Required proof

Add tests proving at least:

### Initialization

- a fresh init with an empty manual `files:` list does not describe protection as absent or complete;
- the manual template explains additive override semantics;
- init remains idempotent and preserves user edits.

### Direct definition protection

- `docs/awareness/invariants.yaml` and other supported governed-source files are protected automatically;
- `docs/awareness/generated/*` and `docs/awareness/candidates/*` are not mistaken for authored authority merely by location;
- protobuf, OpenAPI, and JSON Schema files discovered by supported deterministic scanners are protected;
- an annotated source contract/invariant definition is protected.

### Candidate caution without promotion

- a source file that produces a deterministic invariant or authority candidate is provisionally protected;
- the candidate remains candidate and no governed source is mutated;
- deleting/rejecting the candidate signal removes provisional protection on the next derivation unless another reason remains.

### Governed relationship protection

- a file explicitly listed in `protects.files` is protected;
- a directly linked contract realizer/enforcer/validator is protected;
- a required-test file directly linked by governed knowledge is protected;
- unrelated sibling or transitively adjacent files are not automatically swept in.

### Coverage honesty

- empty manual registry plus detected contract/invariant signals never yields zero effective protection or a low-risk conclusion;
- successful scan with no supported signals yields `EMPTY`, not `COMPLETE` safety language;
- unavailable/stale/invalid inputs produce `PARTIAL` or `DEGRADED` as specified;
- coverage reasons and path ordering are deterministic across shuffled input order.

### Matching and security

- directory matching respects path-segment boundaries;
- absolute paths inside the repository normalize correctly;
- outside-repository and symlink-escape paths fail safely;
- malformed registry entries produce diagnostics and cannot weaken derived protection.

### Snapshot publication

- snapshot semantic output is deterministic;
- failed generation preserves the previous complete snapshot;
- stale input digests are detected;
- `bootstrap --check` detects missing/stale protection coverage;
- dry-run writes nothing.

### Hook integration

- protected and provisionally protected files require briefing;
- unclassified ordinary files do not require a file briefing solely from proximity;
- missing classifier/binary, degraded coverage, and stale invalid snapshots do not silently allow protected edits;
- existing session marker behavior remains intact.

### Preflight integration

- semantic protection escalates a pattern-only file to at least architecture-sensitive;
- semantic protection with no anchors yields unknown/degraded behavior;
- stricter security/data-loss/convergence classifications still dominate;
- manual-empty state cannot produce a false low-risk verdict for a structurally protected file.

## 13. Required commands and evidence

Before implementation handoff, run and report from a clean checkout:

```bash
go test ./cmd/awg/... -count=1
go test ./golang/coverage/... -count=1
go test ./golang/server/... -count=1
go test ./golang/architecture/... -count=1
go test ./... -count=1
sensei check
sensei audit --check --domain github.com/globulario/sensei
```

Also provide one temporary foreign-repository proof fixture demonstrating:

1. empty manual registry;
2. contract/schema/invariant-bearing files;
3. non-empty automatically derived protection;
4. hook refusal before briefing;
5. successful edit authorization after briefing;
6. candidate protection without promotion;
7. an unrelated README remaining unprotected.

GitHub Actions must pass on the exact handed-off head SHA.

## 14. Non-goals

This repair must not:

- auto-promote contracts, invariants, authority surfaces, or candidates;
- infer architectural truth from protection classification;
- add mutation, admission, certification, completion, or merge authority;
- replace task preflight, source inspection, tests, review, or the existing Sensei Architect workflow;
- perform unbounded transitive graph traversal;
- add a silent exclusion/bypass mechanism;
- blanket-protect every repository file;
- rewrite user-authored manual protection entries;
- make generated or candidate awareness files authoritative by location;
- require a running MCP server for pre-edit protection;
- weaken current fail-closed risk-classification behavior.

## 15. Stop conditions

Stop and post `ARCHITECT QUESTION` before coding around any of these:

- the current graph model cannot distinguish authored/governed knowledge from generated/candidate knowledge;
- direct realization, validation, serialization, or required-test relationships cannot be obtained through existing typed data without raw ad hoc graph queries;
- adding the protection snapshot would conflict with the existing transactional `.sensei/project` publication owner;
- the hook cannot call a stable local typed classifier without introducing a second protection implementation;
- candidate-derived protection would require candidate promotion;
- satisfying the contract appears to require an exclusion mechanism;
- an existing owner already provides equivalent semantic protection and this design would duplicate it.

Do not fill semantic gaps with filename heuristics beyond the explicitly supported structural contract sources.

## 16. Handoff protocol

Implementation must happen on `feat/semantic-protection-coverage-bootstrap`.

Before editing architecture-sensitive files, load the Sensei Architect skill and run Sensei preflight/task preparation according to the repository's current governed workflow. The existing self-governance registry already protects `cmd/awg/`, `golang/server/`, `golang/architecture/`, and governed awareness sources.

When implementation and CI are ready, post:

```text
IMPLEMENTATION READY FOR ARCHITECT REVIEW

Architect contract: docs/design/semantic-protection-coverage-bootstrap.md
Base SHA: 1d098cd4923fac25ba445bbf1dedf3072f0d5058
Head SHA: <exact>

Implemented:
- ...

Protection model evidence:
- manual:
- structural:
- governed relations:
- candidate/provisional:
- coverage states:

Sensei evidence:
- metadata/preflight/task preparation:
- active invariants/forbidden fixes:
- required proof:
- durable feedback proposed:

Verification:
- go test ./cmd/awg/... -count=1: PASS
- go test ./golang/coverage/... -count=1: PASS
- go test ./golang/server/... -count=1: PASS
- go test ./golang/architecture/... -count=1: PASS
- go test ./... -count=1: PASS
- sensei check: PASS
- sensei audit --check --domain github.com/globulario/sensei: PASS
- GitHub Actions: PASS

Deviations:
- None | ...

HANDOFF: GPT ARCHITECT REVIEW
```

Then stop. Do not merge or begin unrelated Sensei Dashboard or Phase 10 work.

## 17. Acceptance criteria

This repair is architecturally complete only when:

1. a newly initialized/bootstrapped repository no longer depends on a populated manual list for all file protection;
2. direct contract/invariant/governed-source files are automatically protected;
3. direct governed realization/enforcement/validation/proof relationships extend protection when available;
4. candidates can trigger provisional caution without becoming authority;
5. empty/incomplete coverage is explicit and never represented as safety;
6. hooks, bootstrap, CLI, audit, and preflight consume one canonical protection owner;
7. protection remains deterministic, path-safe, inspectable, and offline-capable;
8. the full proof suite and CI pass on the exact reviewed head SHA;
9. architect review reports no blocking findings.