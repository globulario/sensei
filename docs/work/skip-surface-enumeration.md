# Priority 5 closure: the full skip surface in this repository

Priority 5's closure rule:

> When repeated review findings instantiate one law across sibling surfaces,
> enumerate every surface that consumes the affected semantic state before
> declaring closure.

I did not do that. The law — *a check reports its inability to run by failing,
never by skipping* — was repaired in three PRs (#321, #323, #324) and I
described the result as the class being handled. The enumeration says
otherwise.

```
skip sites in this repository        114
examined before declaring closure     51   (cmd/awg, golang/reachability, golang/server)
NEVER EXAMINED                        63
```

I repaired specimens and stopped at the packages I happened to be editing.
That is the exact behaviour the closure rule exists to prevent.

## Classification of all 114

```
A  named external limit (binary, tool, platform, credential absent)   38
B  zero / empty / absent state — candidate fail-open                  27
C  remainder (fixture-shape and configuration conditions)             49
```

Class A is correct and stays. Class B is the dangerous set: a condition that
can only arise when something has gone wrong, reported by falling silent.

## Repaired here

Two Class B sites, chosen because they protect the **display-cap law** — that
an anchor capped out of a response still reaches risk classification:

```
golang/server/preflight_applicability_test.go:95   "the response showed every anchor;
                                                    this fixture no longer exercises the cap"
golang/server/preflight_applicability_test.go:229  "the response showed the security anchor;
                                                    this fixture no longer exercises the cap"
```

The cap engaging is the **premise** of the assertion that follows. Losing it
retired the only check that a hidden anchor still votes on risk — and both
ways of losing it (the corpus shrinking, the cap being raised) are repository
changes somebody should see. Both now fail and name what must be restored.

### A note on the mutation testing

The first four mutations I ran **survived**, and the reason matters more than
the result: I mutated `maxPatternsPerBriefing`, then `maxSurfaceNodesPerClass`,
then the compact profile's `impactNodes` — constants I picked by *name*. None
of them governs this response. The cap that does is
`preflightCapsCompact.invariants = 3` (`golang/server/preflight.go:53`), found
by following the value to `capNodes(...)` at line 241.

With the right constant the pair is clean:

```
M26e preflightCapsCompact.invariants 3 -> 99      KILLED, naming the lost premise
M27b same mutation, Fatal reverted to Skip        SURVIVES — the defect
```

**A surviving mutation can mean the mutation missed, not that the code is
unprotected.** Stopping at the first survivor would have produced the opposite
conclusion, and stopping before mutating at all would have shipped a repair
with no evidence that it binds to anything.

## Second pass: nine more repaired

Class B was worked through rather than left as a list.

```
cmd/prospective-label/load_test.go:48,80    the frozen reference set is 56 TRACKED
                                            files; absence is a defect
investigation_test.go:976,990               createValidBaseDocument builds the
                                            fixture IN CODE, so a fixture lacking
                                            the property is a defect in the fixture
questiondisposition/matrix_test.go:138      seedDisposable likewise; with no scope
                                            domain the fail-closed path is never reached
golang/server/packaging_test.go:457,487     the seed is EMBEDDED and COMMITTED and
                                            the marker is what certifies it
preflight_applicability_test.go:95,229      the display-cap law (first pass)
```

`packaging_test.go:487` is the one to read. Its own comment says *"Absence of
evidence is not a pass"* — and it skipped on absent evidence. The rule was
written directly above the line breaking it, which is now the third time in
this program that a test defeated a guarantee stated in its own comment.

```
M28 embedded seed marker removed                 KILLED
M29 reference set hidden                         KILLED
M30 createValidBaseDocument yields no coverage   KILLED
M31 seeded question loses its scope domain       KILLED
```

Each mutation removed the real thing — the marker, the directory, the coverage,
the domain — and ran the suite.

### Four reclassified as Class A, correctly

`cmd/eval-arms/protocol_test.go:128` (needs an external gin checkout),
`cmd/awg/cmd_gate_contracts_test.go:320` (`eval/` is excluded from the
standalone build, and says so), `cmd/awg/helpers_test.go:207` and
`cmd/principle-check/meta_principle_coverage_test.go:445` (both need a sibling
services checkout). These name a genuine external limitation and stay as skips.

## Third pass: the "at least one" shape

Two more, and the first needed a different repair than the rest.

**`proofdischarge/fixtures_test.go:42`** iterates bundles, and not every bundle
carries a `proof_discharge`. So skipping *a* bundle is correct — but skipping
*every* bundle means the frozen validator was never exercised and the package
still prints `ok`. A blanket `Fatal` would have been wrong here. The per-bundle
skip stays and the **total** is asserted: `validated == 0` fails.

**`closureprotocol/contract_test.go:54`** skipped when the tracked fixture
carried no operational artifact receipts — without them the cross-result
rejection below is never attempted.

```
M34 no bundle carries a proof_discharge          KILLED
M35 same, with the total assertion removed       SURVIVES — the defect
M36 fixture loses its operational artifacts      SURVIVED — see below
M36b same mutation, placed in the right function KILLED
```

### The second time a survivor meant the mutation missed

`M36` set the field to `nil` after `contractOf(loadValidTransition(t))` — and
that line appears **three times** in the file. A single-occurrence replace hit
line 44, inside a different test, so the function under test never saw it.

This is the same error as `M26` earlier in this document, where four mutations
survived because they targeted cap constants chosen by name. Both times the
survivor read as "the code is unprotected" when it meant "the mutation did not
apply."

**So a surviving mutation is not a result until the mutation is shown to reach
the code under test.** The cheap check is to make the mutation fail *loudly*
first — if it cannot even change behaviour, it is not measuring anything.

## Still not repaired, and why

Twelve Class B sites remain.
They are not repaired in this PR because they are not one change: they span
`golang/architecture/*` (fixture-carries-no-X conditions),
`golang/server/packaging_test.go` (embedded seed markers),
`cmd/prospective-label`, and `cmd/eval-arms` — different owners, different
questions about whether the absent thing is tracked or generated, and each
needs the surrounding test read rather than the message classified.

**Closure is still NOT declared**, and the count is now:

```
114 total   38+4 Class A (correct)   13 repaired   12 Class B open   47 Class C unreviewed
```

The enumeration exists so the remainder is a known quantity rather than an
assumption, which is the whole point of the rule. Declaring closure at 51 of
114 examined was the error; declaring it at 67 would be the same error with a
better number.
