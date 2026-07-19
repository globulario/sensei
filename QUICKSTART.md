# Sensei Quickstart — awareness for any project in 15 minutes

Sensei gives AI agents (and humans) institutional memory with teeth: the
architectural rules, known failure modes, and forbidden fixes of YOUR
project, surfaced automatically before code gets edited — and a portable
pack of 134 battle-validated meta-principles to start from on day one.

## 1. Install (one time)

One line — prebuilt, self-contained (no Go, no Docker):

```bash
# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/globulario/sensei/main/install.sh | sh
```
```powershell
# Windows (PowerShell)
irm https://raw.githubusercontent.com/globulario/sensei/main/install.ps1 | iex
```

Or via a package manager: `brew install globulario/tap/sensei` (macOS / Linux) ·
`winget install Globulario.Sensei` (Windows).

Prefer to build from source? Needs Go 1.25+, git, curl, python3:

```bash
git clone https://github.com/globulario/sensei
cd sensei && ./scripts/install.sh && export PATH="$PWD/bin:$PATH"
```

Full platform notes (tarballs, macOS/Windows, the Oxigraph dependency, the
Docker alternative): **[INSTALL.md](INSTALL.md)**.

VS Code users — install the dashboard extension (search "Sensei — Architectural
Memory" in the Extensions view, or):

```bash
code --install-extension globulario.sensei-awareness
```

Recommended local runtime:

```bash
bash ./scripts/install-sensei-user-services.sh --skip-build
```

That gives you a supervised local Sensei stack and is the preferred path for real
day-to-day use. The ad hoc `sensei serve -no-seed` path below remains fine for a
quick demo.

## 2. Initialize your project

```bash
cd /path/to/your/project
sensei init
```

This scaffolds:

| File | Purpose |
|---|---|
| `docs/awareness/invariants.yaml` | your architectural rules (one example included) |
| `docs/awareness/failure_modes.yaml` | incidents and anticipated bug classes |
| `docs/awareness/meta_principles.yaml` | the portable pack: 8 categories, 134 principles |
| `docs/awareness/high_risk_files.yaml` | paths that require a briefing before edits |
| `.sensei/skills/sensei-architect/` | canonical Sensei Architect skill, enabled by default |
| `.agents/skills/sensei-architect/` | Codex / Agent Skills project skill |
| `.claude/skills/sensei-architect/` | Claude Code project skill |
| `.cursor/rules/sensei.mdc` | Cursor rule that points to the canonical skill |
| `.claude/hooks/` | Claude Code hooks that ENFORCE briefing-before-edit |
| `CLAUDE.md` (appended) | tells the agent how to use awareness |

Use `sensei init --skills=false` to opt out of skill installation. Re-running
`sensei init` is idempotent: unchanged managed skills are left alone, newer
bundled copies can update untouched installs, and local edits are preserved with
a notice unless you pass `--skills-force`.

## 3. Start the graph

```bash
sensei serve -no-seed &     # starts the local Oxigraph store + gRPC server (no Docker)
sensei build                # compiles docs/awareness into the graph
```

`-no-seed` matters: without it the server seeds the embedded Globular
reference graph. Your project builds its own.

## 4. First briefing

```bash
sensei briefing -file src/your/critical_file.py -task "what you're about to do"
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

Re-run `sensei build`, then the briefing — your rule now confronts every
agent that opens that file. Linking `related_invariants` to `meta.*`
connects your specific rule to the generative principle behind it.

## 6. Wire your AI agent (Claude Code)

`sensei init` created `.claude/hooks/` and a CLAUDE.md section. Two things to wire.

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
`sensei edit-check` and blocks it if it introduces a forbidden-fix or
high-severity shape (set `AWG_EDIT_CHECK_ADVISORY=1` to make that one
warn-only). The `PostToolUse` hook records the briefing when Claude calls it.
Nothing else is blocked — low-risk files with rule-clean edits edit normally.

The `Stop` hook is **advisory and never blocks**: when a session ends it runs
`sensei feedback-check` and, if the work touched risky code or added an
incident/regression test but wrote no awareness feedback, it prints a reminder
to record the scar with `sensei propose`. A session that already added graph
feedback (or changed nothing risky) stays silent.

**b) In-conversation tools (optional).** Point Claude's MCP config at the
bridge for `awareness_briefing` / `impact` / `resolve` / `query` /
`metadata` / `preflight` as callable tools:

```json
{ "mcpServers": { "sensei": {
    "command": "/path/to/sensei/bin/awareness-mcp",
    "args": ["--awareness-addr", "localhost:10120"] } } }
```

## 7. Gate your CI

```yaml
- run: sensei validate -repo-root .          # YAML structure, dangling refs
- run: sensei audit -check -services-repo .  # freshness, coverage, stale refs
```

## 8. Evaluate whether the repo is ready for controlled agents

```bash
sensei repo-eval --repo .
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
sensei repo-eval draft-upgrade --repo .
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
incident → write the failure_mode + invariant → sensei build
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
