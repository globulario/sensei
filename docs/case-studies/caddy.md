# Case study: Sensei on Caddy

**Can Sensei recover a real project's architecture laws — accurately — from a
cold checkout?** We pointed it at [Caddy](https://github.com/caddyserver/caddy),
a large, well-known Go web server (322 Go files), starting from a **pristine,
pre-Sensei tree** with no `docs/awareness/` and no `.awg/`.

Everything below is reproducible. The claims split into two honest layers:

1. **Structure** — deterministic, zero-LLM, instant. What Sensei extracts the
   moment you point it at a repo.
2. **Laws** — mined from the project's own history, and **each one cites a real
   Caddy PR** so you can verify it wasn't invented.

---

## Layer 1 — structure, extracted cold

Deterministic static extraction only. No LLM, no network, no code execution —
just parsing the tree.

```bash
sensei bootstrap --repo /path/to/caddy --dry-run
```

```
AWG bootstrap report — caddy
  components found:           17
  tests found:                490
  source anchors found:       176
  import dependencies:        35
```

Loaded into an **isolated, domain-scoped** graph (never the live store), it
proves it can actually answer file-level questions — and that domains don't leak
into each other:

```bash
scripts/awg-bootstrap-foreign.sh /path/to/caddy github.com/caddyserver/caddy \
    modules/caddyhttp/reverseproxy/caddyfile.go
```

```
  import-scan[go]: 17 components
  total: 773 triples, validated
PASS: structural context resolves for github.com/caddyserver/caddy — Mode C graph is real
PASS: no cross-domain leak
```

This is the safe "try it on any public repo" path: static, reproducible, no
secrets, isolated on non-live ports.

---

## The money shot — a briefing before an edit

One command stands up a private graph and hands back a real briefing — the same
context an agent gets over MCP *before* it touches the file:

```bash
sensei demo --repo /path/to/caddy --file modules/caddyhttp/reverseproxy/caddyfile.go
```

```
  ✓ awareness loaded (6265 triples)
  ✓ briefing generated

Awareness briefing for modules/caddyhttp/reverseproxy/caddyfile.go

Decision focus:
- Respect: [warning] caddy.caddyfile_unmarshaler_uses_dispenser_errf —
  Caddyfile unmarshalers report errors with the dispenser, not fmt.Errorf

Architecture (direct):
- [contract] contract.caddy.caddyfile_unmarshaler — Caddyfile unmarshaler error reporting

(generated in 5ms)
```

An agent about to edit that unmarshaler now knows the rule it would otherwise
break: use `d.Errf` / `d.ArgErr`, not `fmt.Errorf` — or the config error loses
its source location.

---

## Layer 2 — the laws, mined from Caddy's own history

This is the part people distrust, so here is exactly how it works and what it
costs.

**Cold, single-channel yields nothing — by design.** Scanning only commits, with
no PR-review channel, Sensei refuses to guess:

```bash
sensei cold-bootstrap --repo /path/to/caddy --auto-window --dry-run
```

```
  total signals found:       37
  themes found:              26
  triangulated themes:       0
  candidates accepted:       0      ← single-source themes held back
```

A law must **triangulate** across at least two distinct evidence types (e.g. a
revert commit *and* a reviewer's comment). No triangulation, no candidate. That
is why "clone it and Sensei magically knew" is *not* a claim we make.

**With the PR-review channel, the scars surface — each cited.** Feeding Caddy's
PR reviews in (`--repo-slug caddyserver/caddy`), Sensei mined **10 triangulated
candidates** citing **12 distinct real PRs/issues** (#4952, #6447, #6769, #6829,
#6984, #7095, #7141, #7243, #7300, #7529, #7697, #7761). After human review, six
became active invariants and four became forbidden-fix rules.

Every rule below is checkable against Caddy's public history:

| Architecture law Sensei recovered | Grounded in |
|---|---|
| Caddyfile unmarshalers must report errors with the dispenser (`d.Errf`/`d.ArgErr`), not `fmt.Errorf`, so the error keeps its token's `file:line` | Caddy convention (invariant + forbidden fix) |
| Every module declares a `CaddyModule` ID and registers itself; unregistered modules aren't loadable | structural + tests |
| Resources acquired in `Provision` must be released in `Cleanup` — else every `caddy reload` leaks handles/goroutines | structural + failure-mode |
| The running config is mutated **only** through `changeConfig` (atomic swap under the config lock; readers never see a partial config) | `admin.go` + revert history |
| Context values are keyed by a typed `caddy.CtxKey`, never a plain string (collision + divergence from `ServerCtxKey`/`ErrorCtxKey`) | **PR #6984** review |
| A streaming reverse-proxy copy must honor context cancellation — the shortcut leaks the connection/goroutine | **PR #4952**, tried in `f5dce84a`, **reverted by `238f1108`** — a known-broken repair |

The last two are the proof that this is *mined, not guessed*: they point at a
specific reviewer comment and a specific tried-then-reverted commit. You can open
the PRs and read them.

---

## The score — and what it deliberately does *not* claim

```bash
sensei repo-eval --repo /path/to/caddy
```

```
Overall posture: good (79/100, confidence: medium)
Agent readiness: guarded_repair_only (80/100)

Basis of this verdict — what it does NOT verify:
  - critical-coverage scored over an EMPTY measured surface (0 nodes) —
    the score reflects absence of measured critical surface, not verified governance
  - scores the COMMITTED corpus; freshness vs current source is NOT verified
```

The evaluation reports its own blind spots. A tool that tells you what it did
**not** verify is one you can trust with what it *did*.

---

## Reproduce it

```bash
# clean, pre-Sensei tree
git clone https://github.com/caddyserver/caddy /tmp/caddy && cd /tmp/caddy

# Layer 1 — structure (deterministic, instant)
sensei bootstrap --repo /tmp/caddy --dry-run

# money shot — a real briefing
sensei demo --repo /tmp/caddy --file modules/caddyhttp/reverseproxy/caddyfile.go

# Layer 2 — mined laws (needs the PR-review channel to triangulate)
sensei cold-bootstrap --repo /tmp/caddy --repo-slug caddyserver/caddy \
    --auto-window --dry-run

# the honest score
sensei repo-eval --repo /tmp/caddy
```
