# #259 graph snapshot witness

**Status: provenance record. Changes nothing. The frozen reference set and the
execution identity are untouched.**

The graph the #259 experiment is pinned to existed only inside a running store.
This records that it has been exported, and that its identity has been
**reproduced from the export in an isolated store** — so the experiment no
longer depends on one process, one port, or one directory surviving.

## Identity

| | |
|---|---|
| expected semantic identity | `def94857a06a997412c56c682c39481b226f1834f93a4173425852965367b912` |
| world revision | `eac9603e332e2393815fb702c7aa1a105302ee20` |
| execution identity | `4c641b99482504fcda656dd89922110c895227087d0ebe69650d73e6441ebb2b` |
| sample manifest | `a6fc72d75ef6fc080f129ed2de06c85742a35617487321f041904ac48d8c0364` |
| blind corpus | `6334f400bcb805f6787c353a280859753a74390dce2a53fd2c389a4aaedbdfe4` |

## The export

```
triples exported            158349
named graphs                0        (the default graph is the whole dataset,
                                      so N-Triples loses nothing here)
raw sha256 of the dump      4cccb987acae5beea2cb47a67abee87b7c1ef4d13486f127cbddd554de43f1cd
sorted sha256               bb44ad839f9830097fd4003789c89029fd3f074b2e2d18be89942f37764fe306
restore identity verified   TRUE
```

The dump is **not** committed — it is 31 MB. It lives outside any repository at
`~/Documents/github.com/globulario/sensei-259-graph-snapshot/live-graph-def94857.nt`.
Anyone relying on this witness must confirm that file still hashes to
`4cccb987…` before trusting a restore from it.

### Export command

```
curl -H "Accept: application/n-triples" \
     "http://127.0.0.1:7881/store?default" -o live-graph-def94857.nt
```

Named-graph check, which is what makes an N-Triples export sufficient:

```
SELECT (COUNT(DISTINCT ?g) AS ?n) WHERE { GRAPH ?g { ?s ?p ?o } }   →  0
```

## Restore proof

Performed on 2026-08-23 against a **separate store directory and port**, with no
mutation of the live store, and with no #259 retrieval executed over the 48
sampled changes — so nothing about adjudication was contaminated by it.

```
oxigraph load  --location <fresh dir> --file live-graph-def94857.nt
oxigraph serve --location <fresh dir> --bind 127.0.0.1:7883

awareness-graph -addr :10133 -oxigraph-url http://127.0.0.1:7883/query -no-seed \
  -home-domain github.com/globulario/sensei-code \
  -repo-domain github.com/globulario/sensei-code \
  -repo-root  <sensei-code checkout> \
  -awareness-dir <sensei-code checkout>/docs/awareness

sensei metadata --addr localhost:10133 --domain github.com/globulario/sensei
```

Result — identical to the live store on every identity-bearing field:

```
Seed digest         def94857a06a997412c56c682c39481b226f1834f93a4173425852965367b912
Live graph digest   def94857a06a997412c56c682c39481b226f1834f93a4173425852965367b912
Live graph triples  158349
Freshness state     current
Authority verdict   authoritative
```

### Why the file hash and the identity differ

`sha256(dump)` is `4cccb987…`, not `def94857…`, and an earlier reading of that
concluded the identity was unrecoverable. That was wrong. The identity is
carried by marker triples **inside the graph**, not computed over a
serialization — which is precisely why it survives an N-Triples round trip while
the file hash does not match. Content and identity are different properties, and
here both are preserved.

## Versions

```
oxigraph          0.5.9
awareness-graph   sha256 1070ee8dedeec323…  (the same binary serving the pinned store)
live store        /home/dave/.sensei-code-graph/store   (104 MB, persisted, survives restart)
```

The restore was run with the *same* server binary as the pinned store, so the
result does not establish that a different build reproduces the identity.

## Standing rule

> **Do not rebuild or reload the #259 graph until Slice 2 has run.**

`sensei build`, `sensei-code setup --apply`, or anything else that reloads that
store would produce a legitimately different experimental world — more so now
that #279 and #280 have moved the repository forward and added a new ontology
class and code symbols. The old world would still be restorable from the export
above, but the running one would no longer be it.

## Requirement this exposes for future freezes

A frozen graph is not *"a server currently reports digest X."* That is a live
witness, not an artifact. A frozen graph is:

```
portable snapshot
+ canonical identity algorithm and version
+ expected identity
+ successful isolated restore proof
```

v1 acquired the last three retroactively and passed. A future reference set
should produce them at freeze time rather than discovering afterwards whether it
can.
