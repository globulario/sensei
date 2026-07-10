# Sensei cold-bootstrap — teach your agent another repo's hard-won laws

*Hands-on walkthrough. Point Sensei at a mature repo's history, promote one
reviewed rule, and watch it warn a future bad edit — all without polluting your
own graph. ~5 minutes.*

Most of Sensei is about making **your** codebase's rules queryable. Cold-bootstrap
is the other direction: a repo you depend on (Caddy, etcd, …) already paid for
its rules in bugs, reverts, and review comments. Cold-bootstrap mines those
scars, lets you promote the load-bearing ones, and serves them **scoped to that
repo** so an agent editing that code gets warned *before* it repeats the mistake.

```
external repo history → candidate rules → citation check → you promote
  → domain-scoped serving → warning on a bad edit
```

This page walks the committed **Caddy pilot** end to end with real commands and
output. Nothing here mutates your production graph — it runs against a throwaway
store.

---

## What you'll see

One reviewed Caddy rule —
*"Caddyfile directive errors must use `dispenser.Errf`, not `fmt.Errorf`, so the
error keeps its source location"* (drawn from real caddy PR #7814 review
comments) — gets:

1. **promoted** into a repo-scoped graph for `github.com/caddyserver/caddy`,
2. **served** in a briefing for the real Caddy file, **with provenance**,
3. used to **warn** on a bad future edit (`fmt.Errorf`) — and stay silent on a
   compliant one (`dispenser.Errf`),
4. **isolated**: it never appears for a Globular file, in either direction.

## Prerequisites

```bash
git clone https://github.com/globulario/sensei && cd awareness-graph

# Build the CLI and the server binary (awg serve execs ./bin/awareness-graph),
go build -o /tmp/awg ./cmd/awg
go build -o bin/awareness-graph ./golang/server
# and have an `oxigraph` binary on PATH or in ./bin/ (the demo starts its own).
```

## The fast path — one command

```bash
AWG_BIN=/tmp/awg bash pilot/caddy/demo.sh
```

It starts an **isolated** Sensei (gRPC `:10121`, a throwaway Oxigraph on `:7901`
under `/tmp/awg-pilot-demo`), promotes the rule, runs every step below, prints a
pass/fail line for each isolation property, and tears the store down. To see the
moving parts, run the steps yourself.

---

## Step by step

### 0. Start an isolated Sensei (separate ports + throwaway data dir)

```bash
/tmp/awg serve --addr :10121 --oxigraph-bind 127.0.0.1:7901 --data /tmp/awg-pilot-demo &
```

It seeds the embedded Globular graph into the fresh store; nothing touches your
real `:7878` store.

### 1. Promote the reviewed Caddy candidate

```bash
/tmp/awg promote --repo github.com/caddyserver/caddy --no-rebuild \
  caddy.reverseproxy.forwardauth_errf_preserves_location
```

This writes a **domain-tagged** rule into `pilot/caddy/invariants.yaml`
(repo + provenance preserved) and **does not** rebuild Globular's embedded seed.
Promotion is explicit and human-gated — there is no auto-promotion.

Then load just that pilot graph into the isolated store (additive):

```bash
/tmp/awg build --input pilot/caddy --output /tmp/pilot.nt
curl -s -X POST -H 'Content-Type: application/n-triples' \
  --data-binary @/tmp/pilot.nt 'http://localhost:7901/store?default'
```

### 2. Brief the real Caddy file — the rule appears, with provenance

```bash
/tmp/awg briefing --addr localhost:10121 \
  --file modules/caddyhttp/reverseproxy/forwardauth/caddyfile.go \
  --domain github.com/caddyserver/caddy
```

```
Direct invariants:
- [medium] caddy.reverseproxy.forwardauth_errf_preserves_location — Caddyfile directive errors must use dispenser.Errf, not fmt.Errorf

Provenance (promoted repo-scoped rules):
- caddy.reverseproxy.forwardauth_errf_preserves_location: repo github.com/caddyserver/caddy · origin coldsource · review load-bearing · bundle caddy-reverseproxy-forwardauth-2026-06 · range HEAD~500..HEAD · cites: …#7814 c3390255669; …#7814 c3390101816
```

The provenance line is the rule's chain of custody: which repo, that it was
cold-sourced, the human review label, and the citations behind it.

### 3. Edit-check a **bad** future edit — it warns

```bash
/tmp/awg edit-check --addr localhost:10121 \
  --file modules/caddyhttp/reverseproxy/forwardauth/caddyfile.go \
  --domain github.com/caddyserver/caddy \
  --content 'return fmt.Errorf("cannot re-declare uri: %s", uri)'
```

```
warnings: 1
[warning] caddy.reverseproxy.forwardauth_errf_preserves_location (Invariant)
  forbidden pattern matched: \bfmt\.Errorf\(
  provenance: repo github.com/caddyserver/caddy · origin coldsource · review load-bearing · …
```

This is **advisory** — a warning, never a block, and it never edits your code.

### 4. Edit-check a **compliant** edit — silence

```bash
/tmp/awg edit-check --addr localhost:10121 \
  --file modules/caddyhttp/reverseproxy/forwardauth/caddyfile.go \
  --domain github.com/caddyserver/caddy \
  --content 'return dispenser.Errf("cannot re-declare uri: %s", uri)'
```

```
warnings: 0
```

### 5. The Caddy rule never reaches Globular

A Globular file's briefing carries Globular's own rules — never the Caddy one —
and the same bad `fmt.Errorf` edit on a Globular file raises **no** Caddy
warning:

```bash
/tmp/awg edit-check --addr localhost:10121 \
  --file golang/repository/repository_server/repository_server.go \
  --content 'return fmt.Errorf("boom")'
# warnings: 0   (the Caddy rule is out of scope here)
```

A query that spans two domains with no `--domain` **fails closed** rather than
mixing rules across repos.

---

## Why this matters

You just taught an agent a rule that the Caddy maintainers learned the hard way —
and the agent will now be reminded of it the moment it's about to reintroduce the
bug, *in Caddy's code only*. That is the whole point: **Sensei extracts the laws a
project already paid for, and makes future agents aware of them before they
repeat the same class of mistake.**

The v0 runs that back this up (Caddy and etcd, live LLM drafter):

| | load-bearing | wrong | fabricated citations |
|---|---|---|---|
| **Caddy** | ~50–60% | **0** | **0** |
| **etcd**  | ~40–60% | **0** | **0** |

The drafter narrowed noisy areas into sharp rules instead of inventing them, and
the **citation cage** rejected anything it couldn't anchor to real evidence. See
[the v0 milestone](milestone-cold-bootstrap-v0.md) for the full account.

## What this is *not* (yet)

- **Not autonomous truth** — a drafter proposes; a human promotes. No
  auto-promotion, no bulk promotion.
- **Warning-only** — no hard blocking, no CI gate.
- **Not SaaS-proven** across all languages/repos — two Go repos so far.
- The pilot serves from a throwaway store; foreign rules never ride inside
  Globular's shipped seed.

## Where to look next

- `pilot/caddy/demo.sh` — the exact script behind this page.
- `pilot/caddy/README.md` — the pilot's provenance contract and layout.
- [`milestone-cold-bootstrap-v0.md`](milestone-cold-bootstrap-v0.md) — what v0
  proved, what it doesn't claim, and the possible next phases.
