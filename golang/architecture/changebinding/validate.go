// SPDX-License-Identifier: AGPL-3.0-only

package changebinding

// ValidateBinding is the PURE single-record validator. Given a parsed binding, the
// expected subject, and an EXPLICITLY-supplied provenance-verification result, it returns
// the typed validity. It reads nothing ambient — no Git, filesystem, branch, commit
// message, environment, event file, network, task discovery, or --task-dir. Identity
// comparison is exact: no trimming, case folding, prefix/basename matching, or hash
// shortening.
//
// Precedence (each check fails closed to its own typed class):
//  1. unsupported version;
//  2. malformed (defensive shape re-check, in case the binding was not parsed);
//  3. publication invalid (self-excluding digest does not verify);
//  4. repository mismatch;
//  5. stale head;
//  6. change-range mismatch (base / change id / provider);
//  7. task mismatch (only when an expected task is supplied);
//  8. unverifiable provenance (anything other than positively verified);
//  9. authoritative.
//
// A valid digest never compensates for unverifiable provenance, and verified provenance
// never compensates for a digest mismatch — the digest is checked before provenance.
func ValidateBinding(b ChangeTaskBinding, expected ExpectedSubject, prov ProvenanceVerification) BindingResult {
	if b.SchemaVersion != SchemaVersion {
		return result(BindingUnsupportedVersion, "schema_version is not "+SchemaVersion)
	}
	if v := validateShape(b); v != "" {
		return result(v, "binding is structurally malformed")
	}

	// Publication integrity: recompute the self-excluding digest and require an exact match.
	want, err := BindingDigest(b)
	if err != nil || b.DigestSHA256 != want {
		return result(BindingPublicationInvalid, "binding digest does not match its canonical content")
	}

	// Exact subject matching.
	if b.Repository != expected.Repository {
		return result(BindingRepositoryMismatch, "binding repository is not the evaluated repository")
	}
	if b.Change.HeadSHA != expected.Change.HeadSHA {
		return result(BindingStaleHead, "binding head SHA does not match the evaluated head")
	}
	if b.Change.BaseSHA != expected.Change.BaseSHA || b.Change.ID != expected.Change.ID || b.Change.Provider != expected.Change.Provider {
		return result(BindingChangeRangeMismatch, "binding change range/id does not match the evaluated change")
	}
	if expected.Task != nil && b.Task != *expected.Task {
		return result(BindingTaskMismatch, "binding names a different task than the selected task")
	}

	// Authority: a populated issuer is never sufficient — only positive verification.
	if prov != ProvenanceVerified {
		return result(BindingUnverifiableProvenance, "binding provenance authority is not positively verified")
	}
	return result(BindingAuthoritative, "")
}

// ValidateBindingSet is the set-level validator. It never resolves a contradiction by
// selecting a record (first/newest/sorted/authoritative-looking/--task-dir-matching):
// zero publications is absent, exactly one is validated, and MORE than one is
// contradictory and fails closed. A single valid record can never outvote a second
// record — one authoritative binding is required.
func ValidateBindingSet(bindings []ChangeTaskBinding, expected ExpectedSubject, verifier ProvenanceVerifier) BindingResult {
	switch len(bindings) {
	case 0:
		return result(BindingAbsent, "no change-to-task binding supplied")
	case 1:
		prov := ProvenanceInvalid
		if verifier != nil {
			prov = verifier.VerifyProvenance(bindings[0])
		}
		return ValidateBinding(bindings[0], expected, prov)
	default:
		return result(BindingContradictory, "more than one publication for one evaluated change; exactly one authoritative binding is required and none is ever selected from a set")
	}
}
