# Sensei Architect Skill

Sensei ships a built-in `sensei-architect` skill. It teaches agents to use
Sensei as an architectural reflex: establish awareness health, preflight the
task, build a grounded architecture view, challenge plans and edits, guide the
user proportionally, prove the implementation, and propose durable lessons.

The skill is enabled by default:

```bash
sensei init
```

`sensei init --mcp` installs the same skill and also configures the MCP bridge so
the skill can call typed Sensei tools directly.

## Installed Locations

`sensei init` installs managed copies at:

| Path | Purpose |
|---|---|
| `.sensei/skills/sensei-architect/` | canonical repository-local package |
| `.agents/skills/sensei-architect/` | Codex / Agent Skills project discovery |
| `.claude/skills/sensei-architect/` | Claude Code project skill discovery |
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

## Agent Behavior

The skill should activate before architecture-sensitive work, including:

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
