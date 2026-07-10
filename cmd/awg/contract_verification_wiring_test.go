// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"

	"github.com/globulario/awareness-graph/golang/rdf"
)

// nt builds one N-Triples line. IRI objects pass isIRI=true.
func cvwTriple(s, p, o string, isIRI bool) string {
	if isIRI {
		return "<" + s + "> <" + p + "> <" + o + "> .\n"
	}
	return "<" + s + "> <" + p + "> \"" + o + "\" .\n"
}

func cvwIRI(id string) string { return rdf.AwNS + "contract/" + id }

func TestVerificationWiring_HomeUnbackedFails(t *testing.T) {
	var b strings.Builder
	c := cvwIRI("contract.home.unbacked")
	b.WriteString(cvwTriple(c, rdf.PropType, rdf.ClassContract, true))
	b.WriteString(cvwTriple(c, rdf.PropRequiresVerification, "some behavior", false))
	// no anchor, no repo tag → home-domain, unverified → FAIL

	res := checkContractVerificationWiring([]byte(b.String()))
	if res.level != auditFAIL {
		t.Errorf("unbacked home-domain requiresVerification contract must FAIL, got level %v", res.level)
	}
}

func TestVerificationWiring_HomeBackedPasses(t *testing.T) {
	var b strings.Builder
	c := cvwIRI("contract.home.backed")
	b.WriteString(cvwTriple(c, rdf.PropType, rdf.ClassContract, true))
	b.WriteString(cvwTriple(c, rdf.PropRequiresVerification, "some behavior", false))
	b.WriteString(cvwTriple(c, rdf.PropConstrainedByInvariant, rdf.AwNS+"invariant/x", true))

	res := checkContractVerificationWiring([]byte(b.String()))
	if res.level != auditPASS {
		t.Errorf("backed contract must PASS, got %v: %s", res.level, res.summary)
	}
}

func TestVerificationWiring_RepoTaggedFixtureExcluded(t *testing.T) {
	var b strings.Builder
	c := cvwIRI("contract.greeting.wrapper_failure")
	b.WriteString(cvwTriple(c, rdf.PropType, rdf.ClassContract, true))
	b.WriteString(cvwTriple(c, rdf.PropRequiresVerification, "Greeting return path", false))
	b.WriteString(cvwTriple(c, rdf.PropRepo, "github.com/example/tinyrepo", false))
	// no anchor — but it's a repo-tagged fixture, so it must NOT be flagged.

	res := checkContractVerificationWiring([]byte(b.String()))
	if res.level != auditPASS {
		t.Errorf("repo-tagged fixture must be excluded (PASS), got %v: %s\ndetails=%v", res.level, res.summary, res.details)
	}
	for _, d := range res.details {
		if strings.Contains(d, "greeting") {
			t.Errorf("benchmark fixture leaked into the gate: %s", d)
		}
	}
}

func TestVerificationWiring_MixedOnlyFlagsHomeUnbacked(t *testing.T) {
	var b strings.Builder
	home := cvwIRI("contract.home.unbacked")
	b.WriteString(cvwTriple(home, rdf.PropType, rdf.ClassContract, true))
	b.WriteString(cvwTriple(home, rdf.PropRequiresVerification, "x", false))
	fix := cvwIRI("contract.greeting.wrapper_failure")
	b.WriteString(cvwTriple(fix, rdf.PropType, rdf.ClassContract, true))
	b.WriteString(cvwTriple(fix, rdf.PropRequiresVerification, "y", false))
	b.WriteString(cvwTriple(fix, rdf.PropRepo, "github.com/example/tinyrepo", false))

	res := checkContractVerificationWiring([]byte(b.String()))
	if res.level != auditFAIL || len(res.details) != 1 || !strings.Contains(res.details[0], "home.unbacked") {
		t.Errorf("expected FAIL flagging only the home unbacked contract; got level=%v details=%v", res.level, res.details)
	}
}
