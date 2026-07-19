# Phase 9.4b — completion gate: enforce + the three outcome classes + policy opt-in

This operationalizes the **9.4b** sub-slice already frozen in
[`phase9.4-contract.md`](phase9.4-contract.md). It adds nothing to that contract;
it turns the contract's 9.4b definition and its *already-locked* completion-policy
semantics into a concrete, reviewable implementation plan. 9.4a (advisory read +
report) is merged; 9.4c (action wiring + audit + the typed change-to-task binding)
remains **locked**.

## Scope (exactly this)

Add an **enforce** path to `sensei gate --completion` that:

1. reads the same canonical completion projection envelope 9.4a consumes
   (`BuildCompletionProjectionEnvelope` → `PublicationView` → `ValidateCompletionPublication`)
   — read-only, no mutation, no re-derivation of the verdict;
2. decides the outcome in the **three outcome classes** the contract forbids
   conflating (identity-invalid → block; runtime-unavailable → degraded pass;
   verdict-available → apply verdict policy);
3. is governed by a **new, explicit `completion:` policy section** — a distinct
   schema, never the EditCheck `Rules` map — with per-domain opt-in and the
   `require_completion` option;
4. exits non-zero **only** when an adopted domain's policy says block; advisory
   and unadopted domains still exit 0.

**Identity source in 9.4b stays the explicit `--task-dir` (as in 9.4a).** The
typed, provider-neutral `completion.change_task_binding/v1` (PR ↔ task, head-SHA
verification) is **9.4c** — 9.4b must not infer identity from branch/PR/title/dir
scan/active-pointer, and must not implement the binding schema.

## The enforce decision (from the contract's mapping tables)

Given an adopted domain in `mode: enforce`:

| Envelope outcome | Class | Enforce result | Exit |
|---|---|---|---|
| available · `authoritative_completion` | verdict | pass | 0 |
| available · `not_completed` | verdict | pass **unless** `require_completion` → block | 0 / 1 |
| available · `broken_completion` | verdict | **block** | 1 |
| available · `contradictory_terminal_history` | verdict | **block** | 1 |
| available · `unsupported` | verdict | **block** (computed non-establishable world) | 1 |
| unavailable · **identity invalid** (task identity absent/ambiguous/malformed/mismatched, incl. repository/task one-world violation) | identity | **block — cannot verify** | 1 |
| unavailable · **runtime** (identity established, then owner/store/Sensei unreachable or errored) | runtime | **degraded pass** (annotated) | 0 |
| invalid publication (`ValidateCompletionPublication` fails) | — | **block** (a surface that cannot even present a canonical verdict must not silently pass under enforce) | 1 |

Advisory mode (and any unadopted domain) is unchanged from 9.4a: **report only,
exit 0** for every one of the above.

### The identity-vs-runtime split (the crux to get right)

The envelope's `unavailable` branch carries a typed availability class. 9.4b must
map cause, not collapse it:

- `task_directory_unresolved` (no task identity / unreadable task path / the
  explicit `--task-dir` does not resolve to exactly one verified task) → **identity
  invalid → block under enforce.** An absent identity is a caller failure, never an
  outage; degrading it to a pass is the *identity-bypass* forbidden fix.
- an owner error whose cause is a **runtime** failure (store/owner unreachable or
  errored after identity was established) → **runtime unavailable → degraded pass.**

Today `projection_owner_error` bundles "owner refused because identity/one-world is
invalid" together with "owner errored at runtime." **A 9.4b design task is to split
that cause** (either a finer availability class from the owner, or a gate-level
determination that identity was established before the runtime error) so identity
failures can never ride the runtime lane into a degraded pass. This split must be
proven adversarially (see below), because it is the exact bypass the contract names.

## The `completion:` policy (schema designed here; semantics already locked)

A new section in the per-repo gate policy, **distinct from the EditCheck `Rules`
map** — the completion verdict is not an EditCheck rule and must never be forced
through `Rules` or activated by EditCheck rule inheritance.

```yaml
completion:
  default: advisory            # advisory | enforce
  domains:
    github.com/globulario/sensei:
      mode: enforce            # advisory | enforce
      require_completion: true # optional; only meaningful under enforce
```

Locked semantics (from the contract — **not** re-decided here):

- **no `completion` section / no matching domain entry → advisory** (nothing
  enforces without explicit adoption);
- **one domain cannot activate another domain's gate** (a foreign-domain verdict
  never gates this repo);
- **`require_completion` is separate from pathological-verdict enforcement** —
  enforce blocks broken/contradictory/unsupported regardless; `require_completion`
  *additionally* blocks `not_completed`;
- **EditCheck rule inheritance must not activate completion enforcement**;
- **malformed or contradictory completion policy fails loudly** — never silently
  defaults to enforce or advisory.

New JSON schema under `docs/schemas/…` (a `completion_gate_policy` shape,
`additionalProperties: false`), a typed loader/validator (mirroring `gate_policy.go`
but a **separate** type — not folded into the EditCheck `Rules` map), and valid +
invalid fixtures (unknown key, bad enum, contradictory domain, `require_completion`
without enforce).

## Required adversarial proofs (the contract's 9.4b bar)

- **the three classes are distinct** — identity-invalid blocks, runtime-unavailable
  degraded-passes, computed pathological verdict blocks; and specifically that an
  identity-invalid envelope can **never** be served as runtime-unavailable
  (construct repo-enforces + broken/absent task binding → assert block, not pass);
- **EditCheck rule inheritance cannot activate completion enforcement** — a policy
  whose `Rules` map is in enforce/block mode, with no `completion` adoption, leaves
  the completion gate advisory (exit 0);
- **no per-domain opt-in → advisory** — an enforce-capable binary on an unadopted
  domain blocks nothing;
- **foreign-domain adoption never gates this repo**;
- **malformed completion policy fails loudly** (non-zero, explicit error) rather
  than silently choosing enforce or advisory;
- **fail-safe on runtime outage** (degraded pass) vs. **fail-closed on a computed
  pathological verdict** (block) — the two must not be swapped;
- **still zero mutation** under enforce (task-dir content hash + assessment digest +
  ledger head unchanged), and **no `require_completion` bypass** of pathological
  blocking.

## Governance (records already forward-declared by the 9.4 contract)

9.4b implements against the govern-first records the 9.4 contract already landed and
`golang/coverage/phase9_contract_test.go` already asserts:

- `closure.completion_gate_fails_open_on_unavailability_and_closed_on_a_computed_verdict`
- `closure.completion_gate_requires_explicit_identity_when_enforcement_applies`
- `closure.completion_gate_conflates_unavailability_with_a_broken_verdict`
- forbidden: `phase9_gate_fails_closed_on_sensei_unavailability`,
  `phase9_gate_enforces_without_per_domain_opt_in`,
  `phase9_gate_treats_missing_required_task_identity_as_runtime_unavailability`

If 9.4b surfaces a genuinely new obligation (e.g. the identity/runtime cause-split),
it is added govern-first with its own coverage assertion before the code.

`CorrectnessCertified` stays false (Phase 6 sole writer); the gate never mutates,
never claims completion, never becomes terminal authority.

## Proposed bounded checkpoints (each its own review + STOP)

1. **Policy schema + loader** — the `completion_gate_policy` JSON schema, the
   separate typed loader/validator (fails loudly), fixtures, and the govern-first
   cause-split record if needed. No enforce wiring yet.
2. **Enforce decision + the three classes** — the decision table above wired behind
   `--enforce` / `--mode enforce`, the identity-vs-runtime cause split, exit codes,
   SARIF/text/JSON annotations (degraded / blocked). Advisory path untouched.
3. **Adversarial proof suite** — every proof in "Required adversarial proofs,"
   including the identity-bypass refutation and the EditCheck-inheritance isolation.

This doc opens 9.4b. It writes **no enforce code** — it is the design artifact for
review, exactly as `phase9.4-contract.md` opened 9.4. Awaiting the architect's exact
first-checkpoint boundary before implementation.
