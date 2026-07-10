# Hard-gate design: graduating EditCheck from warning to blocking

*Design only — a proposal, not implemented. No engine, rule, or graph changes
are made by this document. It records how warning-level enforcement could become
a true merge gate, and the guardrails that keep that safe.*

Today `EditCheck` is advisory: it evaluates a proposed edit's content against the
in-scope `detect` rules for a file and returns `severity: warning` — domain-scoped,
fail-closed on ambiguity, never blocking, never editing code. This document
designs the next step: a **CI pre-merge gate** that can *block* a merge on a
violation, without becoming the thing teams rip out the first week.

## Locked decisions

1. **Enforcement point:** a **CI / pre-merge gate**. The authoritative refusal
   happens in the repo's own CI, over the PR diff. Repo-owner-controlled,
   auditable, no local-machine trust required. (An agent-harness soft-stop —
   the editing tool calling EditCheck before it writes, like the
   awareness-briefing `PreToolUse` hooks — is a fine *earlier* nudge but is not
   the gate; humans editing directly would bypass it.)
2. **Availability policy:** **fail open**. If AWG or its store is unreachable,
   the gate passes with a "gate degraded" annotation. This honors the platform
   rule that *AI is supplementary, never required* and *fail safe* — an AWG
   outage must never halt every merge.
3. **Graduation + override:** a rule earns blocking status by **baking as a
   warning first**, and any block is **overridable via an explicit, audited
   in-diff token**.

## The gate, end to end

A repo that has adopted AWG for its own domain adds one CI job on PRs:

```
for each changed file F in the PR diff (within the gated domain):
    content = the file's ADDED/CHANGED lines (not pre-existing code)
    warnings = EditCheck(file=F, proposed_content=content, domain=<repo domain>)
    partition warnings into { warn-level, block-level } by the rule's enforcement
    drop block-level warnings that carry a matching // awg-allow override (audit them)
fail the check iff any block-level warning remains
```

- **Block-level violation on added/changed lines → CI fails → merge blocked.**
- **Warn-level → annotate** (PR comment / job log), never fails.
- **AWG unreachable → pass + "degraded" annotation** (fail open).
- **Only the diff is judged** — the gate never blocks on pre-existing code the PR
  didn't touch. You can adopt a blocking rule without first fixing the whole repo.

This needs a thin new **diff mode** (`awg gate --diff <range> --domain <repo>`)
that maps a diff to per-file EditCheck calls and aggregates. That is future
*implementation*, named here, not built.

## Rule model: an enforcement level

Add an optional field to the `detect` block:

```yaml
detect:
  forbidden_pattern: '\bfmt\.Errorf\('
  enforcement: warn        # warn (default) | block
```

- Emitted as a new `aw:detectEnforcement` literal (future vocab/producer change).
- **Default is `warn`.** Absent → advisory, exactly as today. Nothing already
  promoted changes behavior.

### Graduation: how a rule earns `block`

`awg promote` refuses `enforcement: block` unless the candidate attests it has
graduated — a small `graduation:` block recording:

- **review label** `load-bearing` (already required for pilot promotion),
- **baking evidence** — observed in `warn` mode across ≥ N PRs / a time window
  with a **low false-positive rate** (requires warn-mode telemetry; future),
- **precision** — the pattern has no known false positives on the gated paths.

The point: a rule may only block *after* the warning phase has proven it doesn't
cry wolf. Promotion is the gate; there is no auto-graduation.

## Override: explicit and audited

A blocking violation can be acknowledged in the diff, per occurrence:

```go
// awg-allow: caddy.reverseproxy.forwardauth_errf_preserves_location — intentional: this path returns a non-Caddyfile error
return fmt.Errorf("startup: %w", err)
```

- Downgrades **that rule, on that line** from block to a **logged override**
  (audit record: rule id, reason, file, commit, author). It is not a global mute.
- A reason is **required** — an empty override is itself a gate failure.
- Modeled on this repo's existing `[engine-ack]` acknowledgment in
  `check-commit-scope.sh` and the awareness-hook bypass philosophy: the escape
  hatch exists, but it leaves a durable, reviewable trace.

## The central risk: precision

A blocking regex false positive halts merges — the fastest way to get a gate
disabled. Mitigations, in order:

1. **Diff-scoped, added-lines-only** evaluation (above) — the blast radius is the
   change, not the repo.
2. **Block on `forbidden_pattern` matches only** in v1. `required_pattern`
   *absence* is a whole-file/context judgment (the pilot's
   `\bdispenser\.Errf\(` vs real `d.Errf(` mismatch already showed this is
   brittle) — it stays **warn-level** until structural matching exists.
3. **Graduation's "observed low FP in warn mode"** is the empirical guard — a
   rule with warn-mode false positives never reaches `block`.
4. **Later:** AST/structural matching for block-level rules instead of regex.

## Scope and adoption

- A hard gate only makes sense for a repo that has **adopted AWG for its own
  domain** — e.g. Caddy's CI gating Caddy rules, or Globular gating Globular.
  Globular gating its own domain is the natural first customer.
- **Foreign-repo rules never gate another repo's CI.** Domain scope already
  prevents cross-domain application; the gate runs **per-domain**. A Caddy rule
  hosted in Globular's AWG cannot block a Globular PR.
- Therefore hard-gate is **opt-in per repo-owner, per-domain** — never imposed.

## Audit

Every block, every override, and every degraded (fail-open) run emits a durable
record (CI job log at minimum; optionally an AWG audit entry). A gate you can't
review after the fact is folklore.

## Suggested rollout

1. **Warn-only** (today) — observe, collect warn-mode false-positive data.
2. **Block-new-only** — `enforcement: block` rules fail CI on added/changed lines;
   override available; fail-open; forbidden-pattern matches only. This is the
   hard-gate v1.
3. **Broaden later** — structural matching, required-pattern blocking, wider rule
   sets — each gated on warn-mode evidence.

## Non-goals (explicit)

- **No implementation** — this is design only.
- **No auto-promotion** of rules to `block`; promotion stays human-gated.
- **No fail-closed.**
- **No change to current warning-level behavior** — `warn` remains the default
  and the only thing that ships until a gate is deliberately built and adopted.
- **No new meta-principle** authored here.

## Open questions for the next decision point

- Where does warn-mode false-positive telemetry live, and what threshold
  graduates a rule? (Needed before any rule can legitimately reach `block`.)
- Is the agent-harness soft-stop worth shipping alongside, or after, the CI gate?
- Audit sink: CI log only, or a queryable AWG audit store?

---

*Status: design proposal for Phase B. Builds on the merged warning-level
enforcement (PR #25). See [`milestone-cold-bootstrap-v0.md`](milestone-cold-bootstrap-v0.md)
for the arc that led here. Nothing in this document is implemented.*
