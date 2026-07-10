# Sensei Quickstart — awareness for any project in 15 minutes

Sensei gives AI agents (and humans) institutional memory with teeth: the
architectural rules, known failure modes, and forbidden fixes of YOUR
project, surfaced automatically before code gets edited — and a portable
pack of 133 battle-validated meta-principles to start from on day one.

## 1. Install (one time)

Source build:

```bash
git clone https://github.com/globulario/sensei
cd awareness-graph && ./scripts/install.sh      # builds awg + server, fetches oxigraph → bin/
export PATH="$PWD/bin:$PATH"
```

Prebuilt Linux `amd64` alternative:

```bash
tar -xzf awg-local_<version>_linux_amd64.tgz
cd extracted-dir
export PATH="$PWD/bin:$PATH"
bash ./scripts/fetch-oxigraph.sh
```

Source build requires Go 1.23+, git, curl, python3. The prebuilt Linux bundle
does not require Go. Full platform notes (macOS, the Oxigraph dependency, the
Docker alternative): **[INSTALL.md](INSTALL.md)**.

Recommended local runtime:

```bash
bash ./scripts/install-awg-user-services.sh --skip-build
```

That gives you a supervised local Sensei stack and is the preferred path for real
day-to-day use. The ad hoc `awg serve -no-seed` path below remains fine for a
quick demo.

## 2. Initialize your project

```bash
cd /path/to/your/project
awg init
```

This scaffolds:

| File | Purpose |
|---|---|
| `docs/awareness/invariants.yaml` | your architectural rules (one example included) |
| `docs/awareness/failure_modes.yaml` | incidents and anticipated bug classes |
| `docs/awareness/meta_principles.yaml` | the portable pack: 8 categories, 133 principles |
| `docs/awareness/high_risk_files.yaml` | paths that require a briefing before edits |
| `.claude/hooks/` | Claude Code hooks that ENFORCE briefing-before-edit |
| `CLAUDE.md` (appended) | tells the agent how to use awareness |

## 3. Start the graph

```bash
awg serve -no-seed &     # starts the local Oxigraph store + gRPC server (no Docker)
awg build                # compiles docs/awareness into the graph
```

`-no-seed` matters: without it the server seeds the embedded Globular
reference graph. Your project builds its own.

## 4. First briefing

```bash
awg briefing -file src/your/critical_file.py -task "what you're about to do"
```

Empty is honest — it means no rules are anchored there YET. Write your
first invariant (step 5) and run it again.

## 5. Your first invariant

Take your most painful past bug — if it broke once and the fix was
non-obvious, it belongs here. Append to `docs/awareness/invariants.yaml`:

```yaml
  - id: payments.paid_state_requires_processor_confirmation
    title: An order records as paid only after processor confirmation — never from local cache writes
    severity: critical
    status: active
    protects:
      files:
        - src/payment_processor.py
    related_invariants:
      - meta.storage_is_not_semantic_authority          # link to the pack
      - meta.code.local_state_must_not_become_hidden_authority
```

Re-run `awg build`, then the briefing — your rule now confronts every
agent that opens that file. Linking `related_invariants` to `meta.*`
connects your specific rule to the generative principle behind it.

## 6. Wire your AI agent (Claude Code)

`awg init` created `.claude/hooks/` and a CLAUDE.md section. Two things to wire.

**a) Enforcement hooks.** In plain terms: *Claude is blocked from editing a
high-risk file until it has requested a briefing for it, and blocked from
writing an edit whose content trips a forbidden-fix rule.* Add this to your
project's `.claude/settings.json` (create the file if it doesn't exist):

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Edit|Write|MultiEdit",
        "hooks": [
          { "type": "command", "command": ".claude/hooks/enforce-briefing.sh", "timeout": 10 },
          { "type": "command", "command": ".claude/hooks/edit-check-guard.sh", "timeout": 10 }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "awareness_briefing",
        "hooks": [
          { "type": "command", "command": ".claude/hooks/record-briefing.sh", "timeout": 10 }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          { "type": "command", "command": ".claude/hooks/feedback-reminder.sh", "timeout": 15 }
        ]
      }
    ]
  }
}
```

The `PreToolUse` hooks run in order: `enforce-briefing.sh` blocks edits to
paths listed in `docs/awareness/high_risk_files.yaml` until a briefing is
recorded, then `edit-check-guard.sh` runs the proposed content through
`awg edit-check` and blocks it if it introduces a forbidden-fix or
high-severity shape (set `AWG_EDIT_CHECK_ADVISORY=1` to make that one
warn-only). The `PostToolUse` hook records the briefing when Claude calls it.
Nothing else is blocked — low-risk files with rule-clean edits edit normally.

The `Stop` hook is **advisory and never blocks**: when a session ends it runs
`awg feedback-check` and, if the work touched risky code or added an
incident/regression test but wrote no awareness feedback, it prints a reminder
to record the scar with `awg propose`. A session that already added graph
feedback (or changed nothing risky) stays silent.

**b) In-conversation tools (optional).** Point Claude's MCP config at the
bridge for `awareness_briefing` / `impact` / `resolve` / `query` /
`metadata` / `preflight` as callable tools:

```json
{ "mcpServers": { "awg": {
    "command": "/path/to/awareness-graph/bin/awareness-mcp",
    "args": ["--awareness-addr", "localhost:10120"] } } }
```

## 7. Gate your CI

```yaml
- run: awg validate -repo-root .          # YAML structure, dangling refs
- run: awg audit -check -services-repo .  # freshness, coverage, stale refs
```

## 8. Evaluate whether the repo is ready for controlled agents

```bash
awg repo-eval --repo .
```

This gives a plain-language posture report for the repository, including:

- overall architecture posture
- `agent_readiness` verdict
- integrity findings that would make governed edits unsafe
- an `upgrade_path` showing the next invariant and contract anchors Sensei would
  want before trusting broader agent work

If the repo comes back as `guarded_repair_only`, that is not a failure. It
means Sensei sees enough structure to support governed repairs, but not enough
stable authority yet for broader autonomous change.

## 9. Draft the next governance layer without promoting it

```bash
awg repo-eval draft-upgrade --repo .
```

This writes review-only candidate files under:

```text
docs/awareness/candidates/repo_eval_upgrade/
```

These drafts are intentionally non-authoritative:

- `status: candidate`
- `confidence: structural`
- `do_not_auto_promote: true`

Use them as a starting point for human review. They help turn a repository
from "repairable under guard" into one with explicit local contracts, without
silently upgrading machine guesses into live authority.

## The loop that makes it compound

```
incident → write the failure_mode + invariant → awg build
        → next agent gets briefed → gate test pins it → drift caught
```

The pack starts you with eight categories of principles validated on a
production distributed platform: authority, signal, lifecycle, dependency
(backend truth) — perception ("is the screen telling the truth?"),
composition ("does the layout make truth perceivable?"), structure
("is the code shaped to last?"), and evolution ("how may the project change
safely over time?"). Your project's own knowledge grows on
that trellis, one incident at a time.

A good system is not just running. It is honest about what it knows,
loud about what it doesn't, and impossible to quietly make worse.
