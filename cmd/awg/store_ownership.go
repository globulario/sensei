// SPDX-License-Identifier: AGPL-3.0-only

// store_ownership.go enforces one invariant: a governed production Oxigraph store belongs
// to exactly ONE Sensei domain.
//
// Forced by the Phase 7 measurement rather than chosen on taste. A marker's digest is
// computed over the WHOLE store, so publishing domain B recomputes it and domain A's
// ACTIVE pointer — which names the old whole-store digest — goes stale the instant B
// lands. Measured: one reader process, one moment, one store, and domain sensei-code
// agreed while domain sensei refused, the only difference being whose pointer the
// activation had recorded.
//
// Per-domain subgraph digests would also resolve the ambiguity and are deliberately not
// attempted here. This is the simpler invariant, and it is enforced in the owner that
// already exists — the domain registry — rather than in a parallel registry of stores.
//
// The second hazard it closes is the built-in default. netcfg's default store is
// http://localhost:7878, which on this installation is the LEGACY awg store
// (~/.local/share/awg/oxigraph, 237,049 triples, awg-oxigraph.service) and belongs to no
// governed domain. Neither governed domain uses it. So a command run without a project
// config publishes into the legacy store purely because it is the default and reachable —
// selection by liveness, which is what law 2 exists to reject.
package main

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// normalizeStoreIdentity reduces a store URL to its IDENTITY, so two spellings of one
// store cannot pass for two stores.
//
// Scheme and host are case-insensitive per RFC 3986 and are lowered; the path and query
// are left exactly as written, because "/store?default" and "/store?graph=x" are different
// targets and normalising them together would merge things that are genuinely distinct.
// An unparseable URL is returned trimmed and lowercased rather than dropped: a value this
// cannot read must still be comparable, or a malformed entry would silently own nothing.
// normalizeStoreIdentity is the identity two declarations are compared BY, so it must
// canonicalize exactly as far as publication does and no further.
//
// It used to lowercase scheme and host and keep the path verbatim, while normalizeStoreURL
// -- the form actually written to -- fills an empty path with /store, rewrites a /query
// suffix to /store, and defaults the query to `default`. So `http://host:7881` and
// `http://host:7881/store?default` named ONE store and compared as TWO owners, and a second
// domain spelling the endpoint the other way evaded both registry validation and the
// foreign-owner check before overwriting the first domain's store (review finding
// store_ownership.go:51).
//
// The fix is to ask the existing owner of store-URL semantics rather than to add a second
// normalization vocabulary: whatever normalizeStoreURL says publication will address IS the
// store's identity. A value it cannot parse as a store endpoint keeps the old lowercased
// form, because a declaration this cannot interpret must still compare equal to itself.
func normalizeStoreIdentity(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if canonical, err := normalizeStoreURL(s); err == nil {
		s = canonical
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return strings.ToLower(s)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	return u.String()
}

// storeOwnershipError names both domains, because "this store is taken" without saying by
// whom is not something an operator can act on.
type storeOwnershipError struct {
	Store     string
	Owner     string
	Requested string
	// Declared is the store the requesting domain does declare, when it declares one.
	Declared string
}

func (e *storeOwnershipError) Error() string {
	if e.Owner != "" {
		return fmt.Sprintf("refusing to publish %s into a store another domain owns, so nothing has been written.\n"+
			"  store            %s\n"+
			"  owned by domain  %s\n"+
			"  requested by     %s\n\n"+
			"A graph marker certifies the WHOLE store, so publishing here would recompute the digest "+
			"%s's ACTIVE pointer names and strand every reader of that domain. One governed store "+
			"belongs to one domain. Publish into %s's own store, or give each domain its own store.",
			e.Requested, e.Store, e.Owner, e.Requested, e.Owner, e.Requested)
	}
	return fmt.Sprintf("refusing to publish %s into a store it does not declare, so nothing has been written.\n"+
		"  target store     %s\n"+
		"  this domain declares  %s\n\n"+
		"No domain owns the target, so this is not a collision -- it is a publication going somewhere "+
		"the registry did not intend, which is how the built-in default (:7878, the legacy store) "+
		"receives graphs nobody meant to send it. Publish into the declared store, or name the target "+
		"explicitly with --store-url if you mean it.",
		e.Requested, e.Store, e.Declared)
}

// verifyStoreOwnership answers whether domain may publish into target.
//
// Three rules, and the first is absolute:
//
//  1. the target is declared by ANOTHER domain -> refuse, override or not. An operator
//     naming a store does not make stranding a neighbour's pointer acceptable, and this is
//     the one case where the damage lands on a domain nobody in this command mentioned.
//  2. this domain declares a store and the target is a different one -> refuse UNLESS the
//     operator named it. That is the accident the netcfg default causes.
//  3. this domain declares nothing -> inert, exactly as before. Disposable stores are
//     precisely the stores no domain claims, so experiments keep working and the opt-out
//     lives in the registry rather than in a flag that disables the check.
func verifyStoreOwnership(reg *DomainRegistry, domain, target string, overridden bool) error {
	if reg == nil {
		return nil
	}
	want := normalizeStoreIdentity(target)
	if want == "" {
		return nil
	}
	requested := strings.TrimSpace(domain)
	for name, rd := range reg.Domains {
		if name == requested {
			continue
		}
		if normalizeStoreIdentity(rd.StoreURL) == want {
			return &storeOwnershipError{Store: target, Owner: name, Requested: requested}
		}
	}
	declared := ""
	if rd, ok := reg.Domains[requested]; ok {
		declared = strings.TrimSpace(rd.StoreURL)
	}
	if declared == "" || overridden {
		return nil
	}
	if normalizeStoreIdentity(declared) == want {
		return nil
	}
	return &storeOwnershipError{Store: target, Requested: requested, Declared: declared}
}

// validateStoreOwnership refuses a registry that binds one store to two governed domains.
//
// Checked at LOAD so every command inherits it. A registry in this state has no correct
// reading: whichever domain publishes last owns the digest, and the other's pointer is
// stale from that moment — so there is nothing for a caller to do with it except refuse.
func (r *DomainRegistry) validateStoreOwnership() error {
	if r == nil {
		return nil
	}
	byStore := map[string][]string{}
	for name, rd := range r.Domains {
		if id := normalizeStoreIdentity(rd.StoreURL); id != "" {
			byStore[id] = append(byStore[id], name)
		}
	}
	var problems []string
	for store, domains := range byStore {
		if len(domains) < 2 {
			continue
		}
		sort.Strings(domains)
		problems = append(problems, fmt.Sprintf("store %s is declared by %s", store, strings.Join(domains, " and ")))
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("a governed store belongs to exactly one domain, and this registry says otherwise:\n  %s\n\n"+
		"A graph marker certifies the whole store, so two domains sharing one store means whichever "+
		"publishes last owns the digest and the other's ACTIVE pointer is stale from that moment. "+
		"Give each domain its own store.", strings.Join(problems, "\n  "))
}

// publishedDomain is the domain a build is publishing FOR.
//
// The scoped publication path is selected by --repo and passes that value as the domain;
// --domain is a separate flag that the scoped path may leave empty. Reading --domain alone
// gave the ownership check an empty requested domain, and an empty domain matches no
// registry entry, so every declared store looked like another domain's and a publication
// naming no domain was refused for the wrong reason.
//
// One helper so the ownership check and the activation cannot disagree about which domain
// this publication is for.
// publishedGovernedDomain resolves the GOVERNED DOMAIN a publication activates, and refuses anything
// that is not one.
//
// TWO VOCABULARIES SHARE A FLAG NAME, and they are not the same identity:
//
//	--repo    github.com/owner/name   the governed domain whose graph is being published
//	--domain  repo | shared           the default TAGGING KIND for untagged nodes
//
// The previous form returned the first non-empty of the two, so with --repo omitted the tagging kind
// BECAME the governed domain -- and that value reached three authorities: store custody, the recorded
// store mutation intent, and the ACTIVE generation pointer. `build --all --domain shared` would
// register an ACTIVE generation under "shared", a name that is not a governed domain, through the same
// call a real publication uses.
//
// The authority to separate them already existed. repodomain.Validate, reached through
// validateDomain, requires host/path and rejects every tagging kind. Nothing consulted it. So this is
// not a new rule; it is an existing one finally reaching the publication path.
//
// There is deliberately NO fallback to --domain. A tagging kind is never a governed domain, so a
// fallback between the two vocabularies cannot be made safe by validating its result -- the
// substitution is the defect. An empty result means "this publication names no governed domain",
// which activateGeneration already reports rather than silently skipping.
//
// A governed domain passed to --domain is refused by name rather than ignored: it is an operator
// error with a clear remedy, and silently dropping it would publish without the pointer they intended.
func publishedGovernedDomain(repoFlag, domainFlag string) (string, error) {
	if d := strings.TrimSpace(repoFlag); d != "" {
		if err := validateDomain(d); err != nil {
			return "", fmt.Errorf("--repo %q is not a governed domain: %w\n"+
				"  a publication's governed domain is the repository identity it activates, e.g. github.com/owner/name", d, err)
		}
		return d, nil
	}
	if k := strings.TrimSpace(domainFlag); k != "" && validateDomain(k) == nil {
		return "", fmt.Errorf("--domain %q is a governed domain, but --domain names the node tagging kind (repo|shared).\n"+
			"  publish that domain with: --repo %s\n"+
			"  --domain only decides how UNTAGGED nodes are classified, and is never the published identity", k, k)
	}
	return "", nil
}
