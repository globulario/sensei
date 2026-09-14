# `sensei build`: two vocabularies, one word

A repeated blind finding at `cmd_build.go:135`, raised on three of four consecutive heads of
#361. **HOLD.**

## The collision

`sensei build` declares two flags whose help text says exactly what each holds:

| flag | holds | used as |
|---|---|---|
| `--repo` | **the governed domain name**, e.g. `github.com/globulario/services` | `RepositoryDomain`; guarded by `rejectPathLikeBuildDomain`, which exists to refuse a filesystem path here |
| `--domain` | the default **tagging kind** for untagged nodes: `repo` or `shared` | `DefaultDomain` |

Two strings, and the one that is *not* a domain name is the one called `domain`.

Both `verifyStoreOwnership` and `activateGeneration` take a governed domain **name**, and both
were given the tagging kind.

## Reproduced, before repair

Measured against a registry declaring two domains with two different stores, with the governed
domain, the tagging kind and the store URLs all deliberately distinct so no assertion could pass
by coincidence.

**`verifyStoreOwnership` indexes `reg.Domains` by governed domain name**, so the kind produces
one of two wrong answers and never the intended one:

- **a false refusal, in the normal configuration.** Given `"repo"`, it finds no entry for itself,
  then walks the registry, finds the domain that *legitimately* owns the target store, sees that
  its key is not `"repo"` — and reports the rightful owner as a foreign claimant. A correctly
  configured scoped build was refused from its own declared store:

  ```
  sensei build: refusing to publish example.com/acme/owned into a store another domain owns
    store            http://127.0.0.1:47111/store
    owned by domain  example.com/acme/owned
    requested by     example.com/acme/owned
  ```

- **inert, in the case the check exists for.** Where no registered domain declares the target
  store, `reg.Domains["repo"]` does not exist, nothing is declared, nothing is compared.

**`activateGeneration` hands its argument to `recordActiveGeneration`**, which deliberately
leaves an *unregistered* domain alone and returns nil. Given the kind, the real domain's ACTIVE
pointer never moved while the command printed:

```
  ACTIVE generation: 1111…1111 for repo
```

Law 6 activation silently not happening, reported as success.

## Why it survived three review rounds

**A test asserted the defect.** `TestTheOwnershipCheckPrecedesEveryStoreMutation` required the
call to contain `*domain` — *"It must be given the domain and the target store, not a placeholder
that always agrees."* The intent was right and only the operand name was wrong, so any correct
repair **failed the suite** while the defective code stayed green.

And the names could not settle it: `runScopedRepoUpdate`'s parameter is *named* `domain` and is
*passed* `*repo`, so its own `activateGeneration` call was always correct while looking identical
to the two that were wrong. This is why the reviewer's phrasing — "checks `*domain` where `*repo`
is correct" — reads as a contradiction until the flags are read.

## The operand contract, after repair

> `verifyStoreOwnership` and `activateGeneration` take the **governed domain name** — what
> `--repo` supplies. `--domain` is a node tagging kind and is not a domain name.

Enforced by a **type**, not a rename: `nodeDomainKind` is a defined type, so handing it to a
governed domain-name consumer does not compile. Its single crossing back to a plain string is a
named method (`nodeKind.String()`), asserted to occur exactly once.

That seam is strong enough that three separate mutants attempting the swap **could not compile**
— Go's unused-variable rule orphans a flag whichever way the swap is written. One was re-aimed
with explicit scaffolding so the swap itself, rather than the compiler, is what the witness
tests.

The kind's vocabulary is now also read **by membership**: `--domain` accepted any string, so a
governed domain name typed there was silently adopted as the default tag for every untagged node
— the permissive direction this repository has recorded repeatedly, and part of why the two flags
were confusable in the first place. `--domain github.com/org/repo` is now refused, and the
refusal points at `--repo`.

## Witnesses

Contract (`verifyStoreOwnership` is about domain names: allowed, refused-naming-the-owner, and
both swaps rejected); driven through the real `runBuild` for a domain publishing into **its own**
declared store, into **another domain's** store, and across all three `--domain` values proving
the tagging kind has **no influence** on the verdict; activation moving the pointer for a
governed domain name and claiming nothing for an unnamed one; and the kind's vocabulary in both
directions.

The refusal witnesses assert the build **stops** — exit 1 *and* the absence of
`PUBLICATION_REFUSED`, the stage immediately after the guard. A mutant that dropped the
`return 1` and continued past the guard survived a first pass that asserted only the message: a
guard's position and effect are part of its correctness, not just its wording.

**Mutants: 8 of 8 killed** — kind-for-name at both sites, name-for-kind, both operands swapped,
verification bypassed, activation after a failed verification, vocabulary read by exclusion, and
validation skipped. Three DID_NOT_COMPILE attempts are recorded above as evidence for the type
seam rather than counted as kills.

## Not in scope

The served-generation authority family is untouched and its census is unchanged: **20 subjects /
20 owner-resolved / 20 generation-verified / GAP 0**. Two previously raised P2 findings
(`cmd/awareness-mcp/main.go:2536`, `cmd_build.go:747` on `--domain-registry`) remain **open
review evidence**; their absence from a later blind pass does not retire them.

# One transaction, one registry

The second preserved P2 on `cmd_build.go`, raised on multiple heads of #361. **HOLD.**

## The claim, and the law it violates

> With `sensei build --domain-registry /custom/domains.yaml`, publication admission and store
> ownership checks use the custom registry, but `activateGeneration` updates the active pointer in
> `~/.sensei/domains.yaml`. The custom registry is left unmodified with a stale or missing active
> generation, causing readers relying on it to refuse the newly published graph.

The law was **already written down**, in the doc comment of the path resolver this repair replaces:
*"Extracted so the pre-mutation store-ownership check and the pre-mutation admission check cannot
end up reading two different registries, which would let one of them vouch for a world the other
never saw."* Activation was never counted as one of the operations that must agree — and it is the
only one that **writes**.

## Reproduced

Measured on the tree at head `92055029`: within one build transaction, ownership (`:161`) and
admission (`:178`) consumed `buildRegistryPath(*domainRegistry)`, while **both** activation sites
named `DefaultDomainRegistryPath()`.

One refinement the reviewer's line number hides: `:314` is on the `--all` path, where the governed
domain is necessarily empty, so activation there reports *"NOT updated"* and writes nothing. **The
reachable write is `:777`**, inside `runScopedRepoUpdate` — the ordinary `--repo` publication path,
whose signature did not receive the registry at all. Both were repaired; only one could have
written to the wrong file.

## The invariant repaired

> **one transaction → one resolved registry identity → every ownership, declaration and activation
> operation consumes that identity**

`domainRegistrySelection` resolves once, in `runBuild`, and is threaded to the ownership check, the
admission call, `runScopedRepoUpdate`, and both activations. The duplicate path resolver is
deleted, so there is one answer to "which registry". `activateGeneration` takes the **type**, so a
bare `DefaultDomainRegistryPath()` cannot be substituted at one stage — the same compile-time seam
used for the governed-domain operand, for the same reason: the defect was one string expression
that read exactly like another.

The three commands with no `--domain-registry` flag (`rebuild`, `governance`, `serve`) now state
`selectDomainRegistry("")` explicitly rather than naming a default path, so every activation says
which registry it writes.

## Witnesses

The writing operation follows its selection, with two registries disagreeing on both store URL and
ACTIVE generation; a conflicting default is neither read nor written; a selected registry that does
**not** declare the domain is not silently completed from the default (a publication may update an
admission, never create one); no override preserves default behaviour; and — derived from the
source rather than listed — the transaction names `DefaultDomainRegistryPath()` **zero** times,
resolves exactly once, and `runScopedRepoUpdate`'s signature is checked to receive the selection so
a future parameter cannot be dropped silently.

Mutation added one more: replacing `selectDomainRegistry(*domainRegistry)` with
`selectDomainRegistry("")` survived every witness above, because nothing checked that the **flag**
reaches the selection. A driven witness now does, through the ownership check — the cheapest
consumer to observe — with the custom registry declaring a foreign owner and the default registry
empty, plus its opposite.

**Mutants: 7 of 7 killed** — default restored at each activation, selected-for-default swapped at
the final stage only, ownership-reads-selection-while-activation-reads-default (the original
defect), the selection ignored at resolution, the constructor ignoring the operator's path, a
missing declaration completed from the default, and the scoped update resolving its own registry.

## The end-to-end gap, stated

No fixture in this repository drives `runScopedRepoUpdate` through a store, and building an
Oxigraph mock faithful enough for the scoped SPARQL update would mostly prove things about the
mock. So the law is proven at the seam: unit witnesses that the writing operation honours its
selection, plus a derived check that the transaction hands every operation the same one. The
composition is complete; the store round trip is not covered, and that is recorded rather than
implied.
