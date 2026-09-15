# The live migration procedure, and its rollback

Required by the plan before execution: *"Do not execute the live migration without
reporting the exact procedure and rollback first."* This is that report. **Nothing here
has been executed.**

Written 2026-09-13, after the Phase 7 disposable-store proof
(`graph-identity-phase7-proof.md`) passed 11 properties and changed what this procedure
needs to say.

## The finding that makes this small

The migration is **not a graph operation**. Measured on this machine today:

| domain | store | store triples | marker digest / count | agree? |
|---|---|---|---|---|
| `github.com/globulario/sensei` | `:7881` | 142,739 | `92e6cf2fe79cd44d…` / 142,739 | **yes** |
| `github.com/globulario/sensei-code` | `:7882` | 35,268 | `c0b660fc42a50c4b…` / 35,268 | **yes** |
| `github.com/globulario/services` | `:7878` | 237,049 | — (no domain marker) | n/a |

Both governed stores already serve exactly what their markers certify. What is missing
is only the **declaration**: `~/.sensei/domains.yaml` currently declares
`active_generation` for **zero** domains, so every reader reports "NOT DECLARED …
cannot be verified" — inert, exactly as designed, and useless until an operator says
which generation is active.

So the migration is: **write two lines into the registry that state what is already
being served.** No rebuild, no reload, no data movement, no service restart.

## Preconditions (verify, do not assume)

Run and confirm each before step 1. All are read-only.

```bash
# P1 each store serves the count its marker certifies
for p in 7881 7882; do curl -s -X POST -H 'Content-Type: application/sparql-query' \
  --data 'SELECT (COUNT(*) AS ?n) WHERE { ?s ?p ?o }' \
  http://127.0.0.1:$p/query -H 'Accept: text/csv' | tail -1; done
#   expect 142739 then 35268

# P2 the markers say the same
python3 -c "import json;d=json.load(open('$HOME/Documents/github.com/globulario/sensei/.sensei/graph-authority.json'));print(d['digest_sha256'],d['triple_count'])"
python3 -c "import json;d=json.load(open('$HOME/Documents/github.com/globulario/sensei-code/.sensei/graph-authority.json'));print(d['digest_sha256'],d['triple_count'])"
#   expect 92e6cf2fe79cd44d… 142739   and   c0b660fc42a50c4b… 35268

# P3 both services are serving
systemctl --user is-active sensei-awareness-graph-sensei.service \
  sensei-awareness-graph-sensei-code.service

# P4 back up the registry
cp ~/.sensei/domains.yaml ~/.sensei/domains.yaml.pre-active-generation
sha256sum ~/.sensei/domains.yaml | tee ~/.sensei/domains.yaml.sha256.before
```

If P1 and P2 disagree for a domain, **stop**. A disagreement means the store is not
serving its certified generation, and declaring a pointer would make a stale graph
authoritative. That is a rebuild question, not a migration question.

## The procedure

**Step 1 — declare the pointer for one domain only.** Start with `sensei-code`: it is
the smaller graph and the one whose readers are sensei-code's own governed runs, so a
mistake surfaces immediately in a lane that already refuses on disagreement.

Edit `~/.sensei/domains.yaml`, adding one line under
`github.com/globulario/sensei-code:`

```yaml
    active_generation: c0b660fc42a50c4bREPLACE_WITH_THE_FULL_DIGEST_FROM_P2
```

Use the **full** digest from P2, never a prefix: a prefix is not the generation, and the
comparison refuses one.

**Step 2 — verify, from the production reader.**

```bash
sensei metadata --domain github.com/globulario/sensei-code
```

Required output:

```
  Generation verdict:  the served graph IS the declared ACTIVE generation
  Disagreements:       none (endpoint, marker and ACTIVE generation all agree)
```

Any other verdict → go to **Rollback** immediately. Do not proceed to step 3.

**Step 3 — exercise one governed read.** Run any sensei-code command that consults the
graph (`sensei briefing --file <a high-risk file>`). It must behave exactly as before.
The pointer is a new refusal path; this confirms it does not refuse a correct graph.

**Step 4 — repeat steps 1–3 for `github.com/globulario/sensei`** with its own digest.

**Step 5 — record the orphan.** `sensei/.awg/graph-authority.json` holds
`c24d8f6093de785f…` / 4,284 triples — a generation nothing serves, from before the
`.awg` → `.sensei` rename. `sensei metadata` now reports it. Deleting it is an operator
action and is **not** part of this migration; do it separately so a problem in either
change cannot be confused with the other.

## Rollback

Total, immediate, and at every step:

```bash
cp ~/.sensei/domains.yaml.pre-active-generation ~/.sensei/domains.yaml
sha256sum -c ~/.sensei/domains.yaml.sha256.before
```

Nothing else is touched by this procedure, so nothing else needs undoing. No graph, no
store, no marker, no service. An absent `active_generation` is inert by construction, so
rollback returns every reader to exactly today's behaviour.

## The constraint the proof established — read before scheduling anything

**A whole-store digest is a per-store identity, not a per-domain one.** Publishing any
domain into a store recomputes the whole-store digest, so every *other* domain's pointer
in that store immediately goes stale and its readers refuse. Measured directly in Phase
7: in one reader process, at one moment, one domain agreed and another refused, the only
difference being whether the activation had recorded the pointer.

The current deployment is already one store per domain (`:7881` sensei, `:7882`
sensei-code), so this procedure is safe **as long as that stays true**. Two consequences:

1. **Never publish two governed domains into one store while pointers are declared.**
   `sensei build --repo A --store-url <store serving B>` will strand B. The
   non-canonical `--store-url` notice fires, but it warns; it does not refuse.
2. The durable fix is an owner's choice, and it is not made yet: either forbid a shared
   store outright, or make `active_generation` name the domain's **named-graph** digest
   rather than the whole-store digest. Until then this procedure is correct and its
   scope is one domain per store.

## What this procedure deliberately does not do

- **No `sensei build --all`.** Standing instruction, and unnecessary: nothing needs
  rebuilding.
- **No rebuild, reload, import or refresh.** The stores already serve their certified
  generations.
- **No service restart.** The pointer is read by the CLI from the registry, not by the
  server. The awareness-graph binaries currently running predate the served-generation
  fix (`7a7821ce`), which does not matter here because expected and served agree today —
  deploying that binary is a separate, later change, and it is what makes the pointer
  useful when they *stop* agreeing.
- **No awareness candidate promotion**, and no touching the standing W3 authority
  question.

## Authorization

This document is the report the plan requires. Executing step 1 needs an explicit
instruction naming this procedure. The smallest authorizing sentence is:

> Authorized: execute the live migration procedure in
> `docs/architecture/graph-identity-live-migration.md`, steps 1–4.
