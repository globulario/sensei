# Agent Usage — how an AI agent should use Sensei

Sensei gives you the project knowledge that isn't in the code: the invariants a
file must uphold, the failure modes it's involved in, the fixes that look right
but are known-broken, and the tests that pin the behavior. Consult it **before**
you edit, and record what you learn **after** you fix.

This page is the operational contract. For the wire surface see
[api-reference.md](./api-reference.md); for every CLI flag see
[cli-reference.md](./cli-reference.md).

## What Sensei is / is not

- It **is** compiled, reloadable awareness context: authored YAML
  (`docs/awareness/*.yaml`) + `@awareness` annotations, compiled into RDF and
  served over gRPC in ~2 ms.
- It is **not** runtime authority, desired-state authority, arbitrary SPARQL,
  or a replacement for tests, builds, and review. An `EMPTY` result is **not**
  proof an edit is safe.

## The seven tools

You reach these as MCP tools (`awareness_*`) or as `awg` subcommands. Pick by
what you're about to do — don't default to `briefing` for everything.

| About to… | Call | Why |
|---|---|---|
| Decide whether/how to touch an area | `preflight(task=, files=[…])` | typed `risk_class` + `confidence` to branch on before reading prose |
| Edit a known file | `briefing(file=)` | direct invariants + intent + patterns for that file |
| Chain on file anchors (no prose) | `impact(file=)` | typed nodes, cheaper to process |
| Check a concrete edit's content | `edit_check(file=, proposed_content=)` | advisory warnings if the edit's shape violates a rule |
| Expand a `referenced_id` | `resolve(class=, id=)` | the full node + provenance |
| Tell "no rule here" from "graph thin here" | `metadata` | overall coverage/freshness, once per session |
| Operator/debug browse | `query(mode=)` | typed, whitelisted — no raw SPARQL |

## Required pre-edit workflow

1. **Identify** the files you're likely to touch.
2. **Preflight** the task: `preflight(task=…, files=[…])`. Read `risk_class`
   **and** `status`.
   - `SECURITY_RISK` / `DATA_LOSS_RISK` → get explicit user approval before
     applying any edit.
   - `ARCHITECTURE_SENSITIVE` / `CONVERGENCE_RISK` → read everything in
     `files_to_read` and brief each file.
   - `UNKNOWN_IMPACT` → the graph can't classify; behave as `SECURITY_RISK`
     until proven otherwise. **`EMPTY` is never `LOW_RISK`.**
3. **Brief** each target file: `briefing(file=…)`. Read `status`, `prose`,
   `referenced_ids`, and any `implementation_patterns`.
4. **Resolve** any `referenced_id` marked `high`/`critical` before writing code
   that touches it.
5. **Edit.** Optionally `edit_check(file=, proposed_content=)` on your proposed
   content to catch a bad-shape change (e.g. a forbidden call) — it's advisory,
   never blocking.
6. **Run** the `required_tests` / `tests_to_run` the graph named.
7. **Record** durable knowledge you gained — see [After the fix](#after-the-fix-the-write-path).

If a tool errors or returns `DEGRADED`, say so explicitly ("awareness
unavailable/degraded") and fall back to reading local YAML, tests, and code.
Never pretend the constraints don't exist.

## Interpreting status

Every Briefing / Preflight / Impact response carries an explicit status.

- **`OK`** — anchors found. Treat the returned invariants, failure modes,
  forbidden fixes, and required tests as active context.
- **`EMPTY`** — no direct anchors for this file/task. **Not** "safe." Proceed
  carefully, say "no direct awareness anchors found for `<target>`," and check
  `metadata`: if the graph is healthy overall, the silence is probably real; if
  the graph is thin everywhere, the empty means nothing.
- **`DEGRADED`** / transport error — the backend is unavailable or returned a
  non-gRPC response. Don't make high-risk architectural changes without user
  approval; use code/tests/docs as fallback and say so.

## After the fix — the write path

When a fix taught you something durable (a new failure mode, an invariant you
clarified, a forbidden fix you ruled out, a regression test that pins it),
record it with **one typed call**:

```bash
awg propose --kind failure_mode --title "…" \
  --contract "<the contract that was violated/clarified>" \
  --related-invariant <inv.id> \
  --source-file <path> --required-test <file.go:TestName> \
  --evidence "<what you observed>"
```

`propose` appends the entry to the right YAML, rebuilds the seed, reloads the
local store, and `git add`s the change — then stops. **You review and commit.**
It is **contract-first**: every entry must answer what contract was
violated/clarified, what failure was observed, what test proves it, what fix is
forbidden, and which invariant/failure_mode it connects to. Vague notes are
rejected. If the contract is genuinely unknown, use `--kind contract_unknown`
with `--proposed-contract` or `--revision-request`; the entry is parked under
`docs/awareness/candidates/` until the contract is resolved.

The `Stop` hook (`awg feedback-check`) is advisory: it reminds you when a
session fixed a risky area but wrote no graph feedback. A session that already
proposed feedback (or changed nothing risky) stays silent.

## Safety constraints

- Never request or construct raw SPARQL. `query` is typed and whitelisted; the
  `sparql` field does not exist on the wire.
- Treat `EMPTY` as "unknown coverage," never as "no risk."
- Keep authored context (briefing/impact) and runtime observation separate in
  your reasoning — don't rewrite an invariant from a transient observation.

## End-of-task summary line

End non-trivial code tasks with a short awareness summary so the human can see
what you consulted:

```
AWG: preflight(<task>) → <risk_class> | briefing(<files>) |
     invariants: <ids> | forbidden-fixes avoided: <ids> |
     tests run: <ids> | proposed: <kind/id or none> | uncertainty: <what you couldn't verify>
```
