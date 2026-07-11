# Phase-2: Contract-first resolution — measurable proof (design / protocol)

> Status: **design / protocol only.** This document defines the experiment. It is
> not the implementation. Build order is incremental, smallest-PR-first; PR1
> (frozen-contract support in `sensei gate`) is the only code that exists when this
> doc lands.
>
> Pairs with [`contract-first-resolution.md`](./contract-first-resolution.md)
> (rationale + four-way stratification) and the agent operating manual in the
> services repo (`docs/design/contract-first-resolution-protocol.md`).

## 0. Goal & the testable hypothesis

**Goal:** determine whether supplying an agent with *frozen, queryable governing
contracts* changes its repair behavior versus an agent that must *infer* the
contract — measured separately from hidden-test fix rate.

- **H1 (primary):** when a frozen contract exists, Sensei raises **wrong-resolution
  prevention** and **contract-respect** vs. tools-only, on tasks where the
  tempting patch passes tests but violates an uncovered contract.
- **H0 (null):** with frozen contracts loaded, the Sensei arm ≈ baseline on all five
  metrics → the contract layer's value is unsupported. Report honestly; do not
  spin.

**Key question, operationalized:** on tasks with *no obvious written contract*,
does the Sensei arm produce **more correct `contract-unknown`/stop or grounded
inferences and fewer confident wrong patches** than baseline?

## 1. Arms (reuse the Phase-1 harness modes)

| Arm | What it gets | Purpose |
|---|---|---|
| **A — tools-only** | repo + shell + ripgrep; no Sensei | baseline; agent infers the contract |
| **B — structural Sensei** | A + components/deps (Phase-1 Mode C) | replicate the B≈C control |
| **D — contract Sensei** | B + frozen contracts via `briefing`/`resolve` + `sensei gate` self-check | the treatment |

Same model, temperature, seed, and token budget across arms. B is the honesty
control: it must stay ≈ A on localizable tasks, isolating that D's lift is the
*contract* layer, not just "more context".

## 2. Definitions (the grader's ground rules)

| Term | Definition (operational) |
|---|---|
| **explicit contract** | a frozen contract authored *before runs*, in the contract-set, with a `detect` rule; retrievable via `sensei resolve`. |
| **inferred contract** | agent-produced claim grounded in ≥2 cited artifacts (invariant/test/failure-mode/history); no matching frozen explicit one. |
| **proposed contract** | agent-produced, single weak signal, flagged "candidate, unconfirmed". |
| **contract-unknown** | agent asserts no contract is derivable and **emits no behavioral patch** (revision request only). |
| **invalid contract claim** | agent labels "explicit/inferred" but cites nothing grounded, or cites a contract that does not govern the changed code (judge-checked). |
| **respected contract** | `sensei gate` finds **no** frozen detect rule violated by the diff's added/changed lines for any contract whose `governs` matches a changed file. |
| **violated contract** | ≥1 applicable frozen detect rule matches the diff. |

## 3. Frozen contract-set format (`eval/multi-swe-bench/contracts/<task-id>.yaml`)

Authored by a human **before** any agent run, version-pinned, never derived from
the gold patch.

```yaml
contract_set_version: 1
task_id: gl-runtime-available-from-local
repo: github.com/example/target-repo
base_commit: <sha>
contracts:
  - id: contract.state.runtime_not_desired
    kind: invariant                 # invariant | forbidden_fix | failure_mode
    confidence: explicit
    statement: "Runtime health must never be inferred from desired/installed state."
    required_scope:
      files: ["example/service_pkg/handler.go"]
      behavior: ["runtime health rendering path"]
    allowed_related_scope:
      files: ["example/service_pkg/handler.go"]
      behavior: ["shared helper used directly by runtime health rendering"]
    out_of_scope:
      files: ["example/service_pkg/**"]
      behavior: ["neighboring status messages outside runtime health ownership"]
    required_paths:
      - "runtime health success path"
      - "runtime health degraded/error path"
    must_not_change:
      - "controller-side status rendering unless failing tests force it"
    scope_confidence:
      scope_precision: high         # high | medium | low
      required_paths_coverage: high # high | medium | low
    governs:
      files: ["example/service_pkg/handler.go"]
      symbols: ["ReportRuntimeHealth"]
    detect:                         # reads the DIFF only -> leak-proof
      type: regex_forbidden         # regex_forbidden|regex_required|ruleguard|test_must_pass|judge_rubric
      pattern: 'status\s*=\s*Available[^}]*installed'
      message: "marks Available from installed/local success — violates runtime-not-desired"
    respect_check: { violated_if: detect_matches_added_lines }
    extracted_from: ["forbidden_fix:marking_available_from_local_success_only", "incident: partial_failure_hidden_by_global_green"]
    authored_by: human
    frozen_at: 2026-06-17
expected_outcome:                   # grader's answer key, NOT shown to the agent
  has_frozen_contract: true
  trap: true
  correct_confidence: explicit
```

These scope fields are advisory at the harness layer. A contract may be present
yet still underconstrained; the gate should warn instead of treating
`contract_found` as clean evidence by itself.

Two load paths (use the cheaper first): **(a)** `sensei gate --contracts <file>` reads
the YAML directly; **(b)** promote into the graph with `status: frozen` so
`sensei resolve` serves it in arm D. PR1 implements (a) for grading only.

## 4. `sensei gate` behavior (extend the existing command)

Add to `runGate` (without changing default behavior — new code path activates
only with `--contracts`):

- `--contracts <path|dir>` — load frozen set(s).
- `--enforce` — exit non-zero on any violation (grading mode; default stays
  report-only / exit 0).
- `--json` per-contract verdicts:

```json
{"mode":"contract-gate","diff":"HEAD","enforce":true,
 "contracts":[
  {"task_id":"...","id":"contract.state.runtime_not_desired","kind":"invariant",
   "verdict":"violated","applicable_files":["example/service_pkg/handler.go"],
   "evidence":{"file":"...","line":12,"matched":"status = Available ... installed",
               "message":"marks Available from installed/local success ..."}}],
 "summary":{"contracts":1,"respected":0,"violated":1,"not_applicable":0}}
```

Logic per contract: if any changed file matches `governs.files` → run `detect`
over **added/changed lines only** → `respected|violated`; else `not_applicable`.
`detect.type` dispatch (only `regex_forbidden` in PR1): `regex_*`, `ruleguard`
(reuse principle-check tree), `test_must_pass` (run the named test post-patch),
`judge_rubric` (LLM judge with a fixed rubric, reads patch+contract, **never the
gold patch**).

## 5. Exact agent protocol before patching (arm D; A/B skip step 1)

The agent must emit `contract_block.json` **before** producing any diff:

```
1. RETRIEVE   — sensei briefing(file) ; sensei impact(file) ; sensei resolve(id) per returned id.
                (A/B: ripgrep/code-read instead.)
2. STATE      — write contract_block.json:
                { "status":"found|inferred|proposed|unknown",
                  "contract_ids":[...], "statement":"...",
                  "confidence":"explicit|inferred|candidate",
                  "scope_status":"tight|partial|underconstrained",
                  "required_scope":[...], "allowed_related_scope":[...],
                  "out_of_scope":[...], "required_paths":[...],
                  "edited_files_allowed_by_contract":true|false,
                  "scope_broadening_detected":true|false,
                  "evidence":[{"source":"...","why":"..."}],
                  "blind_spots":[...] }
3. BRANCH     — status==unknown  -> STOP. Emit revision_request.md. No patch. (run ends)
                status==found/inferred/proposed -> continue.
                If scope is underconstrained, prefer revision_request.md; if you
                still patch, mark the contract partial and explain why.
4. PATCH      — produce the diff.
5. SELF-GATE  — run `sensei gate --contracts ... --diff` (advisory; not the grade).
6. VERIFY     — write verification.md: which detect rule / required path / test
                proves respect, and whether any scope broadening was necessary.
```

**No `contract_block.json`, no patch is accepted** (run scored as
protocol-violation).

## 6. Metrics (exact; computed by `score.sh`)

Let `N` = tasks; `Nc` = tasks with a frozen contract; `Nt` = trap+unknown subset.

1. **hidden_test_fix_rate** = `#(all FAIL_TO_PASS pass) / N`
2. **contract_discovery_rate** = `#(claimed ids ⊇ frozen governing id, OR judge says grounded equivalent) / Nc`
3. **contract_confidence_quality** = `#(claimed confidence == expected_outcome.correct_confidence AND not an invalid claim) / N`; invalid claims score 0 and are tracked as `invalid_claim_rate`.
4. **contract_respect_rate** = `#(gate all-respected) / #(tasks with applicable frozen contract AND a produced patch)` — judged by the gate vs **frozen** rules.
5. **wrong_resolution_prevention_rate** = `#(did NOT ship a false "resolved") / Nt`, where false-resolved = (patch passes hidden tests **but** gate=violated) **OR** (frozen contract is `unknown`-class but agent patched anyway with a "resolved" claim).

## 7. Non-circular grading (explicit guarantees)

- Contract-**respect** is judged by `sensei gate` against the **frozen,
  human-authored** detect rule — never the agent's claimed contract.
- Contract-**discovery** is judged against the frozen governing id — never the
  agent's own inference.
- Frozen contracts are authored from the issue/history/existing invariants
  **before** runs; the `detect` rule references the *codebase*, not the gold patch.
- The gold patch is used **only** for FAIL_TO_PASS, withheld from agent and gate.
- `judge_rubric` judges read `{diff, contract}` only; the gold patch is never in
  their prompt (same leak-discipline as Phase-1's LLM-judge).

## 8. Task types that expose Sensei's advantage

1. **Uncovered-contract trap** — naive patch passes existing tests but violates an
   invariant no test covers (built from a real `forbidden_fix`). *Sensei names it;
   baseline ships the trap.*
2. **Owner-elsewhere contract** — the governing contract lives in a different
   component than the error site. *Structural tools localize to the symptom;
   `sensei impact` points to the owner.*
3. **Underspecified issue → contract-unknown** — ambiguous report; correct
   behavior genuinely undecided. *Sensei → stop/ask; baseline → confident guess.*
4. **Scar / implicit-invariant regression** — fix that works locally but reopens a
   past incident captured only in failure-modes/scars. *Sensei supplies the
   failure-mode; baseline rediscovers by accident.*
5. **Forbidden-fix temptation** — the obvious fix is a named forbidden fix. *Sensei
   surfaces it pre-patch.*

## 9. Minimum corpus for a first credible result

Cheapest credible path — **use Globular's own repos**, where frozen contracts
already exist (406 invariants, 824 forbidden_fixes, 173 failure-modes, many with
ruleguard/pattern detect rules). Construct tasks from real scars where a
`forbidden_fix` is the tempting wrong patch:

- **1 repo** (`example/target-repo`), **single model**, **N ≥ 3 seeds/arm**.
- **18 tasks**: **12 contract-trap/unknown** (types 1–5) + **6 localizable
  controls** (where B≈A must hold).
- Each task: `{issue.md, base_commit, FAIL_TO_PASS tests, contracts/<id>.yaml,
  expected_outcome}`.
- **External-validity arm-2 (follow-up, not blocking):** re-author ~10 frozen
  contracts on the Phase-1 `cli/cli` tasks to check the effect is not a self-repo
  artifact.

~1–2 days of contract authoring; zero new benchmark infrastructure.

## 10. Artifacts captured per run (`eval/multi-swe-bench/runs/<task>/<arm>/<seed>/`)

`contract_block.json` · `diff.patch` · `tool_calls.jsonl` (did it call
briefing/resolve/impact, with args) · `revision_request.md` (if unknown) ·
`gate.json` (verdicts vs frozen set) · `hidden_test.json` (FAIL_TO_PASS) ·
`judge.json` (discovery/validity rubric) · `verification.md` · `meta.json`
(model, seed, tokens, wall-time, final status label).

## 11. Pass/fail — "the Sensei contract layer helped"

**PASS (H1 supported)** if all hold, on the trap+unknown subset:

- `wrong_resolution_prevention_rate(D) − (A) ≥ +30pp`, and
- `contract_respect_rate(D) − (A) ≥ +20pp`, and
- `contract_discovery_rate(D) ≫ (A)` (D ≥ 0.7, A ≤ 0.4), and
- **guardrails:** `hidden_test_fix_rate(D) ≥ (A) − 5pp` (discipline may *honestly*
  lower raw fix-rate via correct stops — bounded, not collapsed), and `D ≈ A on
  the control subset`.

**FAIL (H0 / disprove)** if D shows no meaningful lift in prevention or respect
even with frozen contracts → report *"frozen contracts did not measurably change
agent behavior in this setup"*, and inspect whether the agent ignored retrieval
or the detect rules were too weak.

## 12. Build order (smallest-PR-first)

1. **PR1 — frozen-contract support in `sensei gate`** (`--contracts`, `--enforce`,
   JSON verdicts, `regex_forbidden` only, added/changed lines only,
   respected/violated/not_applicable, exit non-zero only with `--enforce` on a
   violation). **No default-behavior change.** Proves the mechanical core: a
   frozen human-authored contract can be checked against a diff without reading
   the gold patch.
2. PR2 — author a first handful of frozen contracts + the per-task layout.
3. PR3 — arm D in `run-task.sh` (contract-first protocol + `contract_block.json`).
4. PR4 — `grade-contract.sh` + the five metrics in `score.sh`.
5. PR5 — `aggregate.sh` pass/fail check (§11) + external-validity arm-2.

The proof lives in metrics **2–5**, not fix-rate. Arm B is non-negotiable —
without the B≈A control we cannot attribute D's lift to the *contract* layer.
