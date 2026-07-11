# Sensei Quickstart (detailed walkthrough)

> **Just want the fast path?** → **[../QUICKSTART.md](../QUICKSTART.md)** takes
> you from zero to your first enforced briefing in 15 minutes. This page is the
> longer, step-by-step version with more explanation at each stage.

Sensei makes architectural intent queryable at the point of edit. This guide takes you from zero to a working awareness graph that catches dangerous edits before they ship.

## What you'll build

By the end of this guide you'll have:
- A set of YAML files encoding your project's architectural rules
- A running awareness graph that answers "what do I need to know before editing this file?"
- Claude Code hooks that enforce consultation before edits to critical code

## Prerequisites

- **Go1.25+, git, curl, python3** (to build Sensei)
- **Claude Code** (optional — for hook enforcement)

Oxigraph (the RDF store) is fetched for you by the installer — see
**[../INSTALL.md](../INSTALL.md)** for platform notes and the Docker alternative.

## Step 1: Install Sensei

```bash
git clone https://github.com/globulario/sensei.git
cd sensei
./scripts/install.sh                 # builds sensei + server, fetches oxigraph → bin/
export PATH="$PWD/bin:$PATH"
```

This produces `bin/sensei`, `bin/awareness-graph`, and `bin/oxigraph`. The
standalone path (`sensei serve -no-seed`) launches Oxigraph as a child process —
no Docker, no separate store to administer. Full options: [../INSTALL.md](../INSTALL.md).

## Step 2: Scaffold your project

```bash
cd /path/to/your-project
sensei init
```

This creates:
```
docs/awareness/
  invariants.yaml           # Your architectural rules
  failure_modes.yaml        # Known/potential incidents
  incident_patterns.yaml    # Edit shapes that introduce bugs
  high_risk_files.yaml      # Files requiring briefing
  activation_rules.yaml     # When briefing is required
  meta_principles.yaml      # 133 portable principles, 8 categories (seed)
.sensei/config.yaml            # Sensei configuration
.claude/hooks/              # Claude Code enforcement hooks
CLAUDE.md                   # Updated with Sensei section
```

## Step 3: Define your high-risk files

Edit `docs/awareness/high_risk_files.yaml` — add the directories where bugs are most expensive:

```yaml
files:
  - src/auth/
  - src/database/migrations/
  - src/config/
  - src/api/middleware/
  - infrastructure/terraform/
```

These are the paths where the briefing hook will enforce consultation.

## Step 4: Write your first invariant

Edit `docs/awareness/invariants.yaml`. Replace the example with a real rule from your project. Think about:
- What broke recently that shouldn't have?
- What rule lives in someone's head but not in the code?
- What do new developers always get wrong?

Here's a real example:

```yaml
invariants:
  - id: auth.session_token_must_be_httponly
    title: Session tokens must use HttpOnly cookies — never accessible to JavaScript
    severity: critical
    status: active
    protects:
      files:
        - src/auth/session.go
        - src/auth/middleware.go
      symbols:
        - SetSessionCookie
        - CreateSession
    forbidden_fixes:
      - expose_token_in_response_body_for_spa
      - add_javascript_readable_token_cookie
    required_tests:
      - TestSessionCookieIsHttpOnly
    related_failure_modes:
      - auth.xss_steals_session_token
```

## Step 5: Write the matching failure mode

Edit `docs/awareness/failure_modes.yaml`:

```yaml
failure_modes:
  - id: auth.xss_steals_session_token
    title: XSS attack steals session token from non-HttpOnly cookie
    severity: critical
    status: active
    symptoms:
      - "Session hijacking reported by users"
      - "Auth tokens appearing in browser console logs"
    root_cause: |
      A developer changed the session cookie to be JavaScript-readable
      so the SPA could include it in API headers. This exposed the token
      to any XSS vector on the page.
    trigger: |
      Any XSS vulnerability (stored, reflected, or DOM-based) on any
      page that shares the cookie domain.
    architecture_fix: |
      1. Session cookies MUST have HttpOnly flag.
      2. For SPA auth, use a separate CSRF token (not the session token).
      3. API auth uses the cookie directly via same-origin requests.
    forbidden_fixes:
      - add_csp_headers_instead_of_httponly
      - sanitize_all_xss_vectors_instead
    related_invariants:
      - auth.session_token_must_be_httponly
      - meta.fallback_must_degrade_semantics
```

## Step 6: Build and load the graph

```bash
# Validate your YAML
sensei check

# Compile and load into Oxigraph
sensei build
```

You should see:
```
  docs/awareness: 6 files, 42 triples (3 not imported)
  total: 42 triples, validated
  transaction file: .sensei/graph-authority.transaction.tsv
  loaded 7200 bytes into http://localhost:7878/store?default
  marker file: .sensei/graph-authority.json
Build complete.
```

That output means Sensei published the local runtime authority pair for this
graph:

- `.sensei/graph-authority.json`
- `.sensei/graph-authority.transaction.tsv`

## Step 7: Start the server

```bash
sensei serve -no-seed
```

The server starts on `localhost:10120` (gRPC). **`-no-seed` matters for your
own project:** without it, an empty store is seeded with Sensei's embedded
*self reference graph* (the awareness graph of Sensei itself) — useful as an
example, but it would mix Sensei's own invariants into your briefings. With
`-no-seed`, your graph contains exactly
what `sensei build` compiled from your `docs/awareness/`, and the runtime marker +
transaction pair tell Sensei that this local graph is the authoritative one to
judge against.

If you later reload a built `.nt` into a long-lived Oxigraph store, verify the
store actually picked up the new graph:

```bash
sensei seed-status --seed ./graph.nt --oxigraph-url http://localhost:7878/query --require-current
```

That catches the common split-brain cases: new seed file with old live graph,
stale transaction stamp, or any other non-current authority alignment.

## Step 8: Query it

In a new terminal:

```bash
# What do I need to know before editing session.go?
sensei briefing --file src/auth/session.go

# What knowledge nodes touch this file?
sensei impact --file src/auth/session.go

# How risky is this edit?
sensei preflight --file src/auth/session.go --task "add token refresh"
```

The briefing will return:
```
Status: BRIEFING_STATUS_OK

Direct invariants:
- [critical] auth.session_token_must_be_httponly
  "Session tokens must use HttpOnly cookies — never accessible to JavaScript"

Forbidden fixes:
  - expose_token_in_response_body_for_spa
  - add_javascript_readable_token_cookie

Required tests:
  - TestSessionCookieIsHttpOnly
```

## Step 8.5: Evaluate repair-readiness before asking agents to do more

Once the local graph is healthy, you can ask Sensei whether the repository is in a
state where controlled agent work is likely to be safe:

```bash
sensei repo-eval --repo .
```

This report stays product-language-first. It tells you:

- the repository's current posture
- whether Sensei sees it as `ready_for_controlled_agents`,
  `guarded_repair_only`, or still structurally weak
- integrity findings that would make change unsafe
- an `upgrade_path` showing the next contract and invariant anchors Sensei wants

If you only have enough evidence for `guarded_repair_only`, that is the correct
answer. Sensei should not imply broad agent confidence just because the bootstrap
looks clean.

If you want help preparing the next review pass, generate non-authoritative
candidate governance files:

```bash
sensei repo-eval draft-upgrade --repo .
```

That writes review-only drafts under
`docs/awareness/candidates/repo_eval_upgrade/`. They are deliberately marked as
candidate-only and must be reviewed before anything becomes live authority.

## Step 9: Enable Claude Code hooks (optional)

If you use Claude Code, `sensei init` drops hook scripts into `.claude/hooks/`.
Wire them in `.claude/settings.json`. The recommended pair **pushes** the file's
briefing to the agent before it edits, and **blocks** a write that violates a
rule — so the agent gets the invariants delivered (not demanded) *and* can't ship
a forbidden fix:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Edit|Write|MultiEdit",
        "hooks": [
          {
            "type": "command",
            "command": ".claude/hooks/push-briefing.sh",
            "timeout": 10
          },
          {
            "type": "command",
            "command": ".claude/hooks/edit-check-guard.sh",
            "timeout": 10
          }
        ]
      }
    ]
  }
}
```

`push-briefing.sh` (`sensei edit-brief`) hands the agent the file's invariants,
forbidden fixes, and failure modes as context on every edit — the agent can't
forget to consult Sensei because the harness delivers it, and it never blocks.
`edit-check-guard.sh` (`sensei edit-guard`) blocks a write that would introduce a
forbidden-fix shape or trip a high-severity rule.

> Prefer to *require* an explicit briefing call instead of pushing it? Swap
> `push-briefing.sh` for `enforce-briefing.sh` and add a `PostToolUse` hook on
> `awareness_briefing` running `record-briefing.sh` — that blocks edits to
> high-risk files until the agent has called `awareness_briefing` itself.

Now Claude Code will be blocked from editing files in your high-risk directories until it calls `sensei briefing` first.

---

## The incident-to-invariant workflow

Sensei becomes more valuable with every bug you encode. Here's the workflow:

### When something breaks:

1. **Fix the bug** in code.

2. **Add the invariant** to `docs/awareness/invariants.yaml`:
   - What rule was violated?
   - Which files does it protect?
   - What "fixes" would make it worse?
   - What test prevents recurrence?

3. **Add the failure mode** to `docs/awareness/failure_modes.yaml`:
   - What were the symptoms?
   - What was the root cause?
   - What's the correct architectural fix?

4. **Add the incident pattern** to `docs/awareness/incident_patterns.yaml`:
   - What kind of edit introduced the bug?
   - What should the developer have known?

5. **Rebuild**: `sensei build`

Now the same class of bug can never ship again — the briefing warns before the edit is made.

### Classify with meta-principles

When documenting an incident, check if it matches one of the 133 meta-principles in `docs/awareness/meta_principles.yaml` (8 categories: authority, signal, lifecycle, dependency, perception, composition, structure, evolution). These predict where similar bugs are hiding — and evolution principles govern whether the fix itself ships safely:

| If the bug was... | Check this principle |
|---|---|
| A fallback hiding a failure | `meta.fallback_must_degrade_semantics` |
| Two writers racing | `meta.competing_writers_must_converge_or_be_fenced` |
| A write with no cleanup path | `meta.write_creates_completion_obligation` |
| An intermediate state looking done | `meta.half_done_must_not_look_done` |
| A silent no-op for an unhandled case | `meta.silence_is_not_valid_for_unexpected` |
| A retry loop amplifying failure | `meta.failure_response_must_contract_not_amplify` |

Add `related_invariants: [meta.<principle>]` to your failure mode. Then search your codebase for the same pattern — the principle tells you where the next bug is.

---

## Tips

**Start small.** 3-5 invariants from your most painful recent bugs is more valuable than 50 generic rules nobody reads.

**Be specific.** "Don't use globals" is useless. "Config must come from the config store, not os.Getenv, because env vars are invisible and stale" is actionable.

**Encode the why.** The invariant title says what. The failure mode says why. The incident pattern says how it happens. All three together prevent recurrence.

**Forbidden fixes matter most.** The most valuable part of an invariant is often the `forbidden_fixes` list — patterns that look correct but are known-broken. These catch the "obvious" fixes that introduce regressions.

**Test what you protect.** Every invariant should have `required_tests`. If you can't name the test, write one.

---

## Command reference

| Command | What it does |
|---------|-------------|
| `sensei init` | Scaffold a new project |
| `sensei check` | Validate YAML sources |
| `sensei build` | Compile YAML → load into store |
| `sensei build --output file.nt` | Compile to file (no store needed) |
| `sensei serve` | Start the gRPC server |
| `sensei briefing --file <path>` | Context for editing a file |
| `sensei briefing --task "desc"` | Context for a task |
| `sensei impact --file <path>` | Structured knowledge nodes |
| `sensei preflight --file <path>` | Risk classification |
| `sensei version` | Print version |

## Architecture

```
Your YAML files          sensei build           Oxigraph         sensei serve
(docs/awareness/)   -->  (yaml2nt)    -->   (RDF store)  -->  (gRPC)
                                                                 |
                         sensei briefing  <----  gRPC client  <-----+
                         sensei impact
                         sensei preflight
```

Sensei compiles your YAML into RDF triples, stores them in Oxigraph, and serves them via gRPC. The CLI commands are thin gRPC clients that format the responses for humans. Claude Code hooks call the same API to enforce consultation before edits.

## Next steps

- Read the [meta-principles reference](meta-principles.md) for the full classification system
- Set up CI to run `sensei check --strict` on every PR
