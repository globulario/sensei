# Sensei CLI Reference

Complete reference for the `sensei` command. For the gRPC/MCP wire surface see
[api-reference.md](./api-reference.md); for the agent workflow see
[agent-usage.md](./agent-usage.md).

```
sensei <command> [flags]
sensei <command> --help     # per-command flag help
sensei help                 # command index
sensei version              # print version and exit
```

**Conventions used below**

- Flags accept either a single or double dash (`-file` and `--file` are
  equivalent — Go's flag parser). Examples use `--`.
- "**Server**" = needs a running `sensei serve` (gRPC) — default address
  `localhost:10120`, overridable with `--addr`.
- "**Oxigraph**" = needs the Oxigraph HTTP store (default
  `http://localhost:7878`) but not the gRPC server.
- "**Local**" = pure local file/git work; no daemon.
- Query commands take `--json` for machine-readable output.

### Command map

| Group | Commands |
|---|---|
| [Setup & build](#setup--build) | `init` · `import` · `bootstrap` · `build` · `rebuild` · `serve` |
| [Query (agent-facing)](#query-agent-facing) | `briefing` · `impact` · `preflight` · `resolve` · `query` · `metadata` · `edit-check` |
| [Authoring & feedback](#authoring--feedback) | `propose` · `feedback-check` · `promote` · `ingest` · `skill-ingest` |
| [Validation & audit](#validation--audit) | `check` · `validate` · `validate-draft` · `audit` · `repo-eval` (+ `fix`, `draft-upgrade`) · `architecture-extract` · `extract-invariants` |
| [Gating](#gating) | `gate` · `contract-assess` · `contract-bootstrap` |
| [Pattern & structural checks](#pattern--structural-checks) | `pattern-check` · `source-check` · `visual-audit` |
| [Cold bootstrap & mining](#cold-bootstrap--mining) | `cold-bootstrap` · `intent-mine` · `corpus` |
| [Benchmark / evaluation](#benchmark--evaluation) | `benchmark-brief` · `benchmark-judge` · `benchmark-score` · `benchmark-retry` · `benchmark-event-meta` · `seed-status` |

---

## Setup & build

### `sensei init` — Local

Scaffold awareness for a new project **and wire up your agent tools**.

| Flag | Default | Purpose |
|---|---|---|
| `--dir` | `.` | project root |
| `--hooks` | `true` | generate Claude Code hook scripts under `.claude/hooks/` |
| `--claude-md` | `true` | append the Sensei section to `CLAUDE.md` (idempotent) |
| `--agents-md` | `true` | append the Sensei section to `AGENTS.md` (Codex/Cursor/others; idempotent) |
| `--cursor` | `true` | write a Cursor rule at `.cursor/rules/sensei.mdc` (skipped if it exists) |
| `--skills` | `true` | install bundled project skills under `.sensei/skills`, `.agents/skills`, and `.claude/skills` |
| `--skills-force` | `false` | replace locally modified Sensei-managed skill copies |
| `--mcp` | `false` | write/merge the `sensei` MCP server into `.mcp.json` (never clobbers other servers) |

Creates `docs/awareness/` with templates (`invariants.yaml`,
`failure_modes.yaml`, `incident_patterns.yaml`, `high_risk_files.yaml`,
`activation_rules.yaml`, and the `meta_principles.yaml` pack), plus
`.sensei/config.yaml`. Then installs the bundled `sensei-architect` skill and
wires the agent surfaces above — **additively and idempotently**: existing rules
are preserved, re-running never duplicates, and an existing `sensei` MCP entry
is left untouched.

Skill installation writes the canonical managed copy to
`.sensei/skills/sensei-architect/`, plus native project skill copies to
`.agents/skills/sensei-architect/` for Codex / Agent Skills and
`.claude/skills/sensei-architect/` for Claude Code. Cursor uses
`.cursor/rules/sensei.mdc` to point at the canonical skill package. Each managed
copy has `.sensei-managed.json` with the bundled version and content digests.
Untouched managed copies can update on a later Sensei version. Locally modified
or manifest-less copies are preserved and reported; use `--skills-force` only
when you intentionally want to replace them.

### `sensei import` — Local (+ optional Oxigraph)

Onboard or refresh a repository through the job-oriented facade. Fresh import
accepts a git URL or path and runs the pipeline in order: clone or reuse checkout
→ contract extraction → structural extraction → optional history mining →
optional domain-scoped load.

`--refresh` is for an existing checkout. It never clones; it re-extracts the
checkout and optionally reloads the same domain-scoped slice.

| Flag | Default | Purpose |
|---|---|---|
| `--domain` | derived | repo domain, e.g. `github.com/gin-gonic/gin`; for paths, falls back to the git remote |
| `--refresh` | `false` | re-extract an existing checkout; never clone |
| `--depth` | `full` | `basic` for structure only; `full` also attempts contracts/history |
| `--dir` | temp dir | checkout destination for URL imports; ignored for existing paths and refresh |
| `--store-url` | — | load the domain-scoped slice into a store; empty prints the build command |
| `--graph-marker-file` | — | runtime marker file to refresh when loading a served store |
| `--drafter` | `llm` | contract drafter for full depth: `llm` or `echo` |
| `--repo-slug` | derived | owner/name for PR-review history mining |
| `--dry-run` | `false` | print the plan; run nothing |

### `sensei bootstrap` — Local

Initialize Sensei for an *existing* repo: scaffold if missing, run deterministic
architecture extraction (proto/REST contracts, web components, components,
per-language import graph, code symbols, tests), optionally mine history
candidates, then validate and build.

| Flag | Default | Purpose |
|---|---|---|
| `--path` | `.` | repository checkout to bootstrap |
| `--repo` | `.` | deprecated alias for `--path` |
| `--skip-history` | `false` | skip history mining via coldsource |
| `--skip-build` | `false` | extract + validate but don't build the graph |
| `--check` | `false` | compare generated output to committed files; non-zero if stale (CI) |
| `--dry-run` | `false` | print the report; write nothing |

Extractors write to `docs/awareness/generated/` (`contracts.yaml`,
`rest_contracts.yaml`, `web_components.yaml`, `contract_consumption.yaml`,
`components.yaml`, `<lang>_import_graph.yaml`, `source_symbols.yaml` /
`source_edges.yaml` when a `namespaces.yaml` registry exists, `tests.yaml`).

### `sensei build` — Oxigraph (or `--output`)

Compile awareness YAML → N-Triples and load into Oxigraph.

| Flag | Default | Purpose |
|---|---|---|
| `--input` (repeatable) | `docs/awareness` | YAML source directory |
| `--output` | — | write N-Triples to file instead of loading (no Oxigraph needed) |
| `--store-url` | `http://localhost:7878/store?default` | Oxigraph Graph Store endpoint |
| `--strict` | `false` | fail on *unknown* YAML schemas (typos); recognized config files stay non-fatal |
| `--validate-refs` | `false` | fail on dangling references |
| `--graph-marker-file` | auto | write the verified runtime graph marker after a successful store load |
| `--graph-transaction-file` | auto-with-repo | write the matching runtime transaction certification after a successful store load |
| `--ag-repo` / `--services-repo` | auto | repo roots used to build the runtime transaction certification |
| `--repo` | — | default domain/repo for untagged nodes (e.g. `github.com/cli/cli`) |
| `--domain` | — | default domain kind for untagged nodes: `repo` or `shared` (inferred `repo` when `--repo` set) |
| `--source-set` | — | default source-set namespace for untagged nodes |

Validates N-Triples before upload; appends the deterministic seed marker. Prints
per-directory file/triple counts.

When loading into a live store, `sensei build` can now publish a local runtime
authority pair:

- graph marker: `.sensei/graph-authority.json`
- matching transaction certification: `.sensei/graph-authority.transaction.tsv`

That pair lets `sensei serve --no-seed` treat the loaded graph as
`current + transaction=certified` on its own runtime terms, instead of trying
to reuse the embedded Globular transaction stamp.

### `sensei rebuild` — Oxigraph (optional)

Rebuild the self-only public `awareness.nt` from this repo's YAML sources and
(optionally) reload Oxigraph. Pass `--combined` to include the paired services
repo for an internal combined seed. Steps: scan YAML → N-Triples → validate →
update `embeddata/` → PUT to Oxigraph.

| Flag | Default | Purpose |
|---|---|---|
| `--services-repo` | auto | path to the services repo |
| `--ag-repo` | auto | path to the awareness-graph repo |
| `--oxigraph-url` | `http://localhost:7878/store?default` | Graph Store endpoint |
| `--check` | `false` | compare only; exit 1 if stale (CI) |
| `--no-runtime-reload` | `false` | skip the Oxigraph PUT |
| `--combined` | `false` | include paired services awareness in the seed |
| `--strict` | `false` | fail if Oxigraph is unavailable |

> `propose`, `promote`, and `ingest` call this internally — you rarely run it by
> hand outside CI staleness checks.

### `sensei serve` — starts the server

Start Oxigraph (as a managed child process) **and** the gRPC server as one
unit. No Docker.

| Flag | Default | Purpose |
|---|---|---|
| `--addr` | `:10120` | gRPC listen address |
| `--oxigraph-bind` | `127.0.0.1:7878` | Oxigraph listen address |
| `--no-seed` | `false` | skip the embedded Globular seed — **use this for your own project** so it builds its own graph |
| `--data` | `~/.local/share/sensei/oxigraph` | Oxigraph data directory |
| `--no-oxigraph` | `false` | don't start Oxigraph; connect to an external instance |
| `--home-domain` | `globular` | domain key for untagged host-project nodes |

Searches for the `oxigraph`/`awareness-graph` binaries next to `sensei`, then in
`./bin/`, then on `PATH`. Reuses an Oxigraph already bound to the port. SIGINT/
SIGTERM shuts both down cleanly.

---

## Query (agent-facing)

All require a running **Server** (`--addr`, default `localhost:10120`) and
accept `--json`.

### `sensei briefing`

Prose context (~500 tokens) for a file or task. **Call this first.**

| Flag | Default | Purpose |
|---|---|---|
| `--file` | — | repo-relative path |
| `--task` | — | task description |
| `--depth` | `standard` | `agent_compact` \| `compact` \| `standard` \| `deep` |
| `--domain` | — | repo/domain scope; **required** on a multi-domain graph |

At least one of `--file` / `--task` is required. Prints status, prose,
`referenced_ids`, generation time.

### `sensei impact`

Structured `KnowledgeNode`s for a file (no prose) — direct + inferred
invariants/failure modes/intents, architecture spine, forbidden fixes, required
tests.

| Flag | Default | Purpose |
|---|---|---|
| `--file` | — | repo-relative path (**required**) |
| `--domain` | — | repo/domain scope; required on a multi-domain graph |

### `sensei preflight`

Risk classification before editing. Returns `risk_class` + `confidence` +
action lists. Branch on this before reading prose.

| Flag | Default | Purpose |
|---|---|---|
| `--task` | — | task description |
| `--file` (repeatable) | — | repo-relative file(s) to touch |
| `--mode` | `standard` | `standard` \| `compact` |
| `--domain` | — | scope, passed to per-file impact queries |

At least one of `--file` / `--task` required. Always exits 0 (a high-risk
verdict is reported in output, not the exit code). See
[api-reference.md#preflight](./api-reference.md#preflight) for the `risk_class`
values.

### `sensei resolve <class> <id>`

Fetch one node by class + bare id. Positional args, **not** flags.

```bash
sensei resolve invariant reconcile.dep_block_records_must_be_cleared_when_dep_satisfies
sensei resolve meta_principle storage_is_not_semantic_authority
```

`--domain` optionally scopes the lookup. Classes: `Invariant`, `FailureMode`,
`IncidentPattern`, `Intent`, `ForbiddenFix`, `Test`, `SourceFile`, `Symbol`,
`CodeSymbol`, `MetaPrinciple`, `Component`, `Boundary`, `Contract`, `Decision`,
`Evidence`, `DesignPattern`, `ImplementationPattern`, `PatternMisuse`
(case-insensitive). Exit 2 on not-found / wrong arg count.

### `sensei query --mode <mode>`

Typed, whitelisted browse — **no raw SPARQL**.

| Flag | Default | Purpose |
|---|---|---|
| `--mode` | — | **required**: `by_file` \| `by_id` \| `by_class` \| `related` |
| `--file` | — | for `by_file` |
| `--id` | — | class-qualified id, for `by_id` / `related` |
| `--class` | — | for `by_class` (see snake_case classes in the API ref) |
| `--limit` | `50` | max rows |

Tab-delimited table (CLASS, ID, LABEL, SEVERITY, STATUS, RELATION, SOURCE) or
JSON.

### `sensei metadata`

Graph-level coverage, freshness, build provenance, and the architectural-spine
counts. No required args. Call once per session to interpret `EMPTY` briefings.
Returns `build_provenance_state` / `coverage_state` / `seed_state` verdicts plus
local candidate-queue and benchmark summaries when detected.

### `sensei edit-check`

Advisory: evaluate proposed edit content against repo-scoped `detect` rules for
a file. **Warning-only — never blocks, never edits.**

| Flag | Default | Purpose |
|---|---|---|
| `--file` | — | repo-relative path (**required**) |
| `--content` | — | inline proposed content |
| `--content-file` | — | read content from a path (`-` = stdin) |
| `--domain` | — | scope; required on a multi-domain graph |

Provide exactly one of `--content` / `--content-file`. Prints `rules_evaluated`,
warning count, and one block per warning (severity · rule id · class · message ·
detail · provenance). Always exits 0.

---

## Authoring & feedback

### `sensei propose` — Oxigraph (optional)

Append **one** typed feedback entry (a "scar") to the right YAML source,
rebuild the seed, reload the local store, and `git add` it. **Never commits —
you review and commit.** This is the supported write path; it is contract-first
(vague notes are rejected).

| Flag | Default | Purpose |
|---|---|---|
| `--kind` | — | `failure_mode` \| `invariant` \| `required_test` \| `forbidden_fix` \| `contract_unknown` |
| `--json` | — | read a full `ProposeRequest` from a JSON file (`-` = stdin) instead of flags |
| `--id` | derived | stable id (required for `required_test`) |
| `--title` | — | short title |
| `--description` | — | what happened / what this documents |
| `--severity` | — | `critical` \| `high` \| `warning` |
| `--contract` | — | the contract violated or clarified |
| `--proposed-contract` | — | (`contract_unknown`) the contract you propose |
| `--revision-request` | — | (`contract_unknown`) request to revise an existing contract |
| `--source-file` (rep.) | — | source file the entry anchors to |
| `--related-invariant` (rep.) | — | related invariant id |
| `--related-failure` (rep.) | — | related failure_mode id |
| `--required-test` (rep.) | — | `file.go:TestName` |
| `--forbidden-fix` (rep.) | — | forbidden fix id or note |
| `--evidence` (rep.) | — | evidence line |
| `--repo` / `--domain` | — | repo / domain scope (e.g. `github.com/globulario/sensei`) |
| `--target-repo` | awareness-graph | repo whose `docs/awareness/` receives the entry |
| `--dry-run` | `false` | validate + render only |
| `--no-rebuild` | `false` | append YAML but skip rebuild/reload |
| `--no-stage` | `false` | don't `git add` the touched files |
| `--oxigraph-url` | `http://localhost:7878/store?default` | reload endpoint |
| `--format` | `text` | `text` \| `json` |

Result status: `created` · `duplicate` · `validation_failed` · `dry_run`. A
`contract_unknown` entry is parked under `docs/awareness/candidates/` outside the
active build until the contract is resolved.

```bash
sensei propose --kind failure_mode --title "Stale seed served after reload" \
  --contract "reload must serve fresh triples" \
  --related-invariant awareness.seed_reload_must_be_fresh \
  --source-file golang/server/reload.go \
  --required-test golang/server/reload_test.go:TestReloadFresh \
  --evidence "observed stale node after PUT"
```

### `sensei feedback-check` — Local

Advisory Stop-hook backing: warns when a session changed risky code or added an
incident/regression test but wrote no graph feedback. **Never blocks.**

| Flag | Default | Purpose |
|---|---|---|
| `--repo-root` | auto | repo root |
| `--changed-file` (rep.) | — | explicit changed file (else derived from git) |
| `--git` | `true` | derive changed files from git status |
| `--strict` | `false` | exit non-zero when a gap is found (default: always exit 0) |
| `--format` | `text` | `text` \| `json` |
| `--quiet` | `false` | print nothing when there is no gap |

### `sensei promote <candidate-id>` — Oxigraph (pilot mode)

Promote a candidate from `docs/awareness/candidates/` into the matching
canonical YAML (home domain) or into a pilot domain-tagged file (foreign repo).

| Flag | Default | Purpose |
|---|---|---|
| `--target` | auto | target canonical YAML (auto from class) |
| `--dry-run` | `false` | validate only |
| `--no-rebuild` | `false` | skip rebuild after promotion |
| `--repo` | — | *pilot mode* foreign repo (e.g. `github.com/caddyserver/caddy`) → routes into `pilot/<repo>/` |
| `--domain` | `repo` when `--repo` set | domain kind (`repo` \| `shared`) |
| `--source-set` | `pilot/<slug>` | source-set namespace (pilot) |
| `--oxigraph-url` | `http://localhost:7878/store?default` | reload endpoint (pilot reload) |

Never commits.

### `sensei ingest` — Local

Feed knowledge from a YAML file, or re-run the annotation scanner.

| Flag | Default | Purpose |
|---|---|---|
| `--from-file` | — | YAML file of awareness entries |
| `--from-scan` | — | re-run annotation scanner over all services + rebuild |
| `--dry-run` | `false` | validate only |
| `--no-rebuild` | `false` | skip automatic rebuild |
| `--services-repo` / `--ag-repo` | auto | repo paths |

Exactly one of `--from-file` / `--from-scan`.

### `sensei skill-ingest <skill-pack-root>` — Local

Generate review-only `ImplementationPattern` candidates from external agent
skill packs. See [skill-ingestion.md](./skill-ingestion.md) for the workflow and
safety model.

| Flag | Default | Purpose |
|---|---|---|
| `--out` | `docs/awareness/candidates/skills` | output directory for generated candidate YAML |
| `--repo` | — | repository domain used for provenance reporting |
| `--source-set` | `external/skills` | source-set label used for provenance reporting |
| `--include-deprecated` | `false` | include `skills/deprecated/**/SKILL.md` |
| `--dry-run` | `false` | parse, render, and validate without writing files |

Input files must be named exactly `SKILL.md` and begin with YAML front matter
containing `name` and `description`. The command writes one YAML file per valid
skill under the candidate directory, with `class: ImplementationPattern` and
`status: candidate`.

It never rebuilds, never promotes, and never writes into active awareness corpus
paths by default. Candidate directories are skipped by normal `sensei build` until
a human reviews and promotes or manually moves the candidate into an active
corpus path.

---

## Validation & audit

### `sensei check` — Local

Validate YAML sources without building (schema recognition + reference
integrity + N-Triples validity).

| Flag | Default | Purpose |
|---|---|---|
| `--input` (rep.) | `docs/awareness` | YAML directory |
| `--strict` | `false` | fail on unrecognized/invalid schemas |

### `sensei validate` — Local

Deeper static check: dangling references, missing source files, duplicate IDs,
UML enums.

| Flag | Default | Purpose |
|---|---|---|
| `--dir` (rep.) | `docs/awareness` + `docs/intent` | directories to scan |
| `--repo-root` | auto | repo root for relative paths |
| `--ag-repo` | auto | awareness-graph repo (shared meta corpus) |
| `--scope` | `local` | `local` \| `full` |
| `--format` | `table` | `table` \| `json` |
| `--fail-on-warn` | `false` | exit non-zero on warnings too |

Exit 1 if errors found.

### `sensei validate-draft` — Local

Validate one externally-drafted candidate against one exported bundle through
the cold-bootstrap import cage. Prints PASS or FAIL+reasons; writes/promotes
nothing.

| Flag | Default | Purpose |
|---|---|---|
| `--bundle` | — | exported bundle JSON (**required**) |
| `--draft` | — | candidate draft, JSON or YAML (**required**) |
| `--repo` | `.` | working tree for citation resolution |

### `sensei audit` — Oxigraph (optional)

Self-audit for drift across 7 checks (embeddata freshness, YAML validity,
N-Triples validity, coverage gaps, stale file refs, test coverage, contract
assessment).

| Flag | Default | Purpose |
|---|---|---|
| `--verbose` | `false` | per-finding detail |
| `--check` | `false` | exit 1 on any FAIL (CI) |
| `--fix` | `false` | auto-repair mechanical issues (update embeddata + reload Oxigraph) |
| `--services-repo` / `--ag-repo` | auto | repo paths |

### `sensei repo-eval` — Local

Evidence-based repository architecture/awareness quality report with visible
subscores, findings, and recommendations. The report is meant to answer a
practical product question: "is this repo structured well enough for governed
AI repair work yet, and if not, what is the next upgrade step?"

Key outputs:

- `posture`: overall repository quality signal
- `agent_readiness`: whether Sensei sees the repo as ready for controlled agents,
  limited to guarded repair, or still too weak
- `integrity_findings`: structural reasons confidence should remain bounded
- `upgrade_path`: top contract and invariant candidates to review next

`guarded_repair_only` is a legitimate result. It means Sensei sees enough
structure for governed repairs under explicit constraints, but not enough
stable authority for broader change.

Example JSON shape:

```json
{
  "posture": "good",
  "agent_readiness": {
    "verdict": "guarded_repair_only"
  },
  "integrity_findings": [],
  "upgrade_path": {
    "recommended_invariants": [],
    "recommended_contracts": []
  }
}
```

| Flag | Default | Purpose |
|---|---|---|
| `--repo` | auto | repository to evaluate |
| `--services-repo` / `--ag-repo` | auto | repo paths |
| `--json` | `false` | JSON report |

#### `sensei repo-eval fix` — Local

Apply **safe, evidence-backed** fixes: auto-populate missing critical/high
invariant `required_tests` when code annotations declare both
`enforces=<invariant>` and `tested_by=<test>`. Dry-run unless `--apply`. Never
commits.

| Flag | Default | Purpose |
|---|---|---|
| `--apply` | `false` | write safe fixes (else dry-run) |
| `--proposal` | `false` | emit review-ready non-mutating proposals where evidence is explicit but not safe |
| `--proposal-snippets` | `false` | include patch-ready YAML snippets |
| `--format` | `text` | `text` \| `json` \| `review` |
| `--repo` / `--services-repo` / `--ag-repo` | auto | repo paths |

#### `sensei repo-eval draft-upgrade` — Local

Generate review-only governance candidates from the current `repo-eval`
`upgrade_path`. This command does **not** promote anything into live authority.
It drafts the next likely invariants and contracts so a human can review and
refine them.

Output location:

```text
docs/awareness/candidates/repo_eval_upgrade/
```

Draft safety properties:

- invariants and contracts are marked `status: candidate`
- contracts carry `confidence: structural`
- drafts carry `do_not_auto_promote: true`
- missing semantic fields are left explicit for human completion
- live `docs/awareness/*.yaml` and `docs/intent/*.yaml` are not modified

This is the intended bridge from `guarded_repair_only` toward stronger local
authority, while keeping anti-drift controls intact.

Example usage:

```bash
sensei repo-eval draft-upgrade --repo . --dry-run
sensei repo-eval draft-upgrade --repo . --json
```

| Flag | Default | Purpose |
|---|---|---|
| `--repo` | auto | repository to evaluate and draft against |
| `--dry-run` | `false` | print planned draft files without writing |
| `--json` | `false` | JSON report of draft actions |

### `sensei architecture-extract` — Local

Build a read-only architectural contract extraction report from evidence already
present in a checkout. It layers the repository into observed, inferred, and
governed contract sets, then emits the required inventory, migration,
authority-split, direction, unknowns, promotion-candidate, and proof-obligation
sections.

This command is intentionally conservative:

- generated artifacts become observed evidence
- active authored `docs/awareness` entries and enforcing workflows become
  governed evidence within their explicit scope
- `docs/awareness/candidates` entries and history hints become inferred,
  review-only candidates
- nothing is promoted, loaded, or treated as new authority

Example:

```bash
sensei architecture-extract --repo /home/dave/Documents/github.com/caddyserver/caddy \
  --domain github.com/caddyserver/caddy --format markdown
```

| Flag | Default | Purpose |
|---|---|---|
| `--repo` | `.` | checkout whose existing evidence should be layered |
| `--domain` | git origin | repository domain key for scope labels |
| `--format` | `markdown` | `markdown` \| `json` \| `yaml` |
| `--out` | stdout | write report to a file |
| `--history-limit` | `40` | recent commits to inspect for migration hints; `0` disables history |

### `sensei extract-invariants` — Local

Deterministically extracts normalized facts and review-only invariant candidates
from repository evidence. This is the invariant-specific pipeline:

```text
source extraction -> normalized facts -> candidate synthesis -> confidence and contradictions
```

Increment 1 supports Go guards, write paths, schema tags, architectural test
names, generated-artifact evidence, CI/scanner evidence, documentation claims,
and optional recent-history facts. Candidates stay under `status: candidate`;
they are not governed invariants and are not imported by normal awareness builds
unless a human promotes them.

Example:

```bash
sensei extract-invariants --repo /home/dave/Documents/github.com/caddyserver/caddy \
  --format json --output /tmp/caddy-invariants.json
```

| Flag | Default | Purpose |
|---|---|---|
| `--repo` | `.` | checkout to inspect |
| `--format` | `json` | `json` \| `yaml` |
| `--output` | stdout | write extraction artifact to a file |
| `--include-history` | `false` | inspect recent git history for removal/forbidden-pattern facts |
| `--include-docs` | `true` | extract normative documentation/comment facts |
| `--include-tests` | `true` | classify architectural test facts |
| `--include-mutation-analysis` | `false` | allocate isolated mutation workspace; bounded mutant execution is a later increment |
| `--minimum-confidence` | `low` | `low` \| `medium` \| `high` \| `proven` candidate filter |
| `--explain` | `false` | retained for CLI symmetry; JSON/YAML always include explanations |
| `--check` | `false` | compare `--output` with a fresh deterministic extraction |

---

## Gating

### `sensei gate` — Server (or `--contracts`, Local)

Two modes.

**Default (EditCheck dry-run):** evaluate a git diff's added/changed lines
against in-scope detect rules and report which findings **would** block. Never
blocks, never edits.

| Flag | Default | Purpose |
|---|---|---|
| `--diff` | `HEAD` | git range (`origin/main...HEAD`, or `HEAD` for working tree) |
| `--repo-root` | `.` | repo to diff |
| `--domain` | — | scope; required on a multi-domain graph |
| `--addr` | `localhost:10120` | gRPC server |
| `--report-only` | `false` | CI fail-open: always exit 0, print a non-blocking summary even if Sensei is down |
| `--json` | `false` | JSON output |

**Frozen-contract mode (`--contracts`):** self-contained gate over a frozen
contract set — no server needed.

| Flag | Default | Purpose |
|---|---|---|
| `--contracts` | — | path to a frozen contract-set YAML file or directory (enables this mode) |
| `--enforce` | `false` | exit non-zero on a contract violation (else report-only) |

Evaluates `regex_forbidden` detect rules and emits a verdict per contract:
`respected` / `violated` / `not_applicable`, with scope warnings, proof status,
and required test paths.

### `sensei contract-assess` — Local

Report-only contract-synthesis assessment from **explicitly supplied** evidence
scores + blockers. Does not query the graph or infer anything.

Evidence-score flags (integers): `--direct-source-annotation` (0–3),
`--existing-tests-proving-behavior` (0–4),
`--repeated-implementation-pattern` (0–2), `--ownership-authority-path` (0–3),
`--failure-mode-or-incident-history` (0–2), `--nearby-human-intent` (0–3),
`--cross-repo-consistency` (0–2), `--absence-of-conflicting-contracts` (0–3).
Booleans: `--explicit-contract`, `--governing-test`. `--blocker` (repeatable):
`conflicting-explicit-contract` \| `conflicting-test` \|
`missing-ownership-authority` \| `product-ambiguity` \| `weak-pattern-only` \|
`generic-evidence-only`. `--json` for JSON.

### `sensei contract-bootstrap` — Server (optional)

Build a *proposed* repair-contract bootstrap from issue text, fail-to-pass
tests, repo surfaces, and optional Sensei cross-references. Mutates nothing.

| Flag | Default | Purpose |
|---|---|---|
| `--repo-root` | `.` | repo to analyze |
| `--task-file` | — | task JSON (`issue`/`domain`/`f2p_tests`) — takes precedence over `--issue` |
| `--issue` | — | issue text |
| `--f2p-test` (rep.) | — | fail-to-pass test name |
| `--domain` | — | scope for Sensei cross-reference |
| `--addr` | `localhost:10120` | gRPC server (used only if reachable) |
| `--format` | `text` | `text` \| `json` \| `prompt` (LLM context) \| `scaffold` (YAML ready for `sensei gate --contracts`) |

---

## Pattern & structural checks

### `sensei pattern-check <file>...` — Server

Text-scan each file against the ImplementationPattern recipes the briefing
returns; report missing required calls / present forbidden calls.

| Flag | Default | Purpose |
|---|---|---|
| `--addr` | `localhost:10120` | gRPC server |
| `--format` | `table` | `table` \| `json` |
| `--fail-on-violation` | `true` | exit non-zero on violation |

### `sensei source-check` — Local

Scan source files for structural violations using regex patterns from a YAML
config (scope: file / class / method).

| Flag | Default | Purpose |
|---|---|---|
| `--patterns` | — | path to `source_patterns.yaml` (**required**) |
| `--source` | — | source directory to scan (**required**) |
| `--extensions` | `.ts,.js,.go` | comma-separated extensions |
| `--strict` | `false` | exit 1 on any violation |

### `sensei visual-audit` — Chrome CDP + web app

Screenshot each route and compare against golden images. Requires Chrome with
`--remote-debugging-port`.

| Flag | Default | Purpose |
|---|---|---|
| `--routes` | — | comma-separated hash routes (**required**) |
| `--base-url` | `http://localhost:5173` | app base URL |
| `--chrome-port` | `9222` | Chrome debugging port |
| `--golden-dir` | `.sensei/golden` | golden image directory |
| `--update` | `false` | save current screenshots as new goldens |
| `--threshold` | `1.0` | pixel-diff % to flag FAIL |
| `--wait` | `3` | seconds to wait after navigation |

---

## Cold bootstrap & mining

### `sensei cold-bootstrap` — Local (+ optional LLM)

Mine awareness candidates from cold day-0 signals (revert/regression commits +
PR review comments), triangulate, enforce the citation contract, bound to top N.
Writes `status:candidate` YAML only — **never promotes, never touches the active
graph.**

| Flag | Default | Purpose |
|---|---|---|
| `--repo` | `.` | target git working tree |
| `--since` | `HEAD~200..HEAD` | git range to scan |
| `--out` | `docs/awareness/candidates` | candidate output dir |
| `--max` | `10` | emit at most N top-ranked candidates |
| `--dry-run` | `false` | print scoring report; write nothing |
| `--drafter` | `echo` | `echo` (deterministic, no LLM) \| `llm` (`ANTHROPIC_API_KEY`) \| `claude-cli` (authed Claude Code CLI / subscription, no key) |
| `--model` | `claude-opus-4-8` | LLM model (with `--drafter llm` or `claude-cli`) |
| `--pr-comments` | — | offline JSON of PR review comments (replaces `gh`) |
| `--repo-slug` | — | `owner/name` for `gh` PR review fetch |
| `--bundles-out` | stdout | export bundles here |
| `--auto-window` | `false` | widen the revert-scan window automatically until enough signals |
| `--auto-window-target` | `10` | stop widening at this many revert/regression commits |

### `sensei intent-mine` — Local (+ optional LLM)

Ground architectural-intent candidates against a repo tree; dry-run report
grouped by output class. Proposer proposes, Sensei grounds, human approves.

| Flag | Default | Purpose |
|---|---|---|
| `--repo` | `.` | working tree for grounding + extraction |
| `--candidates` | — | YAML of proposed candidates (skips extraction) |
| `--from-coldsource` | — | YAML of coldsource candidates to lift as scar-derived intent |
| `--sources` | `docs,comments,schemas,tests` | comma list: `docs`/`comments`/`schemas`/`tests`/`commits`/`prs` |
| `--drafter` | `echo` | `echo` \| `llm` |
| `--pr-comments` | — | JSON of PR review comments (for `--sources prs`) |
| `--model` | `claude-3-5-sonnet-20241022` | LLM model override |
| `--max` | `12` | max candidates to propose |
| `--apply` | `false` | write: grounding ≥0.80 → `docs/awareness/intent_<id>.yaml`; everything else → `candidates/intents.yaml` |

### `sensei corpus <subcommand>` — Local

Human-gated corpus-integration dispatch. None promote, mutate a graph, touch
seed, or use an LLM.

- `sensei corpus plan --from <report.yaml>` — classify findings into
  integrate/hold/never (read-only table).
- `sensei corpus materialize --from <report.yaml> --selected <id1,id2> --out <dir>`
  — write `status:candidate` entries for selected, integrate-eligible findings
  (always under `candidates/`).
- `sensei corpus validate --from <report.yaml>` — validate report structure.

---

## Benchmark / evaluation

These power the Multi-SWE-bench harness (`eval/multi-swe-bench/`) and the
contract-first repair loop. All are **Local** unless noted. Each accepts
`--format text|json` (and a deprecated `--json` alias).

### `sensei benchmark-brief`

Build a compact local repair envelope from issue text, fail-to-pass tests,
changed files, and authored awareness.

| Flag | Default | Purpose |
|---|---|---|
| `--repo-root` | `.` | repo to analyze |
| `--task-file` | — | task JSON (`issue`/`f2p_tests`/`files`) |
| `--issue` | — | issue text |
| `--f2p-test` (rep.) | — | fail-to-pass test name |
| `--file` (rep.) | — | changed or suspect file |

### `sensei benchmark-judge`

Judge a patch envelope locally for contract preservation, test discipline, and
authority discipline. Same input flags as `benchmark-brief`, plus `--test-run`
(repeatable) for executed test ids.

### `sensei benchmark-score`

Run brief → judge → score in one pass; emits an overall 0–100 score and a
certification breakdown (scope / proof / authority / evidence lanes). Same input
flags as `benchmark-judge`, plus `--repair-success` to mark the repair itself
successful.

### `sensei benchmark-retry`

Build a reusable retry plan from a run record (and, for contract-first flows,
the learning event).

| Flag | Default | Purpose |
|---|---|---|
| `--mode` | — | **required**: `c` (Mode C) \| `d` (Mode D) |
| `--record` | — | **required**: benchmark run record (YAML/JSON) |
| `--event` | — | learning event (**required for mode `d`**) |
| `--retry-event` | — | optional retry attempt event for classification |
| `--retry-budget` | `1` | max retry attempts for this failure family |

### `sensei benchmark-event-meta`

Read a learning-event file and emit small stable orchestration metadata.

| Flag | Default | Purpose |
|---|---|---|
| `--event` | — | learning event YAML/JSON (**required**) |
| `--field` | — | print only one field (`event_id`, `task`, `decision_action`, `primary_failure_mode`, `certification_status`, `learning_evidence`, `retry_result_classification`) |

### `sensei certify`

Evaluate repair-governance certification from an authored learning event. This
is the local governance gate: score may be reported by the event, but
certification never depends on score.

| Flag | Default | Purpose |
|---|---|---|
| `--event` | — | learning event YAML/JSON (**required**) |
| `--proof-obligations` | `docs/awareness/generated/proof_obligations.yaml` | proof-obligations YAML used to evaluate required slots |
| `--format` | `text` | output format: `text` or `json` |
| `--json` | `false` | deprecated alias for `--format json` |
| `--field` | — | print only one field (`verdict`, `promotion`, `repair_claim_id`, `legacy_certification_status`) |

The command derives lane results from authored event metadata and reports:

- `scope`
- `authority`
- `proof`
- `evidence`

It also applies global blockers such as forbidden moves and keeps the invariant
`score_used_for_certification: false`.

For proof slots backed by `evidence_artifacts`, satisfaction is resolved in a
strict order:

1. explicit `satisfies` mapping
2. deterministic fallback from artifact kind plus related authority/proof refs
3. `available_unmapped`
4. `missing_source`

Artifact presence alone never satisfies a slot. The output preserves whether a
slot was satisfied by `mapping_source: explicit` or `mapping_source: inferred`.

Evidence lanes are enforced from the proof obligation itself:

1. `static_only` obligations fail through proof-side slots only
2. `runtime_required` obligations block promotion when runtime-side slots remain unsatisfied
3. `hybrid` obligations require both proof-side and evidence-side slots named by the obligation

Detected forbidden moves are evaluated as hard blockers. A repair with
`detected_forbidden_moves` remains `forbidden_move_detected` even if all proof
slots are satisfied and the event score is high.

### `sensei extract-authority`

Extract conservative `AuthoritySurface` candidates from Go source and write them
under the repo's candidate queue. This is extractor-only: emitted surfaces are
`status: candidate`, not live graph authority.

| Flag | Default | Purpose |
|---|---|---|
| `--repo-root` | `.` | repository root to scan |
| `--output` | `docs/awareness/candidates/authority_surface_candidates.yaml` | candidate YAML output |
| `--check` | `false` | compare the committed candidate file to a fresh run |

The extractor currently looks for clear authority signals only:

- HTTP handler surfaces
- filesystem mutation calls
- config mutation calls
- token/auth guard calls
- lifecycle control calls (`start` / `stop` / `restart` / `signal`)
- certificate / identity / peer / DNS mutation calls

It intentionally emits reviewable candidates, not promoted facts.

### `sensei extract-proof-obligations`

Generate deterministic proof obligations from authority surfaces. This is the
governance layer between project knowledge and repair certification: the output
states which proof slots a repair must satisfy before certification can pass.

| Flag | Default | Purpose |
|---|---|---|
| `--repo-root` | `.` | repository root |
| `--authority` | `docs/awareness/candidates/authority_surface_candidates.yaml` | authority surfaces input |
| `--output` | `docs/awareness/generated/proof_obligations.yaml` | generated proof obligations output |
| `--check` | `false` | compare the committed proof-obligations file to a fresh run |

Templates are intentionally coarse and deterministic in v1:

- `config_mutation`
- `cert_or_key_operation`
- `service_lifecycle`
- `peer_or_dns_mutation`
- `auth_or_token_gate`
- `filesystem_mutation`

Each generated obligation declares an evidence lane (`static_only`,
`hybrid`, or `runtime_required`) and required proof slots such as
`static_guard`, `artifact`, `before_after`, `runtime`, or `negative_contract`.

### `sensei proof-plan`

Show the governance checklist for a repair before editing. This command is
read-only: it resolves authority surfaces, proof obligations, and matching
forbidden fixes, then prints what must be proven before promotion is allowed.

Exactly one selector is required:

- `--file <repo-relative/path>`
- `--authority-surface-id <id>`
- `--proof-obligation-id <id>`
- `--repair-claim <learning_event.yaml>`

| Flag | Default | Purpose |
|---|---|---|
| `--repo-root` | `.` | repository root |
| `--authority` | `docs/awareness/candidates/authority_surface_candidates.yaml` | authority surfaces input |
| `--proof-obligations` | `docs/awareness/generated/proof_obligations.yaml` | proof obligations input |
| `--forbidden-fixes` | `docs/awareness/architecture/forbidden_fixes.yaml` | forbidden fixes input |
| `--format` | `text` | output format: `text` or `json` |

The output includes:

- matched authority surfaces
- required proof obligations and their evidence lanes
- required slots that must be satisfied before promotion
- file-matched forbidden fixes that remain hard blockers if detected

### `sensei seed-status` — Oxigraph

Check whether generated graph state, committed `awareness.nt` /
`awareness.transaction.tsv`, and the live Oxigraph store are aligned. When repo
context is available, `seed-status` computes the fresh artifact in memory and
reports split-brain authority explicitly instead of only checking the live
marker.

| Flag | Default | Purpose |
|---|---|---|
| `--seed` | auto (embedded) | path to `awareness.nt` |
| `--oxigraph-url` | `http://localhost:7878/query` | Oxigraph query/store endpoint |
| `--services-repo` | auto | path to paired services repo for fresh in-memory generation |
| `--ag-repo` | auto | path to awareness-graph repo for seed + transaction comparison |
| `--require-current` | `false` | exit 1 if the live store lacks this marker |
| `--json` | `false` | JSON output |
