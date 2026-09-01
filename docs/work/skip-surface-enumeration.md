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

## Not repaired here, and why

The remaining 25 Class B sites are listed in the commit that adds this file.
They are not repaired in this PR because they are not one change: they span
`golang/architecture/*` (fixture-carries-no-X conditions),
`golang/server/packaging_test.go` (embedded seed markers),
`cmd/prospective-label`, and `cmd/eval-arms` — different owners, different
questions about whether the absent thing is tracked or generated, and each
needs the surrounding test read rather than the message classified.

**Closure is therefore NOT declared.** The enumeration exists so the remainder
is a known quantity rather than an assumption, which is the whole point of the
rule.
