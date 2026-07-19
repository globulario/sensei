// SPDX-License-Identifier: Apache-2.0

// This file isolates all runtime-target-specific validation so that core
// Sensei stays platform-neutral: a repository with no runtime target never
// depends on anything in this file to reach a valid evidence state.

package evidencereceipt

import (
	"strings"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// ProfileRequiresRuntime reports whether a profile's receipts must carry a
// runtime target: either the profile observes runtime evidence, or it declares
// a runtime target kind.
func ProfileRequiresRuntime(p Profile) bool {
	return p.EvidenceKind == closureprotocol.EvidenceRuntime || strings.TrimSpace(p.RuntimeTargetKind) != ""
}

// checkRuntime validates the runtime binding of a receipt against its profile
// and an optional expected runtime target. It returns a reason code and false
// on the first violation. When the profile does not require runtime evidence,
// any runtime target on the receipt is irrelevant and the check passes.
func checkRuntime(profile Profile, expected *RuntimeTarget, receipt Receipt) (string, bool) {
	if !ProfileRequiresRuntime(profile) {
		return "", true
	}
	if receipt.RuntimeTarget == nil {
		return ReasonRuntimeMissing, false
	}
	if expected != nil && !runtimeTargetMatches(*expected, *receipt.RuntimeTarget) {
		return ReasonRuntimeTargetMismatch, false
	}
	return "", true
}

// runtimeTargetMatches reports whether an observed runtime target satisfies the
// expected one. Only fields the expected target pins are compared; a receipt
// observed against the wrong cluster (deployment) or the wrong configuration
// generation fails.
func runtimeTargetMatches(expected, actual RuntimeTarget) bool {
	pin := func(want, got string) bool {
		want = strings.TrimSpace(want)
		return want == "" || want == strings.TrimSpace(got)
	}
	if !pin(expected.Platform, actual.Platform) {
		return false
	}
	if !pin(expected.EnvironmentID, actual.EnvironmentID) {
		return false
	}
	if !pin(expected.DeploymentID, actual.DeploymentID) {
		return false
	}
	if !pin(expected.ReleaseRevision, actual.ReleaseRevision) {
		return false
	}
	if !pin(expected.ConfigurationGeneration, actual.ConfigurationGeneration) {
		return false
	}
	return true
}
