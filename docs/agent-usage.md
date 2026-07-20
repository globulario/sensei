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

## Sensei Architect skill

`sensei init` installs the built-in Sensei Architect skill by default:

- `.sensei/skills/sensei-architect/` is the canonical repository-local copy.
- `.agents/skills/sensei-architect/` is for Codex / Agent Skills discovery.
- `.claude/skills/sensei-architect/` is for Claude Code project skill discovery.
- `.cursor/rules/sensei.mdc` points Cursor to the canonical package.

Use the skill before planning or editing architecture-sensitive work:
contracts, ownership, state layers, lifecycle, recovery, security, convergence,
patterns, proof, degraded coverage, audits, incidents, migrations, and PR
review. The skill should activate proactively, but proportionally. It may block
known contract violations, forbidden fixes, authority conflicts, security risk,
data-loss risk, or irreversible unverified transitions. It should warn or advise
without expanding scope for lesser findings.

The skill is process guidance, not authority. Sensei's reviewed corpus remains
the architectural memory. See [sensei-architect-skill.md](./sensei-architect-skill.md)
for installation and update details.

## Core awareness tools

You reach these as MCP tools (`awareness_*`) or as `sensei` subcommands. Pick by what you're about to do — don't default to `briefing` for everything.

| About to… | Call | Why |
|---|---|---|
| Decide whether/how to touch an area | `preflight(task=, files=[…])` | typed `risk_class` + `confidence` to branch on before reading prose |
| Edit a known file | `briefing(file=)` | direct invariants + intent + patterns for that file |
| Chain on file anchors (no prose) | `impact(file=)` | typed nodes, cheaper to process |
| Check a concrete edit's content | `edit_check(file=, proposed_content=)` | advisory warnings if the edit's shape violates a rule |
| Expand a `referenced_id` | `resolve(class=, id=)` | the full node + provenance |
| Tell "no rule here" from "graph thin here" | `metadata` | overall coverage/freshness, once per session |
| Operator/debug browse | `query(mode=)` | typed, whitelisted — no raw SPARQL |
| Record durable lessons | `propose(kind=, ...)` | review-queue candidates, never active authority |

## Task session & closure tools (Phase Two)

These tools are exposed through the MCP server and CLI for managing change sessions, change admission, and convergence.

| About to… | Call | Why |
|---|---|---|
| Initialize a task session | `prepare-change` | bind task details and run initial convergence + admission |
| Check task/session status | `task_status(repo=)` | read current task session and inspect convergence blockers |
| Brief a file in a task session | `task_briefing(repo=, file=)` | get active blockers and next actions for a specific file |
| Advance a task session | `advance_task(repo=)` | execute registered static probes and advance convergence |
| Evaluate change admission | `admit_change(bundle_dir=, ...)` | check if a proposed change is permitted within a convergence bundle |
| Verify diff scope compliance | `verify_admission(decision_path=, ...)` | verify that edited files stayed inside the admitted scope |

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

## Bounded task session & closure workflow (Phase Two)

For repositories governed by the Phase Two architectural closure protocol:

1. **Initialize the task session**:
   ```bash
   sensei prepare-change --repo <checkout> --repo-domain <domain> \
     --description "..." --mode modify --task-class <class> --risk-class <risk> \
     --direction <direction> --graph-nt <graph.nt> \
     --file modify:<path> ...
   ```
   This creates the task workspace and session context under `.sensei/tasks/<task-id>/`.
2. **Check session status**:
   Call `task_status(repo=)` or run `sensei task-status --active --compact` to read the current operational status and see if the session is blocked by incomplete architectural knowledge.
3. **Obtain file briefings inside the session**:
   Call `task_briefing(repo=, file=)` or run `sensei task-briefing --repo <checkout> --active --file <path>` to check blockers and the next action for a specific file.
4. **Advance static evidence**:
   If the session next action is `run_static_evidence`, call `advance_task(repo=)` or run `sensei advance-task --repo <checkout> --active`. This executes registered static-read probes to automatically resolve blockers.
5. **Handle dialogue/unresolved blockers**:
   If the session is waiting on dialogue/architect answers or external evidence, route the blocker to the closure skill. Manual inputs can be advanced using:
   ```bash
   sensei advance-convergence --closure-request <req.yaml> --claims <claims.yaml> --dialogue <dialogue.yaml> --evidence-state <state.yaml> --graph-nt <graph.nt> --repo <checkout> --question-created-at <RFC3339> --output-dir <dir>
   ```
6. **Request mutation admission**:
   Before editing, evaluate admission via MCP `admit_change` or the CLI:
   ```bash
   sensei admit-change --bundle <dir> --request <request.yaml> --graph-nt <graph.nt> --repo <checkout> --output <decision.yaml> --format yaml
   ```
   *Only proceed with editing if the decision is `admitted` or `admitted_with_conditions`.* Do not edit on `waiting` or `refused`.
7. **Verify scope compliance**:
   After modifying the code, verify the working-tree diff against the admission envelope:
   ```bash
   sensei verify-admission --decision <decision.yaml> --bundle <dir> --repo <checkout> --output <verification.yaml> --format yaml
   ```
   Ensure the status is `compliant` before proceeding to certification and closure.

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
sensei propose --kind failure_mode --title "…" \
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

The `Stop` hook (`sensei feedback-check`) is advisory: it reminds you when a
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
