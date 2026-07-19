# Sensei Managed Skills

Sensei ships five managed skills. They teach agents how to use Sensei; they are
not repository architectural authority.

| Skill | Purpose | Example routing |
|---|---|---|
| `sensei-architect` | Broad architectural conscience and router. | Design, audit, incident, review, recovery, security, sparse coverage. |
| `sensei-import` | Onboard or refresh a repository into a domain-scoped candidate slice. | "Import Gin into Sensei." |
| `sensei-admission` | Decide whether one exact architecture-sensitive mutation may be attempted and verify the diff stayed inside the envelope. | "May I change this file?" |
| `sensei-closure` | Close bounded architectural knowledge gaps when admission or review is waiting. | "Why is this blocked?" |
| `sensei-benchmark` | Run explicit blind historical external proof with sealed oracle receipts. | "Run a blind Gin pilot." |

Routing precedence:

1. `sensei-benchmark` for blind external evaluation.
2. `sensei-import` for onboarding or refresh.
3. `sensei-admission` for exact proposed mutation or scope verification.
4. `sensei-closure` when bounded architecture is incomplete.
5. `sensei-architect` for general architecture work and fallback.

The skill is enabled by default:

```bash
sensei init
```

`sensei init --mcp` installs the same skill and also configures the MCP bridge so
the skill can call typed Sensei tools directly.

## Installed Locations

`sensei init` installs managed copies for every skill at:

| Path | Purpose |
|---|---|
| `.sensei/skills/<skill>/` | canonical repository-local package |
| `.agents/skills/<skill>/` | Codex / Agent Skills project discovery |
| `.claude/skills/<skill>/` | Claude Code project skill discovery |
| `.cursor/rules/sensei.mdc` | Cursor rule that points to the canonical package |

Cursor uses its rule surface here; Sensei does not claim native Cursor
`SKILL.md` discovery.

Opt out:

```bash
sensei init --skills=false
```

Force-refresh managed skill copies:

```bash
sensei init --skills-force
```

## Update Safety

Each managed copy contains `.sensei-managed.json` with the bundled version and
content digests.

- First init installs the bundled skill.
- Re-running the same Sensei version does not rewrite unchanged files.
- A newer Sensei version may update an untouched managed copy.
- A locally modified managed copy is preserved and reported.
- A manifest-less or unreadable skill copy is preserved and reported.
- `--skills-force` replaces the managed copy intentionally.

Unrelated agent skills, existing instructions, and unrelated MCP servers are not
managed by this mechanism.

Inspect a managed manifest directly:

```bash
cat .sensei/skills/sensei-admission/.sensei-managed.json
```

## Agent Behavior

`sensei-architect` should activate before architecture-sensitive work,
including:

- contracts, interfaces, and ownership
- state layers and convergence
- lifecycle, recovery, rollback, cleanup, and idempotency
- security, data-loss, destructive changes, and migrations
- pattern use or suspected pattern misuse
- architecture audits, incidents, debugging, and PR review
- sparse, stale, degraded, or contradictory Sensei coverage

Agents should use MCP tools when available:

- `awareness_metadata`
- `awareness_preflight`
- `awareness_briefing`
- `awareness_impact`
- `awareness_resolve`
- `awareness_query`
- `awareness_edit_check`
- `awareness_propose`

When MCP is unavailable, the skill uses equivalent `sensei` CLI commands.

The specialized skills keep normal work simple:

- Ordinary exact mutation should load `sensei-admission`, not the benchmark or
  closure playbooks.
- Blocked admission routes to `sensei-closure` only until explicit inputs change.
- Import differs from benchmark: import learns a repository slice; benchmark
  tests Sensei against a sealed historical oracle.

## What It Does Not Do

The skill is not architectural authority. It does not replace:

- source inspection
- tests and builds
- runtime observation
- review
- user decisions
- Sensei's reviewed active corpus

It must never treat `EMPTY` as safe, never use candidate knowledge as active
authority, and never promote agent hypotheses without review.
