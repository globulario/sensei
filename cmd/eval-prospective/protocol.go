// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// protocolVersion is one frozen reference protocol and the world it binds.
//
// The binding lives on the protocol rather than in a package-level map, for
// the same reason eval-arms keeps it there: a global world table would mean a
// checkout satisfying one protocol's world automatically satisfies another's,
// which is exactly the compatibility alias a versioned protocol forbids.
type protocolVersion struct {
	ID           string
	Path         string
	DigestSHA256 string

	World  string
	Domain string
	Remote string
	// Revision pins the world to an exact commit. Section 3.2 of the design
	// makes the revision part of the experiment's identity, so a different
	// commit of the right repository is a different experiment, not a rerun.
	Revision string
}

// prospectiveV1 is the frozen protocol this binary serves.
//
// The digest is not decoration. The protocol bounds what may be measured and
// how; a binary that ran against an edited protocol while reporting the
// protocol's name would produce numbers nobody could trace to a rule.
var prospectiveV1 = protocolVersion{
	ID:           "prospective-recall-protocol-v1",
	Path:         "docs/evaluation/prospective-recall-protocol-v1.md",
	DigestSHA256: "ade91a42c8c0c421d0e6ba84ce2a547712c248b7cfef491ecc6ff8b12ff90d8d",
	World:        "world1_sensei_self",
	Domain:       "github.com/globulario/sensei",
	Remote:       "github.com/globulario/sensei",
}

var registeredProtocols = []protocolVersion{prospectiveV1}

var defaultProtocol = prospectiveV1

func protocolByID(id string) (protocolVersion, error) {
	for _, p := range registeredProtocols {
		if p.ID == id {
			return p, nil
		}
	}
	known := make([]string, 0, len(registeredProtocols))
	for _, p := range registeredProtocols {
		known = append(known, p.ID)
	}
	return protocolVersion{}, fmt.Errorf("unknown protocol %q: this binary knows %s — an unregistered protocol cannot be enforced, so a run under it would claim a compliance nobody checked",
		id, strings.Join(known, ", "))
}

// verify reads the protocol document and refuses a digest that does not match.
//
// It fails closed rather than warning. A warning here is a run that proceeds
// against an edited ruler and reports the unedited ruler's name, and the
// mismatch would surface only when somebody tried to reproduce the score.
func (p protocolVersion) verify(repoRoot string) error {
	full := filepath.Join(repoRoot, filepath.FromSlash(p.Path))
	body, err := os.ReadFile(full)
	if err != nil {
		return fmt.Errorf("protocol %s: %w — the document it binds must be present in the checkout being measured", p.ID, err)
	}
	sum := sha256.Sum256(body)
	got := hex.EncodeToString(sum[:])
	if got != p.DigestSHA256 {
		return fmt.Errorf("protocol %s digest mismatch: %s names %s but the checkout holds %s — the ruler changed, so any score under this name would be uninterpretable",
			p.ID, p.Path, p.DigestSHA256, got)
	}
	return nil
}
