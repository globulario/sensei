# Component granularity — per-package identity for Go

## Status

Accepted 2026-08-06. This is a graph migration: it re-identifies generated
architectural entities. Read the accounting below before regenerating.

## The problem

`componentForDirWithRoot` rolled every directory up to two segments under a
known source root. For Go that made **77 distinct packages under
`golang/architecture/` a single component**, `component.golang.architecture`.
Every file in that tree resolved to the same node, so the component layer
carried no discriminating information: a briefing on `workspacecontract` and a
briefing on `testobligation` named the same component, and "which component owns
this file" had no useful answer anywhere in the tree.

## Decision

Component granularity is a **per-language** property, because what counts as one
owned unit differs by language:

| granularity | rule | languages |
|---|---|---|
| `granularityPackageDir` | the directory holding the sources IS the package | go |
| `granularityManifestRoot` | the unit is the manifest directory; sources sit beneath it | rust, typescript, python, unknown |

Go gets directory-is-package because that is the language definition. Rust and
TypeScript keep the two-segment rollup because their sources live under a `src/`
inside the owned unit — applying Go's rule there would split `crates/alpha` into
`crates/alpha/src` and invent a component per `src/` folder. Unknown languages
get the conservative manifest-root rule, i.e. the behaviour everything had
before this change.

This was not theoretical: the first implementation applied Go's rule to every
language and the Rust and TypeScript scan tests failed immediately.

## Identity accounting (Go import graph)

```
components before:  36
components after:  126

survived unchanged:  34
vanished:             2   component.github_app, component.scratchpad
newly created:       92
```

The two vanished ids covered `github_app/` and `scratchpad/`, sample and scratch
trees. Both were verified to have **zero references** anywhere outside the
generated import graph and to be unresolvable in the live graph, so no
compatibility shim is warranted — this is deliberate invalidation, not breakage.

The 34 survivors are directories that were already their own package
(`golang/server`, `cmd/awg`, `golang/rdf`, …); their ids are byte-identical
before and after, so every existing reference to them still resolves.

Edge integrity after regeneration: **0 dangling edge targets** across 126
components.

TypeScript and Python import graphs regenerate byte-identical, confirming the
language split contains the change to Go.

## Compatibility rule

No aliasing, no redirect nodes. A component id is a structural fact derived from
layout; carrying a stale id forward as an alias would mean the graph asserts a
unit of ownership that no longer exists — the same shape as an anchor pointing
at a test nobody wrote. Consumers of the two invalidated ids: none.

## Callers

`ComponentForFile` now REQUIRES a language argument. It previously guessed one
rollup for everybody, and a caller that guesses wrong silently attributes facts
to a component id that does not exist. Making it explicit surfaced a Go caller
(`gosemantics`) and three more (`howextract` ×2, `grpcwebscan`) that the
original survey had missed — `grpcwebscan` is TypeScript and would have been
mis-attributed by a Go-only default.

## Not decided here

Whether authored components should mirror generated per-package identity, and
whether `golang/architecture/` should keep a coarse umbrella component alongside
its 77 packages. Both are authoring questions, not extraction questions.
