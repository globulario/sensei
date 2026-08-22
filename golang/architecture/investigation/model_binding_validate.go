// SPDX-License-Identifier: AGPL-3.0-only

package investigation

import "fmt"

// ValidateModelBinding is the gate that makes ModelStatusResolved a statement
// about observed execution rather than a value a caller can set.
//
// The rule it enforces is asymmetric on purpose:
//
//	resolved     must carry every execution identity
//	not resolved must NOT carry evidence its status cannot have
//
// The second half matters as much as the first. Without it a binding could
// report "unavailable" while carrying an artifact digest, or "disabled" while
// naming a provider — records that describe two different runs at once.
//
// This function decides nothing about whether the model's ANSWER is true. It
// establishes only that Sensei can say exactly what ran, on what request, and
// what came back.
func ValidateModelBinding(m ModelBinding) []string {
	var errs []string
	if !IsValidModelStatus(m.Status) {
		return []string{fmt.Sprintf("invalid model binding status: %q", m.Status)}
	}

	if m.Status == ModelStatusResolved {
		if m.Reason != "" {
			errs = append(errs, "resolved model status must not carry a failure reason")
		}
		if m.ProviderID == "" {
			errs = append(errs, "resolved model status requires a provider id")
		}
		// A provider name without a version cannot distinguish two behaviours
		// behind one label, which is exactly what a replay needs to tell apart.
		if m.ProviderVersion == "" {
			errs = append(errs, "resolved model status requires a provider version")
		}
		if m.ModelName == "" {
			errs = append(errs, "resolved model status requires model_name")
		}
		switch {
		case m.ModelDigestSHA256 != "" && m.ModelDigestAbsence != "":
			errs = append(errs, "model digest and a typed digest absence cannot both be present")
		case m.ModelDigestSHA256 != "":
			if !IsValidSHA256(m.ModelDigestSHA256) {
				errs = append(errs, "resolved model status requires a valid model_digest_sha256")
			}
		case m.ModelDigestAbsence == ModelDigestAbsent:
			// A provider that genuinely has no model digest says so explicitly.
		default:
			errs = append(errs, "resolved model status requires either a model digest or the typed absence "+ModelDigestAbsent)
		}
		if !IsValidSHA256(m.RequestDigestSHA256) {
			errs = append(errs, "resolved model status requires the exact request digest")
		}
		if !IsValidSHA256(m.ArtifactDigestSHA256) {
			errs = append(errs, "resolved model status requires the accepted artifact digest")
		}
		if m.NondeterminismDeclaration == "" {
			errs = append(errs, "resolved model status requires an explicit nondeterminism declaration")
		}
		return errs
	}

	// An absence whose cause is not implied by its own status must say why.
	//
	// disabled and not_requested are self-explanatory: nothing was asked, or
	// the capability is off. Requiring a reason there would add no information
	// and would change the serialized bytes of every deterministic document,
	// which is the one thing #256 must not do. The states that CAN have
	// several causes — could not reach anyone, was told no, broke, came back
	// wrong — must name theirs.
	if m.Reason == "" && m.Status != ModelStatusDisabled && m.Status != ModelStatusNotRequested {
		errs = append(errs, fmt.Sprintf("model status %q requires a typed reason", m.Status))
	}
	// An accepted artifact exists only for resolved. A rejected or unreturned
	// artifact must never be recorded as one.
	if m.ArtifactDigestSHA256 != "" {
		errs = append(errs, fmt.Sprintf("artifact_digest_sha256 must be empty when model status is %q: only an accepted artifact has an identity here", m.Status))
	}
	if m.NondeterminismDeclaration != "" {
		errs = append(errs, fmt.Sprintf("nondeterminism_declaration must be empty when model status is %q", m.Status))
	}

	if !ModelStatusInvoked(m.Status) {
		// Nothing was called, so nothing may look like a call.
		if m.RequestDigestSHA256 != "" {
			errs = append(errs, fmt.Sprintf("request_digest_sha256 must be empty when model status is %q: no request was sent", m.Status))
		}
		if m.Status == ModelStatusDisabled || m.Status == ModelStatusNotRequested {
			if m.ProviderID != "" || m.ProviderVersion != "" {
				errs = append(errs, fmt.Sprintf("provider identity must be empty when model status is %q", m.Status))
			}
			if m.ModelName != "" {
				errs = append(errs, fmt.Sprintf("model_name must be empty when model status is %q", m.Status))
			}
		}
		return errs
	}

	// Invoked but unsuccessful: the request that was actually sent is exactly
	// what makes such a record useful, so it is required rather than optional.
	if !IsValidSHA256(m.RequestDigestSHA256) {
		errs = append(errs, fmt.Sprintf("model status %q means the provider was invoked, so the exact request digest is required", m.Status))
	}
	if m.ProviderID == "" {
		errs = append(errs, fmt.Sprintf("model status %q means a provider was invoked, so its id is required", m.Status))
	}
	return errs
}

// ValidateModelExecutionAgreement fails closed when the document binding and
// the run receipt disagree about the same execution.
//
// RunReceipt carries its own Model plus a separate ModelArtifactDigestSHA256.
// Two fields describing one artifact is exactly the shape that lets a record
// drift into telling two stories, so they are required to agree rather than
// merely coexist.
func ValidateModelExecutionAgreement(binding ModelBinding, receipt RunReceipt) []string {
	var errs []string
	// The WHOLE duplicated binding is compared, not a chosen subset. A partial
	// comparison leaves the unchecked fields free to disagree, and a document
	// altered in one of them can be re-digested and still validate — which is
	// exactly the two-stories problem this function exists to stop. Comparing
	// the struct also means a field added later is covered by default rather
	// than silently exempt.
	if binding != receipt.Model {
		errs = append(errs, "the model binding and the run receipt describe different executions")
		// Name the specific disagreements so the failure is actionable.
		for _, d := range []struct{ field, a, b string }{
			{"status", binding.Status, receipt.Model.Status},
			{"reason", binding.Reason, receipt.Model.Reason},
			{"provider id", binding.ProviderID, receipt.Model.ProviderID},
			{"provider version", binding.ProviderVersion, receipt.Model.ProviderVersion},
			{"model name", binding.ModelName, receipt.Model.ModelName},
			{"model digest", binding.ModelDigestSHA256, receipt.Model.ModelDigestSHA256},
			{"model digest absence", binding.ModelDigestAbsence, receipt.Model.ModelDigestAbsence},
			{"request digest", binding.RequestDigestSHA256, receipt.Model.RequestDigestSHA256},
			{"artifact digest", binding.ArtifactDigestSHA256, receipt.Model.ArtifactDigestSHA256},
			{"nondeterminism declaration", binding.NondeterminismDeclaration, receipt.Model.NondeterminismDeclaration},
		} {
			if d.a != d.b {
				errs = append(errs, fmt.Sprintf("model %s disagrees between binding (%q) and receipt (%q)", d.field, d.a, d.b))
			}
		}
	}
	if receipt.ModelArtifactDigestSHA256 != binding.ArtifactDigestSHA256 {
		errs = append(errs, "receipt model_artifact_digest_sha256 disagrees with the accepted artifact identity in the model binding")
	}
	return errs
}
