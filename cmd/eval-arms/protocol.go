// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"sort"
	"strings"
)

// protocolVersion is one frozen reference protocol and the worlds it binds.
//
// The bindings live HERE, on the protocol, rather than in package-level maps.
// While there was only one protocol the difference was invisible; with two, a
// global world table would mean a checkout that satisfies one protocol's World
// 3 automatically satisfies the other's — which is exactly the compatibility
// alias the v2 amendment forbids. A world binding is a claim a specific
// protocol makes, so it is stored as one.
type protocolVersion struct {
	ID           string
	Path         string
	DigestSHA256 string

	// Worlds are the worlds this protocol consumes. A sample drawn under this
	// protocol while any of them is missing claims a compliance it does not
	// have.
	Worlds []string

	// Domains is the repository domain each world must report.
	Domains map[string]string

	// Remotes is the upstream repository each world's checkout must resolve
	// to. An entry present and empty means "synthetic, no checkout to verify";
	// an entry ABSENT for a world this protocol names means the identity is
	// unregistered and the world fails closed.
	Remotes map[string]string

	// Revisions pins a world to an exact upstream revision where the protocol
	// states one. Empty means the protocol pins the repository but not the
	// commit, and any revision of the right repository is accepted.
	Revisions map[string]string
}

// worldMutantSuite is the protocol's synthetic fourth world. It is not a
// checkout, which is why it was absent from the world list for as long as that
// list meant "things with a --world flag" rather than "things the protocol
// consumes".
const worldMutantSuite = "world4_mutant_suite"

const (
	worldSenseiSelf = "world1_sensei_self"
	worldGlobular   = "world2_globular"
	// worldIndependentCalibration is World 3 under BOTH protocols. The name is
	// shared on purpose: v1 and v2 disagree about which repository it is, and
	// keeping one name is what makes that disagreement checkable rather than
	// letting two differently-named worlds quietly coexist.
	worldIndependentCalibration = "world3_independent_calibration"
)

// protocolV1 is the original frozen protocol. It is registered, and its
// bindings are enforced, precisely so a v1 run remains reproducible and a
// non-SQLite checkout cannot be recorded under it. v1 is historical evidence,
// not a deprecated alias.
var protocolV1 = protocolVersion{
	ID:           "phase10-reference-protocol-v1",
	Path:         "docs/evaluation/phase10-reference-protocol-v1.md",
	DigestSHA256: "e686245ebfeb0885113046d9dfbefcfbab43c2457b1850fa4058ac1ac2f2288c",
	Worlds:       []string{worldSenseiSelf, worldGlobular, worldIndependentCalibration, worldMutantSuite},
	Domains: map[string]string{
		worldSenseiSelf:             "github.com/globulario/sensei",
		worldGlobular:               "github.com/globulario/Globular",
		worldIndependentCalibration: "sqlite.org/sqlite",
	},
	Remotes: map[string]string{
		worldSenseiSelf: "github.com/globulario/sensei",
		worldGlobular:   "github.com/globulario/Globular",
		// v1 names SQLite. Registered so a v1 run can still be attempted and
		// so a gin checkout is REFUSED under v1 rather than silently accepted.
		worldIndependentCalibration: "github.com/sqlite/sqlite",
		worldMutantSuite:            "",
	},
	Revisions: map[string]string{},
}

// protocolV2 amends World 3 only. Every other rule is carried forward from v1
// unchanged, which is why nothing else in this struct differs.
var protocolV2 = protocolVersion{
	ID:           "phase10-reference-protocol-v2",
	Path:         "docs/evaluation/phase10-reference-protocol-v2.md",
	DigestSHA256: "6afaf6ec1cebc73c72df43cb8c6e4842e2ea46356cc64ced61252a8ad620929d",
	Worlds:       []string{worldSenseiSelf, worldGlobular, worldIndependentCalibration, worldMutantSuite},
	Domains: map[string]string{
		worldSenseiSelf:             "github.com/globulario/sensei",
		worldGlobular:               "github.com/globulario/Globular",
		worldIndependentCalibration: "github.com/gin-gonic/gin",
	},
	Remotes: map[string]string{
		worldSenseiSelf:             "github.com/globulario/sensei",
		worldGlobular:               "github.com/globulario/Globular",
		worldIndependentCalibration: "github.com/gin-gonic/gin",
		worldMutantSuite:            "",
	},
	Revisions: map[string]string{
		// v2 pins the commit, not just the repository: section 3 states the
		// revision as part of the protocol's identity, so gin at a different
		// commit is a different experiment.
		worldIndependentCalibration: "34dac209ffb6ef85cc78c5d217bbb7ad001d68fd",
	},
}

// registeredProtocols are the protocols this binary knows by identity. Order
// is irrelevant; lookup is by ID and by digest.
var registeredProtocols = []protocolVersion{protocolV1, protocolV2}

// defaultProtocol is what a run uses when the operator names none. v2, because
// it is the current protocol; v1 stays registered and enforceable but is no
// longer the default a bare invocation obeys.
var defaultProtocol = protocolV2

// protocolByID returns the registered protocol with this identity.
func protocolByID(id string) (protocolVersion, bool) {
	for _, p := range registeredProtocols {
		if p.ID == id {
			return p, true
		}
	}
	return protocolVersion{}, false
}

// protocolByDigest returns the registered protocol whose document hashes to
// this digest. Identity by CONTENT, so a path spelled differently, a symlink,
// or a byte-identical copy at an unrelated location all resolve to the same
// protocol.
func protocolByDigest(digest string) (protocolVersion, bool) {
	if strings.TrimSpace(digest) == "" {
		return protocolVersion{}, false
	}
	for _, p := range registeredProtocols {
		if p.DigestSHA256 == digest {
			return p, true
		}
	}
	return protocolVersion{}, false
}

// resolveProtocol decides which protocol a (document, identity) pair names,
// and refuses any pair that does not agree.
//
// This is the guard against the alias the amendment forbids. Three ways a pair
// can lie, all refused here:
//
//   - a registered document under a different identity — recording one
//     protocol's digest under another's name, which is how a gin checkout
//     would come to be filed as v1 SQLite;
//   - a registered identity over a different document — the same lie from the
//     other side;
//   - a registered identity over an unreadable document — an identity with
//     nothing behind it.
//
// A pair naming no registered protocol is an operator-bound protocol: allowed,
// and returned as unregistered so the caller applies no built-in world
// expectations to it.
func resolveProtocol(path, id, digest string, readErr error) (protocolVersion, bool, error) {
	byID, idKnown := protocolByID(id)

	if readErr != nil {
		if idKnown {
			return protocolVersion{}, false, fmt.Errorf(
				"--protocol-id names the registered protocol %s but --protocol-file %s could not be read (%v); an identity with no document behind it cannot be recorded",
				id, path, readErr)
		}
		return protocolVersion{}, false, nil
	}

	byDigest, digestKnown := protocolByDigest(digest)

	switch {
	case digestKnown && idKnown:
		if byDigest.ID != byID.ID {
			return protocolVersion{}, false, fmt.Errorf(
				"--protocol-file %s is the %s document but --protocol-id says %s; the manifest would record one protocol's digest under another protocol's identity",
				path, byDigest.ID, byID.ID)
		}
		return byDigest, true, nil
	case digestKnown && !idKnown:
		return protocolVersion{}, false, fmt.Errorf(
			"--protocol-file %s is the registered %s document but --protocol-id says %q; a registered protocol may not be re-labelled",
			path, byDigest.ID, id)
	case !digestKnown && idKnown:
		return protocolVersion{}, false, fmt.Errorf(
			"--protocol-id names the registered protocol %s but --protocol-file %s is a different document; a registered identity may not be attached to other bytes",
			id, path)
	default:
		// Neither is registered: an operator-bound protocol, which is theirs
		// to define. No built-in world expectations apply.
		return protocolVersion{}, false, nil
	}
}

// missingWorlds names what this protocol consumes that the run did not produce.
//
// A world present under the right name but bound to another repository is
// reported separately from one that did not run: "you did not run it" and "you
// ran something else and called it that" need different corrections.
func (p protocolVersion) missingWorlds(ranDomains map[string]string) []string {
	var missing []string
	for _, name := range p.Worlds {
		domain, ok := ranDomains[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		if want := p.Domains[name]; want != "" && domain != want {
			missing = append(missing, fmt.Sprintf("%s (bound to %s, %s names %s)", name, domain, p.ID, want))
		}
	}
	sort.Strings(missing)
	return missing
}
