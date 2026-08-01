# ARCHER Integration Closure

Status: architecture contract

## Purpose

Complete Sensei's ARCHER-inspired governed synthesis loop after the accepted O1 through O4 foundations.

The useful ARCHER pattern is retained:

```text
interpretation -> planning -> generation -> evaluation -> bounded retry/replan
```

Sensei adds the authority chain ARCHER does not own:

```text
candidate-ready receipt
  -> existing admission owner
  -> exact sealed candidate application
  -> existing admission verification owner
  -> completion evidence
```

This contract closes the remaining gap without granting a provider architectural, mutation, admission, completion, approval, merge, or promotion authority.

## Current accepted foundation

- O1 owns the immutable synthesis session, typed state machine, attempt/replan budgets, and terminal receipt.
- O2 owns the provider-neutral capability, request, result, observation, and receipt boundary.
- O3 owns exact repository snapshotting, disposable candidate workspaces, independently computed change evidence, and the sealed CandidateArtifact.
- O4 owns deterministic evaluator composition and the mapping from evaluation evidence into an O1 recommendation and terminal candidate-ready receipt.

O4 deliberately stops at `candidate-ready-for-admission`.

## Remaining implementation sequence

The remaining work is split into four bounded pull requests. Each pull request must be independently reviewable and mergeable.

### O5A: admission composition contract and bridge

Create `golang/architecture/admissioncomposition`.

The package composes, but never replaces:

- `synthesis.Receipt`;
- `runnercomposition.RunnerReceipt`;
- `runnercomposition.CandidateArtifact`;
- `evaluatorcomposition.EvaluationReceipt`;
- `admission.Request`;
- `admission.Decision`;
- `admission.Verification`.

It owns a new closed receipt family:

- `sensei.admissioncomposition.request.v1`;
- `sensei.admissioncomposition.receipt.v1`.

The request binds the exact candidate-ready chain and the caller-supplied existing admission context. The receipt records the unchanged admission decision and, when available, verification digests beside the frozen O1 receipt.

Hard laws:

1. O5 never mutates the O1 receipt.
2. The synthesis receipt must be terminal with reason `candidate-ready-for-admission`.
3. The O1 final attempt/evaluation digests must match the O3/O4 chain exactly.
4. The O3 runner disposition must be `verified`.
5. The sealed CandidateArtifact digest and every structural lineage field must recompute and match.
6. Requested file scope is derived from the difference between the exact base manifest and sealed final manifest, never from provider prose.
7. A created, deleted, renamed, copied, type-changed, submodule, or otherwise unsupported operation is preserved as an explicit unsupported operation. It is never silently downgraded to modify.
8. The existing admission owner decides admitted, admitted-with-conditions, waiting, refused, or uncertifiable.
9. A passing O4 evaluation is not admission, correctness, completion, approval, or merge authorization.
10. The receipt is append-only evidence and preserves all refusal or limitation detail.

The first implementation checkpoint may expose a pure bridge over `admission.EvaluateLoaded` so tests remain hermetic. A filesystem wrapper may delegate to `admission.Evaluate` without changing semantics.

### O5B: governed candidate application and verification

Create `golang/architecture/candidateapply`.

This package materializes only an admitted sealed CandidateArtifact into a dedicated target worktree or target directory supplied by the caller.

Hard laws:

1. Application requires an O5 receipt whose decision is admitted or admitted-with-conditions.
2. The exact artifact digest admitted is the only artifact that may be applied.
3. The provider is never asked to replay edits.
4. The target must be bound to the admitted base revision and must be clean before application.
5. Materialization preserves raw bytes, executable mode, symlinks, deletions, and the canonical manifest.
6. Reserved control paths, path traversal, case-fold collisions, Unicode-normalization collisions, and escaping symlinks fail closed.
7. After materialization, input, final-tree, and proposed-change digests are recomputed independently.
8. Any mismatch refuses verification and preserves the target for inspection according to explicit cleanup policy.
9. Existing `admission.Verify` or `admission.Verification` semantics are reused; no parallel scope verifier is introduced.
10. Application does not commit, push, merge, or publish.

The package owns closed documents:

- `sensei.candidateapply.request.v1`;
- `sensei.candidateapply.receipt.v1`.

### O6: command provider adapter

Create `golang/architecture/commandprovider`.

This is the first real O2 adapter. It executes an explicitly configured external command, such as Claude Code or Codex CLI, behind `providerport.Provider`.

Hard laws:

1. Provider command, arguments, working directory capability, environment allowlist, and operation capabilities are explicit configuration.
2. No shell interpolation. Use direct argv execution.
3. The provider receives one closed O2 request through stdin and must return one closed O2 result through stdout.
4. Provider stderr is bounded observation evidence, not authority.
5. Ambient credentials are not copied unless explicitly allowlisted.
6. Cancellation and deadlines terminate the complete process group.
7. Output byte limits and observation limits are precommitted.
8. Unknown fields, malformed JSON, digest mismatch, unsupported operation, multiple JSON documents, or trailing non-whitespace output become typed invalid output.
9. Provider result payloads remain untrusted until the existing O2 mapping and O1 transition accept them.
10. Tests use a deterministic helper process; CI requires no external provider credential.

Thin CLI-specific constructors may be added one at a time after the generic command adapter closes. The first constructors may describe Claude Code and Codex CLI argv conventions, but may not embed authentication or vendor policy in the orchestration owner.

### O7: bounded synthesis driver

Create `golang/architecture/synthesisdriver` and a CLI surface `sensei synthesis-run`.

The driver composes existing owners through injected interfaces:

```text
create/resume O1 session
  -> O2 interpretation
  -> O1 transition
  -> O2 planning
  -> O1 transition
  -> O3 generation
  -> O4 evaluation
  -> O1 retry/replan/terminal transition
  -> O5 admission
  -> O5B apply
  -> admission verification
  -> terminal driver receipt
```

Hard laws:

1. The driver switches only on typed state, outcomes, and receipts. It does not reinterpret provider prose.
2. Retry and replan budgets remain the immutable O1 budgets.
3. Every retry starts from the pinned admitted base snapshot unless an explicit later contract authorizes another parent.
4. Infrastructure failure, typed provider non-completion, evaluator unavailability, admission refusal, apply failure, and verification failure remain distinct terminal outcomes.
5. The driver cannot select another candidate after evaluation.
6. The driver cannot enlarge scope, accepted conditions, budgets, evaluator policy, or provider capabilities.
7. The driver persists or returns every immutable artifact and digest required to audit the run.
8. A synchronous CLI may use an in-memory artifact store for the first checkpoint; durable resume is a later storage adapter behind the same interfaces.
9. The CLI defaults to no application. `--apply` must be explicit and must target a dedicated worktree path.
10. No automatic commit, push, PR, approval, or merge.

The driver owns:

- a closed policy/config document;
- a closed terminal driver receipt;
- deterministic orchestration tests with fake providers/evaluators/admission/apply owners;
- end-to-end tests with the command-provider helper.

## Completion proof matrix

The final integration is closed only when exact-head tests prove at least:

- happy path through admitted application and scope-compliant verification;
- O4 accept followed by admission refusal;
- admitted-with-conditions with exact acknowledgement binding;
- waiting and uncertifiable admission outcomes;
- candidate artifact tampering;
- wrong O1 attempt or evaluation lineage;
- base revision drift;
- dirty target refusal;
- unsupported candidate operation refusal;
- apply digest mismatch;
- verification scope violation;
- provider crash;
- provider timeout and process-group cleanup;
- malformed or oversized provider output;
- retry then success;
- replan then success;
- retry and replan exhaustion;
- evaluator unavailable;
- deterministic replay of the typed transition and receipt chain;
- no commit, push, PR, merge, or canonical knowledge mutation.

## Pull request order

1. `O5A admission composition`
2. `O5B governed candidate apply and verification`
3. `O6 command provider adapter`
4. `O7 bounded synthesis driver and CLI`
5. `ARCHER end-to-end closure proof`

No later PR may merge before the preceding contract is accepted on an exact head.

## Final completion truth

ARCHER integration is complete when the following statement is demonstrably true:

> Given an exact repository, base revision, task, graph and closure identity, immutable budgets, provider configuration, evaluator policy, and admission context, Sensei can obtain an interpretation and plan, generate a sealed candidate, evaluate it, retry or replan within precommitted limits, submit only the accepted candidate to the existing admission owner, apply only the admitted artifact into a dedicated governed target, verify the exact result through the existing verification owner, and preserve a complete digest-bound receipt chain without granting the provider architectural, mutation, admission, completion, approval, merge, or promotion authority.
