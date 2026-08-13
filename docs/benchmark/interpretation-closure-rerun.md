# R2 — Interpretation Authority Closure dogfood protocol

Persistent protocol for testing whether PR #164 (Interpretation Authority
Closure) behaves as designed against the frozen R1 pilot premises. This file is
the authority; conversation context is not. Read this, not chat history.

**This is an experiment, not a debugging exercise and not a score-optimization
exercise.** Do not modify interpretations, prompts, expected outcomes, task
definitions, source restrictions, scoring rules, or evaluation criteria in
response to observed results. If a genuine harness defect is found: stop,
classify it explicitly, repair it independently, restart affected measurements.
Never silently reinterpret an outcome.

## 1. Frozen historical baseline (R1) — do not modify or contaminate

```
BENCHMARKED
R1: f8a4e7639071            (code only; branch bench/r1-vendor-boundary-repair)
valid paired tasks: 9
Mode B: 9/9
Mode E: 5/9
```

Primary source: `awareness-graph-private/eval/multi-swe-bench/notes/R1-pilot-report.md`
plus `run-R1/` (attempt ledger, per-task `DIAGNOSIS.md`, `PREREGISTERED-*.md`).

| task | R1 Mode E | R1 attribution |
|---|---|---|
| grpc-go-3201 | PASS | exact surface, 1 file |
| grpc-go-2932 | PASS | semantically equal to upstream |
| grpc-go-3258 | PASS | exact surface, smaller than B |
| grpc-go-2631 | PASS | narrower than upstream |
| go-zero-1969 | PASS | broader invariant-driven realization; introduced a precision defect |
| grpc-go-3119 | FAIL | **governance-input** — completeness: required repair surface never disclosed |
| go-zero-2537 | FAIL | **governance-input** — truth: contradicted authored invariant |
| grpc-go-2630 | FAIL | **synthesis** — candidate did not compile |
| grpc-go-3361 | FAIL | **synthesis** — scope recall 1.00, interpretation sound, candidate compiled, repair wrong |

Split: governance-input failures 2 (2537, 3119); synthesis failures 2 (2630, 3361).
**Do not change this historical attribution.**

Excluded from the 9 (not failures): `go-zero-1783` incomplete (transport
deadline), `grpc-go-3351` (not a valid pair).

### Baseline reconciliation note (2026-08-13)

An intermediate revision of the R1 report read "8 valid pairs" and listed 3361
as never-run. The report itself was corrected in place when 3361 landed
("**CORRECTED after grpc-go-3361 landed**", report line 185). The 9-pair record
above matches the corrected primary source. No archaeology beyond this.

## 2. Feature under test

```
interpretation provider (O2)
  → candidate interpretation
  → independent InterpretationAuthority
  → truth challenge / completeness closure / proof obligations / realization
  → evidence-bound closure receipt
  → advisory OR governing
```

An O2 interpretation must no longer become governing merely because it was
generated and structurally valid.

### Semantic rules that must not be "improved"

| axis | value | effect on authority |
|---|---|---|
| truth | Supported | positive evidence |
| truth | **Unknown** | **NEUTRAL** — never blocking |
| truth | Contradicted | BLOCKING |
| completeness | complete | may govern |
| completeness | incomplete | must not govern |
| completeness | unknown | must not gain **hard** governing authority |
| realization | unknown | neutral at pre-synthesis |

Gate 1 is a **contradiction gate, not a universal theorem prover**. The
deterministic truth checker is explicitly Go-scoped. Invariants outside the
implemented static vocabulary may remain unknown. **Do not convert unknown into
failure.**

## 3. Hard experimental boundary

Reuse the original interpretations **exactly**. They are now regression
fixtures:

- Do **not** repair the false #2537 invariant before testing it.
- Do **not** add the missing #3119 file/surface before testing it.
- Do **not** rewrite #1969 to make the realization narrower.
- Do **not** add hints to #2630 or #3361.

The question is *"does the new architecture correctly classify the same premises
that previously governed synthesis?"* — not *"can we write better
interpretations this time?"*

Frozen artifacts: `eval/multi-swe-bench/interpretations/<task>.json` (11 files,
unchanged since R1).

## 4. Implementation surface as merged (verified 2026-08-13 @ 144c1ee2)

| element | location |
|---|---|
| policy engine | `golang/architecture/interpretationclosure/policy.go` — `Certify`, `Verify`, `VerifyForGoverning`, `VerifyCoverage`, `AssessCompleteness`, `AssessRealization` |
| Go truth checker | `interpretationclosure/go_checker.go` — `CheckGoTruth`, `UnknownTruth` |
| challenge plan | `interpretationclosure/challenge.go` — `LoadChallengePlan`, `ValidateChallengePlan`, `ChallengePlanDigest` |
| authority boundary | `synthesisdriver/interpretation_authority.go` — `InterpretationAuthority` interface |
| production authority | `synthesisdriver/closure_report_authority.go` — `ClosureReportAuthority` |
| CLI wiring | `cmd/awg/cmd_synthesis_run.go` — `--interpretation-challenge` |

### Probe vocabulary (complete)

`GoProbeTypeExists`, `GoProbeUnderlyingTypeEquals`, `GoProbeImplementsInterface`.
Type-level Go static facts only.

### Two properties that shape the experiment

1. **`--interpretation-challenge` is optional.** With no plan,
   `challengePlan.GoProbes` is nil, every claim is `TruthUnknown`, Unknown is
   neutral, and authority is unchanged. **Gate 1 is inert by default.**
2. **Nothing derives probes from interpretation text.** The challenge plan is a
   separate authored artifact that did not exist in R1. `ValidateChallengePlan`
   rejects authored *outcomes* (queries only), but the plan still chooses *what*
   is asked.

Consequence: whether 2537 is caught is determined by whether a probe targeting
`uuid.UUID` exists. Authoring that probe with knowledge of the R1 diagnosis
would make the test circular. See §5.

## 5. Blind challenge-plan derivation rule (pre-registered mechanism)

To keep Phase A falsifiable, challenge plans MUST be derived under this rule and
frozen before execution:

1. Inputs allowed: the frozen `interpretations/<task>.json` (its
   `objective`, `binding_invariants`, `relevant_contracts`) and the task
   repository at its base revision.
2. Inputs **forbidden**: `run-R1/work/<task>/**` (DIAGNOSIS.md, scope-recall,
   candidate-verify), the R1 report's per-task findings, the gold patch, and
   this protocol's §1 attribution table.
3. For each concrete named type appearing in the objective or in a binding
   invariant, emit every probe expressible in the 3-kind vocabulary whose
   expected value is entailed by the claim's own wording.
4. A claim not expressible in the vocabulary emits **no probe** and must
   surface as `TruthUnknown` — never as a failure.
5. Plans are written to `run-R2-closure/challenge/<task>.json`, digested with
   `ChallengePlanDigest`, and the digest recorded in the pre-registration
   before any run.

The derivation is performed once, recorded verbatim with its reasoning, and not
revised after seeing results.

## 6. Phase A — targeted regression (run first, report before Phase B)

Cases: **2537, 3119, 1969, 2630, 3361**. Pre-registered expectations:

| task | expected | failure-of-#164 condition |
|---|---|---|
| 2537 | truth = contradicted; authority advisory/blocked; synthesis must NOT proceed | reaches normal synthesis under the same false interpretation |
| 3119 | completeness = incomplete; missing surface explicit; blocked from governing | reaches normal synthesis as though complete |
| 1969 | not contradicted; completeness per original closure; realization **unknown/neutral**; proceeds | blocked *solely* because minimality is not mechanically establishable → overreach |
| 2630 | closure permits governing; synthesis proceeds; downstream verification rejects | blocked without new contradictory/incomplete evidence → false-positive closure |
| 3361 | closure permits governing; synthesis proceeds; repair independently evaluated | rescued/reclassified by closure → premise and synthesis quality wrongly coupled |

#164 must **not** claim to solve 2630/3361. They are the controls proving
premise quality and synthesis quality remain separate.

### Required result table

`task, historical_classification, truth_status, completeness_status,
realization_status, proof_status, closure_authority, synthesis_started,
candidate_produced, candidate_compiled, f2p, p2p, final_classification,
closure_receipt_digest, evidence`

p2p is `UNMEASURED` unless the official harness measured it. Never translate
unmeasured into zero.

## 7. Two independent result axes

Never reduce to pass/fail alone.

- **repair_correctness**: PASS | FAIL | NO_REPAIR | INCOMPLETE
- **governance_correctness**: CORRECTLY_GOVERNED | CORRECTLY_REFUSED |
  INCORRECTLY_GOVERNED | INCORRECTLY_REFUSED | UNRESOLVED

A correct refusal is an Interpretation Closure success and a SWE-bench
non-repair. Never merge them.

## 8. Phase B — exact nine-task rerun

Only if Phase A shows no implementation/harness defect. Tasks: 3201, 2932, 3258,
2631, 1969, 3119, 2537, 2630, 3361. Keep identical where mechanically possible:
repositories/revisions, task definitions, interpretations, source references,
bounded source surfaces, model/provider config, turn/tool budget, sealed
candidate rules, official evaluation harness, classification rules. Document
every unavoidable environmental difference. Gold patch is post-execution
diagnostic evidence only.

### Headline discipline

Do **not** headline `old E 5/9 vs new E N/9`. #164 intentionally introduces
legitimate refusal. The informative questions are: did 2537 and 3119 stop for
the *correct reason*; did known-good interpretations continue through closure;
did 2630/3361 remain synthesis problems; did any former E pass become an
incorrect refusal.

Ideal plausible result = 5 repairs + 2 correct refusals + 2 synthesis failures.
That is still **5/9 SWE-bench repairs**. Do not call it 7/9.

## 9. Primary regression to detect — Gate 1 selection pressure

Confirmed R1 invariants that are *not* trivially statically decidable:

| invariant | status |
|---|---|
| `underlying_kind_governs_assignability` | statically challengeable — FALSE |
| `no_self_sustaining_resolution_cycle` | not generally statically decidable |
| `failure_is_reported_not_encoded_as_empty_success` | not generally statically decidable |
| `only_new_policy_resets_policy_state` | not generally statically decidable |

Verify: no checker available → `TruthUnknown` → remains visible → does **not**
by itself block authority. If Sensei rejects strong architectural knowledge
merely for lacking a mechanical checker, stop and classify a **design
regression**. The system must not become governed by its shallowest knowledge.

## 10. Independent verification remains mandatory

Never infer `interpretation certified ⇒ repair correct`. Chain:

```
interpretation candidate → closure → governing premise → synthesis
→ sealed candidate → compile/tests/evaluation → repair result
```

2630 and 3361 exist to prove the final verification cannot be removed.

## 11. Post-pilot experiments (run isolated, never intermingled)

1. **bounded-B** — give B exactly E's bounded files. `unrestricted B vs bounded
   B` = access advantage; `bounded B vs bounded E` = governance effect. Never
   infer governance benefit from unrestricted B vs bounded E.
2. **invariant ablation** — (A) bounded model + task/files, (B) + explicit
   invariant, (C) full governed path. Isolates explicit architectural knowledge
   from the governance machinery. Pre-register predicted differences.
3. **interpretation robustness** — freeze multiple independently authored
   interpretations *before* running; compare truth/completeness/closure/repair.
   R1 calibration used LLM-authored interpretations; do not generalize that
   error rate to human architects. Never adapt an interpretation after seeing
   its outcome.
4. **pinned p2p** — official trusted harness (`MSWEB_HARNESS=1`). Until then
   regression safety is `UNMEASURED`; f2p success is not evidence of no
   regressions.

## 12. Denominator discipline

Keep separate: `VALID_PASS`, `VALID_FAIL`, `CORRECT_REFUSAL`,
`INCORRECT_REFUSAL`, `INCOMPLETE`, `PROVIDER_CAPACITY`, `TRANSPORT_FAILURE`,
`CONTAMINATED`, `UNMEASURED`.

A provider outage is not a repair failure. A run that never executed is not
zero. A correct refusal is not a repair pass. A contaminated run does not enter
the denominator. `0 executed == 0 failed` must never become evidence of success.

## 13. Pre-registration requirement

Before each experiment write an immutable record: hypothesis, exact task set,
revision/SHA, modes, available information, interpretation artifacts/digests,
expected **mechanism**, pass criterion, failure criterion, contamination
criterion, incomplete criterion, metrics, denominator. Commit/digest it before
execution.

Pre-register mechanisms, not merely outcomes:

> Bad: "Sensei should avoid the forbidden fix."
> Good: "Sensei should avoid mechanism X because invariant Y constrains
> operation Z; evidence Q will distinguish mechanism X from an observationally
> equivalent implementation."

## 14. Harness self-check before trusting any number

Tests actually executed? Denominator > 0? Expected tests mapped? Correct
backend? Contamination absent? Provider actually ran? Official harness used
where claimed? p2p genuinely measured? Candidate actually evaluated?

**A metric is not evidence merely because benchmark code calculated it.
Benchmark the benchmark before trusting the benchmark.**

## 15. No archaeology / artifact separation

R1 is closed. Do not reopen historical reconstruction unless a new experiment
exposes evidence the frozen record itself is wrong. New artifacts live in
`run-R2-closure/`, never in `run-R1/`. Never rewrite or contaminate R1.

State to work forward from:

```
BENCHMARKED   R1 f8a4e7639071, 9 pairs, B 9/9, E 5/9
UNDER DOGFOOD PR #164 Interpretation Authority Closure (merged 144c1ee2)
OWED          p2p, bounded-B, invariant ablation, interpretation robustness
```

## 16. Required final report

Exact tested Sensei SHA; exact benchmark/harness revision;
environment/provider/model configuration; pre-registration artifact/digest;
Phase A five-case results; full nine-case results; repair-correctness counts;
governance-correctness counts; any changed result vs R1 with exact causal
explanation; any false refusal; any bad interpretation that still became
governing; any closure receipt failing provenance/digest validation; any harness
defect discovered; which results are measured vs inferred/unmeasured; whether
#164's acceptance criteria are actually satisfied.

End with separate sections: **PROVEN**, **DISPROVEN**, **SUPPORTED BUT NOT
PROVEN**, **UNMEASURED**, **NEW FAILURE MODES**, **NEXT EXPERIMENT**.

Do not optimize the conclusion for Sensei. If the evidence weakens the
architectural hypothesis, report that plainly. The experiment succeeds if it
yields a more accurate model of Sensei, even if the benchmark score decreases.

## Core laws

1. Authority must be earned.
2. A benchmark must be structured so contrary evidence can change the
   conclusion without changing the rules.
3. Pre-register the mechanism, not merely the desired outcome.
4. For #164 specifically: **knowing something and being allowed to govern from
   it are different states.**
