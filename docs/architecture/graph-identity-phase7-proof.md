# Phase 7 — the disposable-store proof

The plan requires this before any live migration: *"construct a complete graph
generation in a disposable store... Only after those pass should you propose a live
migration procedure."*

Run 2026-09-13. Everything below happened on a **disposable** Oxigraph store on
`127.0.0.1:7899`, a **disposable** awareness-graph reader on `127.0.0.1:10199`, and a
**disposable** domain registry under a scratch `HOME`. The served stores and the
operator's registry were measured before and after:

| | before | after |
|---|---|---|
| store 7878 (services) | 237,049 | 237,049 |
| store 7881 (sensei) | 142,739 | 142,739 |
| store 7882 (sensei-code) | 35,268 | 35,268 |
| `~/.sensei/domains.yaml` | `9e729a83…` | `9e729a83…` (sha256 verified) |
| both checkouts | clean | clean |

The scratch `HOME` mattered: after G4 unified the activation transition, **any**
publication records the ACTIVE pointer into `~/.sensei/domains.yaml`. Running the proof
without isolating `HOME` would have moved a real operator pointer.

## What passed

| # | property | result |
|---|---|---|
| 1 | construct a complete generation in a disposable store | **PASS** — 142,252 triples published, store 0 → 142,270, closure PROVEN (559/559) |
| 2 | law 14: a raw store URL announces itself as non-canonical | **PASS** — the override notice named the configured endpoint it was overriding |
| 3 | the activation transition records the pointer | **PASS** — `ACTIVE generation: b6657e8d… for github.com/globulario/sensei` |
| 4 | the surgical registry edit preserves operator content | **PASS** — the file's comments survived a live rewrite |
| 5 | a reader states whether it serves the declared generation | **PASS after a repair** — see below |
| 6 | marker/store disagreement is reported with both values | **PASS after the same repair** |
| 7 | law 12: a declared generation the store does not serve | **PASS** — "ambiguous graph identity… which generation is ACTIVE cannot be decided here" |
| 8 | laws 7–8: a failed G+1 leaves G untouched | **PASS** — store, marker sha256 and pointer byte-identical; no G+1 marker created; `mutation_started: false` |
| 9 | activate G+1 | **PASS** — store 142,270 → 176,795, new generation `717b0f32…` |
| 10 | law 5 / witness J: a reader pinned to G cannot silently consume G+1 | **PASS** — see the decisive measurement |
| 11 | a malformed registry fails closed | **PASS** — the build refused naming the YAML parse error rather than falling back to a guessed tier |

### The decisive measurement

One store, one reader process, one moment, two domains:

```
domain github.com/globulario/sensei          (pointer still names G)
  Generation verdict:  ambiguous graph identity … which generation is ACTIVE cannot be decided here.

domain github.com/globulario/sensei-code     (pointer updated by the activation)
  Generation verdict:  the served graph IS the declared ACTIVE generation
```

The only difference between refusing and proceeding is whether the activation recorded
the pointer. Laws 1, 4, 5 and 12 and the G4 unification, demonstrated together.

## What the proof CAUGHT — the field that was blank exactly when it mattered

`LiveStoreGraphDigestSha256` is documented, in its own contract, as *"the digest of the
graph actually being served"*. It was not.

The first run of property 5 reported:

```
Marker verdict:      cannot be verified: the served graph states no digest
Generation verdict:  … the served graph states no generation
```

while the store demonstrably held marker triples for `b6657e8d`. The cause is in
`seedmeta.VerifyLiveStore`: it locates the live marker with `Describe(expected.IRI)` —
it looks up **only the generation it expected**. When the store serves a different one,
describe returns nothing, the function exits early as STALE, and `ver.Live` is never
populated.

So the field was empty **exactly when the served graph was not the expected graph**,
which is the only case any of its consumers exist for:

| consumer | behaviour with a blank served digest |
|---|---|
| `markerAgreement` (law 10, 13) | "cannot be verified" — never a mismatch |
| `verifyActiveGeneration` (laws 1, 12) | unverifiable — never a mismatch |
| sensei-code's pinned-generation check (law 5) | falls into its "this server predates the field" branch |

Three checks built this week, all reading one field, and that field went blank in the
failure case. It is the sharpest instance of a law recorded earlier the same day: *a
guard whose operand cannot express the fault reads as enforcement and can never fire.*

### The repair

The owner already existed. `seedmeta.DiscoverLiveMarkers` finds markers **by class** —
"never by looking up the expected IRI", as its own comment puts it — and
`AdmitLiveMarker` returns an `AuthorityObservation` whose `LiveIdentity` is the
independently discovered served digest. The control-state provider already consumed it.
Only the two response builders did not.

Two rules in the fix:

- **Cost.** The discovery runs only when the cheap lookup established nothing. On a
  healthy store the expected marker is present and nothing extra is asked — the
  "resolved only when asked" rule the authority projection already follows, asserted by
  a test that counts discovery calls.
- **Only a coherent identity is an identity.** `AuthorityCurrent` and
  `AuthorityStaleAdmissible` mean a self-consistent marker at its own digest-derived
  IRI: another graph, but a real one. Every other state yields `""` even where the
  observation carries a digest — an integrity-failed store's self-description is
  precisely what must not become a comparable identity, and two live markers must not
  be resolved by picking one.

## What the proof also caught, and did not fix

**An orphaned legacy marker exists on this machine right now.**
`/home/dave/Documents/github.com/globulario/sensei/.awg/graph-authority.json` is present
alongside the `.sensei` one. G4's orphan detection found a real instance, not a
synthetic case. It is reported by `sensei metadata`; removing it is an operator action.

**A whole-store digest is a per-STORE identity, not a per-domain one.** This is the
finding that most affects the live procedure. Publishing domain B into a store
recomputes the whole-store digest, so domain A's ACTIVE pointer — which names the old
whole-store digest — immediately goes stale, and every reader of A refuses. That is
exactly what proof 10 shows, and it is *correct* fail-closed behaviour for a pointer
defined this way, but it means:

> On a shared store, one domain's publication invalidates every other domain's
> generation pointer.

Two ways out, and this is an owner's architectural choice, not a defect to patch:

1. **One store per domain.** Already the deployment shape here (7881 for sensei, 7882
   for sensei-code), in which case the whole-store digest *is* the domain's generation
   and nothing further is needed — but a shared store must then be forbidden rather
   than merely unusual.
2. **Make the pointer name a domain-scoped digest** — the digest of the domain's named
   graph rather than of the whole store — in which case publishing a neighbour leaves
   the pointer valid.

Until that is decided, a live migration must publish **one domain per store**, and the
procedure below says so.
