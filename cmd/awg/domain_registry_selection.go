// SPDX-License-Identifier: AGPL-3.0-only

package main

import "strings"

// domainRegistrySelection is THE ONE domain registry a governed publication consults and writes.
//
// It exists because a build transaction read two registries. The law was already written down,
// in the doc comment of the path resolver this type replaces: "Extracted so the pre-mutation
// store-ownership check and the pre-mutation admission check cannot end up reading two different
// registries, which would let one of them vouch for a world the other never saw." Activation was
// never counted as one of the operations that must agree -- and it is the only one that WRITES.
//
// So with `--domain-registry /custom/domains.yaml`, ownership and admission consulted the custom
// registry while activation moved the ACTIVE pointer in ~/.sensei/domains.yaml: the selected
// registry was left stale, and every reader resolving through it refused the graph that had just
// been published for it. Raised by blind review on multiple heads of #361.
//
// WHY A TYPE AND NOT A STRING. The defect was a string operand at one site being replaceable by
// another string expression -- DefaultDomainRegistryPath() reads exactly like a resolved path.
// Functions that consume a registry for a governed publication now take this type, so the
// default cannot be substituted at one stage without saying so out loud.
//
// The law it carries:
//
//	one transaction -> one resolved registry identity -> every ownership, declaration and
//	activation operation consumes that identity
type domainRegistrySelection struct {
	path string
	// overridden records that an operator named the registry at the point of use, which is a
	// different fact from the resolved path happening to differ from the default.
	overridden bool
}

// selectDomainRegistry resolves the registry once, from the flag when given and otherwise from
// the operator's default. This is the only constructor: a selection cannot be assembled from a
// bare path elsewhere and then disagree with the one the transaction resolved.
func selectDomainRegistry(flagValue string) domainRegistrySelection {
	if p := strings.TrimSpace(flagValue); p != "" {
		return domainRegistrySelection{path: p, overridden: true}
	}
	return domainRegistrySelection{path: DefaultDomainRegistryPath()}
}

// Path is the resolved registry file. Named rather than exported as a field so every crossing
// back to a plain string is visible in a grep -- the same reason nodeDomainKind.String() is a
// method.
func (s domainRegistrySelection) Path() string { return s.path }

// Overridden reports that an operator named this registry, so a caller can say so in a report
// without re-deriving the comparison.
func (s domainRegistrySelection) Overridden() bool { return s.overridden }
