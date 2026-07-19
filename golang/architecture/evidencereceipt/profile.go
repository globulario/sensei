// SPDX-License-Identifier: Apache-2.0

package evidencereceipt

import (
	"errors"
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// ValidateProfile validates an evidence profile. It first applies the frozen
// schema-level checks, then — for an *active* profile (status valid) — enforces
// the stricter closure requirements: an active profile must name an owner
// service, a legal observation path, a parseable freshness window, a trust
// level, and a governed target (authority domain, invariant, contract, or proof
// obligation). A runtime-kind profile must additionally name its runtime target
// kind.
//
// An evidence *requirement* (this profile) is not an evidence *receipt*; this
// function never asserts that any receipt exists.
func ValidateProfile(p Profile) error {
	if err := closureprotocol.ValidateEvidenceProfile(p); err != nil {
		return err
	}
	if p.Status != closureprotocol.ReceiptValid {
		// Inactive / non-authoritative profile: base shape is sufficient.
		return nil
	}
	if strings.TrimSpace(p.Owner) == "" {
		return errors.New("active profile requires an owner service")
	}
	if strings.TrimSpace(p.LegalObservationPath) == "" {
		return errors.New("active profile requires a legal observation path")
	}
	if strings.TrimSpace(p.Trust) == "" {
		return errors.New("active profile requires a trust level")
	}
	if strings.TrimSpace(p.GovernedTarget) == "" {
		return errors.New("active profile requires a governed target (authority domain, invariant, contract, or proof obligation)")
	}
	if strings.TrimSpace(p.Freshness) == "" {
		return errors.New("active profile requires a freshness window")
	}
	if _, err := ParseFreshness(p.Freshness); err != nil {
		return fmt.Errorf("active profile freshness: %w", err)
	}
	if p.EvidenceKind == closureprotocol.EvidenceRuntime && strings.TrimSpace(p.RuntimeTargetKind) == "" {
		return errors.New("active runtime profile requires a runtime_target_kind")
	}
	return nil
}

// FreshnessWindow returns the profile's canonical freshness window.
func FreshnessWindowOf(p Profile) (FreshnessWindow, error) {
	return ParseFreshness(p.Freshness)
}
