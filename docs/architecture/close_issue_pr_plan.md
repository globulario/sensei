# Sensei / Sensei-Code Post-W3 Convergence Plan

## Root objective

After W3 reaches a truthful successful end state, stop feature expansion and converge both repositories.

The objective is NOT:

> close as many PRs and issues as possible.

The objective is:

> For every open PR and issue in `globulario/sensei` and `globulario/sensei-code`, determine from current `main` whether its claimed defect is:
>
> 1. repaired and independently proven;
> 2. still reproducible;
> 3. superseded by another admitted change;
> 4. blocked by an external proof or authority boundary;
> 5. obsolete because its premise was disproven.

Only then merge, close, supersede, retain, or re-scope it.

Do not use W3 success as evidence for defects W3 does not exercise.

---

# Phase 0 — Preconditions

Do not begin convergence until W3 itself is genuinely eligible and completed.

Required W3 prerequisites:

* typed-evidence preflight is green for every governed artifact for semantically correct reasons;
* graph identity stack is reviewed;
* graph reader resolution is deployed;
* one-store-per-domain enforcement is deployed;
* installed binaries match the reviewed code;
* wrong-port/wrong-instance adversarial proof passes against installed binaries;
* ACTIVE GraphIdentity is pinned;
* no stale authority session is resumed;
* W3 starts as a NEW task/session.

Then execute W3.

W3 success must prove the intended durable reviewer-exchange lifecycle, including the durable request/response path and correct lifecycle/authority semantics.

If W3 fails, stop this convergence plan and classify the failure before opening new work.

Do not start backlog cleanup on the assumption that W3 will pass.

---

# Phase 1 — Freeze new architecture work

Once W3 passes, establish a temporary convergence freeze.

During this phase:

* no speculative architecture PRs;
* no new feature slices;
* no "while here" refactors;
* no unrelated cleanup;
* no new corpus entries merely to make old PRs easier to merge;
* no repair of a newly discovered defect unless it prevents convergence or represents an immediate correctness/security boundary.

New findings are recorded as issues unless they block the convergence procedure itself.

The operating rule is:

**converge before expanding.**

---

# Phase 2 — Snapshot both repositories

Capture the exact state before changing anything.

For both:

* `globulario/sensei`
* `globulario/sensei-code`

Record:

* current `main` SHA;
* clean-worktree status;
* open PRs;
* open issues;
* PR head SHA;
* PR base;
* mergeability;
* CI/check state;
* review state;
* dependency/stack relationship;
* files changed;
* whether the PR head is ancestor/descendant of another open PR;
* whether equivalent code already exists on `main`.

Produce two dependency graphs:

```text
PR dependency graph
```

and

```text
issue → repairing PR → dependent PRs
```

Do not rely on PR titles alone.

Inspect actual diffs and ancestry.

---

# Phase 3 — Classify every open PR

Every open PR receives exactly one disposition candidate.

Use these classes.

## A — MERGE CANDIDATE

Use only when:

* the defect/change is still relevant;
* the PR contributes unique required bytes;
* current head is reviewed;
* CI is real and executed, not vacuously green;
* dependencies are already landed or can be landed first;
* the change remains correct against current `main`.

Required evidence:

* exact head;
* exact reviewed head;
* successful real test execution;
* required mutation evidence;
* architectural/governance evidence;
* no stale-base assumptions.

Do not merge merely because CI is green.

---

## B — SUPERSEDED

Use when:

* another merged or merge-bound PR contains the effective repair;
* the original PR contributes no unique required behavior;
* retaining both would duplicate or conflict;
* the original architectural premise has been replaced.

Required proof:

* identify the superseding commit/PR;
* show the original defect is repaired without this PR;
* show no unique required file/change remains.

Close as superseded with the exact successor reference.

Do not merge old intermediate slices solely to preserve history.

Git already preserves history.

---

## C — ALREADY SATISFIED ON MAIN

Use when current `main` independently satisfies the PR objective.

Prove the original failing witness against:

1. the historical/broken revision where possible;
2. current `main`.

If it fails historically and passes on `main`, identify the commit that repaired it.

Close with evidence.

Do not infer satisfaction from similar-looking code.

---

## D — STILL REQUIRED BUT NEEDS REBASE/REVIEW

Use when the change is still needed but its evidence is stale because dependencies or `main` moved.

Do not blindly rebase first.

Before modifying the PR:

* identify which assumptions changed;
* determine whether its witnesses still express the correct invariant;
* rebase or reconstruct only after that analysis.

Then rerun:

* relevant tests;
* full suite where required;
* mutation witnesses;
* Sensei gate;
* adversarial review.

---

## E — PREMISE INVALID / OBSOLETE

Use when later investigation disproved the reason the PR existed.

Examples:

* a publication artifact that no consumer actually reads;
* a graph repair for a problem proven to live in recipe coverage;
* an architectural mechanism replaced by an existing mechanism discovered later.

Close without merging.

Record:

* original premise;
* measurement that disproved it;
* current correct model.

This is a successful outcome, not lost work.

---

## F — EXTERNAL / HUMAN / DEPLOYMENT BLOCKED

Use when implementation is complete but closure requires something outside repository code.

Examples:

* GitHub App live installation proof;
* a human review/authority decision;
* deployment validation;
* external service credentials or environment.

Keep open.

State exactly what external event closes it.

Do not create more code to "make progress" on an external blocker.

---

# Phase 4 — Converge the W3 / graph-identity commissioning stack first

Handle the large commissioning stack before unrelated backlog.

The likely families include:

## Sensei graph identity

Review current live state of:

* #357
* #358
* #359
* #360
* #361
* #362

Determine exact dependency structure from Git, not memory.

Expected conceptual order:

```text
#357 ACTIVE graph identity
    ↓
#359 G2 reader resolution
    ↓
#360 one store per governed domain
    ↓
#361 complete production-reader migration
```

while:

```text
#358 .sensei/project not corpus
```

and:

```text
#362 construction-confinement derivation
```

are logically separate unless ancestry says otherwise.

For each:

* review exact current head;
* establish whether later PRs already include earlier bytes;
* decide merge versus supersede;
* avoid replaying intermediate commits redundantly.

After each merge:

* update downstream bases;
* rerun exact affected evidence;
* do not assume pre-merge review proves post-rebase head.

---

# Phase 5 — Converge the Sensei-code commissioning stack

Audit all open Sensei-code PRs, especially the W1/W3 and typed-evidence chain.

Likely relevant family includes:

* #167
* #168
* #169
* #170
* #171
* #172
* #173
* #174
* #175
* #176
* #177
* plus the final authored-neighbour/test-grant repair if it exists under a later number.

Do not assume all of these should merge.

For each PR answer:

1. Does it contribute unique behavior still required after W3?
2. Is its behavior already included by a descendant?
3. Is it documentation/audit evidence that remains historically useful?
4. Is its base still meaningful?
5. Was its original defect invalidated by later typed-evidence work?
6. Is its exact head independently reviewed?

Pay special attention to stacked PRs.

If:

```text
A → B → C → D
```

and D contains all changes, do not automatically merge A/B/C/D one by one.

Determine whether the repository benefits from:

* sequential merges preserving semantic commits;
* squash/merge of selected slices;
* landing a consolidated descendant;
* closing ancestors as superseded.

Preserve review/provenance identity while minimizing unnecessary integration churn.

---

# Phase 6 — Deploy the integrated graph stack

After graph-related PRs are admitted to `main`:

Build binaries from admitted `main`.

Record:

* Sensei commit;
* Sensei-code commit;
* binary hashes;
* previous binary hashes;
* registry hash;
* ACTIVE generations;
* marker digests;
* store triple counts;
* service endpoints.

Deploy in controlled order.

Do NOT:

* rebuild graph;
* refresh graph;
* import corpus;
* bootstrap;
* change ACTIVE generation

unless explicitly required by a merged change and separately authorized.

Then rerun the wrong-instance adversarial proof.

Required scenarios:

### Correct service available + wrong service available

Every production reader must select canonical service.

### Canonical service unavailable + wrong service available

Every production reader must refuse.

No fallback.

### Explicit diagnostic override

Allowed only when visibly non-canonical and still subject to identity verification where applicable.

### Generation mismatch

Refuse.

Only after this proof may:

`wrong-port / wrong-instance selection`

be marked:

`CLOSED AND DEPLOYED`

instead of:

`CLOSED IN CODE`.

---

# Phase 7 — Re-run W3 once from integrated main if necessary

If the successful W3 proof occurred before the final integration stack landed, run one final W3 commissioning proof from admitted/deployed `main`.

Use:

* NEW task;
* NEW session;
* current ACTIVE GraphIdentity;
* clean worktree;
* installed reviewed binaries.

Do not reuse historical W3 sessions.

Required lifecycle proof should include:

* durable reviewer request written;
* process/waiter may disappear;
* request remains;
* reviewer response binds exact exchange;
* deadline remains the persisted deadline;
* withdrawal/supersession semantics hold;
* correct task resumes;
* no duplicate request/result is consumed;
* correct terminal/admission behavior occurs;
* review identity binds exact candidate/head.

This is the commissioning witness.

After it passes, freeze the receipt as the canonical W3 proof.

---

# Phase 8 — Audit independent Sensei defects

Now move to backlog that W3 does NOT prove.

Do not close these merely because W3 passed.

At minimum audit current open issues such as:

* #352 — tail truncation resurrects spent authority;
* #353 — destroyed ledger history resets terminal state;
* #354 — artifact-less appended event hides an earlier record;
* #355 — generation pointer escapes task directory;
* #356 — corrupt ledger entry panics verifier;
* #113 — GitHub App private-beta/live external proof.

For each reproduce against current `main`.

Classification:

```text
REPRODUCES
FIXED
SUPERSEDED
EXTERNAL
INVALID
```

If it reproduces, keep it open.

Do not repair it during this audit unless separately authorized.

The point is to establish the truthful residual backlog.

---

# Phase 9 — Audit independent Sensei PRs

Specifically inspect PRs such as:

* #350 task abandonment;
* #351 mutation-permission authority;

and any other open PR not part of W3 convergence.

For each ask:

* does its defect still reproduce?
* does its current head still solve it?
* has later architecture made it obsolete?
* is its review current?
* does it have unresolved semantic decisions?
* does it depend on graph/corpus commissioning after merge?

Do not merge #350 merely because task abandonment would be convenient.

Do not merge #351 merely because its tests pass.

Their own unresolved review/governance conditions remain authoritative.

---

# Phase 10 — Resolve stale and historical artifacts

Audit:

* stale authority questions;
* abandoned W3 sessions;
* old commissioning branches;
* obsolete candidate corpus;
* scratch clones;
* superseded local deployment binaries;
* legacy marker/state quarantines;
* historical task pointers.

Do not delete evidence casually.

For each artifact choose:

```text
preserve
archive
quarantine
superseded
delete-safe
```

Anything tied to an incident/audit remains preserved unless policy explicitly permits deletion.

Avoid leaving operational tooling able to accidentally discover archived legacy state.

---

# Phase 11 — Produce the convergence matrix

Produce one master table for EVERY open PR and issue across both repos.

Columns:

```text
repo
number
type
title
family
head/current revision
dependency
original claim
current reproduction
current evidence
disposition
action
blocking condition
successor/superseder
```

Allowed dispositions only:

```text
MERGE
CLOSE_FIXED
CLOSE_SUPERSEDED
CLOSE_INVALID_PREMISE
KEEP_OPEN_REPRODUCES
KEEP_OPEN_EXTERNAL
KEEP_OPEN_REVIEW
KEEP_OPEN_UNAUTHORIZED
```

No vague:

```text
probably done
looks obsolete
likely fixed
```

Every disposition needs evidence.

---

# Phase 12 — Execute closure in dependency order

After presenting the complete convergence matrix, execute only unambiguous actions.

Order:

1. merge reviewed prerequisites;
2. update/review descendants;
3. merge unique surviving descendants;
4. deploy integrated binaries;
5. rerun system proofs;
6. close superseded PRs;
7. close fixed issues;
8. leave independent reproducing issues open;
9. leave external/human-blocked items open.

Do not close issues before the fixing commit is admitted to `main`.

Do not mark a defect fixed merely because a PR exists.

---

# Phase 13 — Final clean-main proof

At the end, verify both repositories from clean fresh clones.

For Sensei:

* clean checkout;
* generated artifacts fresh;
* `go vet ./...`;
* full uncached `go test ./...`;
* Sensei enforcing gate;
* graph identity metadata;
* production-reader resolution;
* store ownership;
* no legacy default-port dependency.

For Sensei-code:

* clean checkout;
* `go vet ./...`;
* full uncached `go test ./...`;
* build;
* Sensei enforcing gate;
* typed W3 preflight;
* graph-domain handshake;
* pinned generation check.

Then perform the installed-runtime adversarial proof.

No claim of convergence until:

```text
source main
CI main
installed binary
ACTIVE graph identity
runtime behavior
```

all refer to the intended admitted state.

---

# Phase 14 — Final backlog report

Produce:

## Closed during convergence

For each:

* PR/issue;
* reason;
* fixing/superseding commit;
* proof.

## Merged during convergence

For each:

* exact merge commit;
* independent review;
* post-merge verification.

## Still open and reproducing

These become the real engineering backlog.

## Still open because external

Example: GitHub App live proof.

## Still open because human/review authority

No attempt to route around these.

## Obsolete/superseded

Explain successor.

## System state

Report:

* Sensei `main` SHA;
* Sensei-code `main` SHA;
* installed binary hashes;
* ACTIVE generations;
* graph digests/triple counts;
* registry hash;
* W3 proof receipt;
* open issue count;
* open PR count.

---

# Stop condition

Stop convergence when all open items have a truthful disposition.

Success does NOT require:

`0 open issues`

or:

`0 open PRs`

Success means:

> No open item is open accidentally, no closed item relies on inference, no superseded branch is masquerading as pending work, and every remaining item names a real independent blocker.

The desired end state is:

```text
one coherent mainline
+
one deployed coherent runtime
+
one canonical W3 proof
+
a short, explicit backlog of independent defects
```

not a GitHub page made artificially empty.

---

# Governing principle

**A governance system closes work because the evidence says the obligation is satisfied, not because the queue looks untidy.**

