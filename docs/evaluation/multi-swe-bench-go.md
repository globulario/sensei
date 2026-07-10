# Sensei Evaluation: Multi-SWE-bench Go Challenge

## Purpose

This evaluation tests whether Sensei improves an AI coding agent's ability to resolve real repository-level software issues.

The goal is not only to measure whether a patch passes tests. The goal is to measure whether Sensei helps the agent produce fixes that are:

- correctly localized
- smaller and safer
- architecture-aware
- invariant-preserving
- test-supported
- easier for a human maintainer to trust

Classic SWE-bench evaluates whether a model can generate a patch for a real GitHub issue. Multi-SWE-bench extends this kind of benchmark across multiple programming languages, including Go, TypeScript, JavaScript, Rust, C, C++, and Java.

For Sensei, the first evaluation target is the Go subset.

## Evaluation Name

**Sensei vs Baseline on Multi-SWE-bench Go**

## Main Question

Does Sensei make the same AI coding agent solve real Go repository issues more safely and more architecturally correctly than the same agent without Sensei?

## Hypothesis

Sensei should improve performance by giving the agent project-level awareness:

- relevant files and symbols
- known invariants
- architectural intent
- ownership and authority rules
- failure modes
- related incidents
- test obligations
- impact radius
- stale references or drift risks

Expected result:

Sensei mode should produce equal or better test-pass results while reducing wrong-file edits, architecture violations, oversized patches, and untested fixes.

## Benchmark Source

Use **Multi-SWE-bench**.

Initial scope:

- Language: Go
- Task count: 10 tasks
- Task type: real issue-resolution tasks
- Repository size: prefer medium to large repositories
- Avoid tasks that are purely documentation, formatting, or dependency-version bumps

Expansion scope:

- If the 10-task pilot is promising, expand to 50 Go tasks.
- After Go, repeat with TypeScript or Rust.

## Benchmark Repository

Use the official Multi-SWE-bench repository:

https://github.com/multi-swe-bench/multi-swe-bench

This repository contains the benchmark harness, task metadata, and instructions needed to run or inspect Multi-SWE-bench issue-resolution tasks.

## Run Modes

Each selected task must be run in three modes.

### Mode A: Agent Alone

The agent receives only:

- issue text
- repository checkout
- benchmark instructions
- test command if provided

No Sensei.
No special project briefing.
No custom invariant/rule context.

### Mode B: Agent + Normal Tools

The agent may use ordinary developer tools:

- grep/search
- file reads
- tests
- build logs
- compiler errors
- existing repo documentation

No Sensei.

This mode represents normal Cursor/Codex/Claude-style repository work.

### Mode C: Agent + Sensei

The agent receives the same access as Mode B, plus Sensei context.

Before editing, the agent must run or consume:

- `awg briefing` for the issue/repo
- `awg impact` for candidate files
- `awg resolve` for relevant symbols/rules
- `awg audit` or equivalent preflight if available

The agent must explicitly state:

- what Sensei facts influenced the patch
- which invariants or rules are relevant
- which files are safe to edit
- which files are risky to edit
- what tests are required
- what architecture risks remain

## Fairness Rules

All modes must use:

- the same model
- the same repository checkout
- the same issue text
- the same time/token budget
- the same final test command
- the same scoring rubric

The only difference between modes must be the availability of Sensei context.

Do not compare Claude+Sensei against a weaker model without Sensei. That would prove nothing.

## Task Selection Rules

Choose tasks that are likely to reveal Sensei value.

Prefer tasks involving:

- cross-file behavior
- service boundaries
- API or contract behavior
- configuration semantics
- error propagation
- ownership or authority
- concurrency/state transitions
- test expectations
- hidden architectural assumptions

Avoid tasks that are too small:

- one-line typo fixes
- pure dependency bumps
- README edits
- formatting-only changes
- simple missing imports
- trivial test snapshots

Sensei is not being tested as a spelling corrector. Sensei is being tested as a project-awareness layer.

## Required Output Per Run

For every task and mode, save an evaluation record:

```yaml
task_id:
repo:
language: Go
mode: A | B | C
model:
issue_summary:
start_commit:
final_patch_commit:
files_touched:
lines_added:
lines_removed:
tests_run:
tests_passed:
test_result:
build_result:
patch_summary:
agent_reasoning_summary:
awg_context_used:
  briefing_used: true | false
  impact_used: true | false
  resolve_used: true | false
  audit_used: true | false
awg_findings_before:
awg_findings_after:
architecture_risks:
rule_violations:
human_review_notes:
score:
```

For Mode A and Mode B, `awg_context_used` must be false.

## Scoring Rubric

Each run receives a score out of 100.

### 1. Test Pass: 40 points

- 40: all required tests pass and issue appears resolved
- 25: partial fix, some relevant tests pass, but incomplete
- 10: patch compiles but does not solve the issue
- 0: patch fails to compile or breaks the test harness

### 2. Correct Localization: 15 points

- 15: touched the correct files and symbols
- 10: mostly correct but included minor unnecessary edits
- 5: found part of the right area but missed important context
- 0: edited unrelated or wrong areas

### 3. Patch Minimality: 10 points

- 10: small, focused patch
- 7: moderate patch with acceptable supporting changes
- 3: large or noisy patch
- 0: shotgun rewrite, broad unrelated changes, or speculative refactor

### 4. Architecture and Rule Safety: 20 points

- 20: respects architecture, ownership, invariants, and existing project intent
- 15: minor concern but no clear violation
- 8: risky change or weak architectural justification
- 0: violates known invariants, bypasses authority, or creates hidden drift

### 5. Test/Evidence Quality: 10 points

- 10: adds or updates meaningful tests when appropriate
- 7: runs existing tests and explains coverage
- 3: weak test evidence
- 0: no meaningful validation

### 6. Explanation Quality: 5 points

- 5: clear explanation grounded in repo facts
- 3: understandable but incomplete
- 1: vague explanation
- 0: misleading or unsupported explanation

## Sensei Win Conditions

Sensei is considered useful if Mode C shows:

- equal or better test-pass rate than Mode B
- fewer wrong-file edits than Mode B
- fewer architecture/rule violations than Mode B
- better localization than Mode B
- better reviewer confidence than Mode B

Sensei does not need to win every task.

Sensei wins the pilot if, across 10 tasks:

- Mode C has the best average score, and
- Mode C introduces fewer architecture violations, and
- Mode C does not reduce test-pass rate compared to Mode B

## Strong Sensei Win

Sensei has a strong win if:

- Mode C solves at least 2 more tasks than Mode B, or
- Mode C has similar solved-task count but clearly safer patches, or
- Mode C catches a risky baseline fix that passes tests but violates project architecture

The third case is especially important. A patch that passes tests but damages architecture is exactly the class of failure Sensei is designed to detect.

## Failure Conditions

Sensei fails the pilot if:

- Mode C performs worse than Mode B on test pass rate
- Mode C causes the agent to overthink and edit more unrelated files
- Sensei context is stale, noisy, or misleading
- Sensei cannot produce useful context for most selected tasks
- Sensei adds process cost without improving patch quality

A failed pilot is still useful. It identifies which extractors, rules, or briefings are missing.

## Human Review Checklist

For each final patch, the reviewer should answer:

1. Did the patch solve the issue?
2. Did it touch the right files?
3. Did it avoid unrelated refactoring?
4. Did it preserve project architecture?
5. Did it respect known invariants?
6. Did it add or run appropriate tests?
7. Would I merge this patch?
8. Did Sensei provide useful context that changed the agent's decision?

## Recommended Pilot Size

Start with 10 Go tasks.

Suggested distribution:

- 3 small/medium bug fixes
- 3 cross-file behavior fixes
- 2 configuration or state-machine fixes
- 1 API/contract-related fix
- 1 concurrency or lifecycle fix

Do not start with 50 tasks. The first 10 tasks are to debug the evaluation protocol itself.

## Report Format

At the end of the pilot, produce this summary:

```md
# Sensei Multi-SWE-bench Go Pilot Report

## Summary

Tasks evaluated:
Model used:
Date:
Benchmark subset:
Sensei version:
Agent environment:

## Results

| Mode | Avg score | Tests passed | Avg files touched | Rule violations | Human mergeable patches |
|---|---:|---:|---:|---:|---:|
| A Agent alone | | | | | |
| B Agent + normal tools | | | | | |
| C Agent + Sensei | | | | | |

## Main Findings

## Where Sensei Helped

## Where Sensei Hurt or Added Noise

## Missing Sensei Capabilities

## Recommended Changes Before 50-task Run

## Conclusion

Sensei pilot result:

- PASS
- PARTIAL PASS
- FAIL

Reason:
```

## Interpretation

This evaluation should not be treated as a leaderboard stunt.

The real question is:

> Does Sensei make AI agents safer and more effective inside large codebases?

A raw benchmark pass is good.

A safe patch that respects system laws is better.

A tool that teaches the agent where not to cut is the real prize.
