// SPDX-License-Identifier: AGPL-3.0-only

package packcustody

import (
	"fmt"
	"strings"
)

// Custody names who may publish a corpus document as their own knowledge.
type Custody string

const (
	// RepoAuthored is the repository's own knowledge. It is published into the
	// repository's slice, tagged to its domain. This is the default and the
	// overwhelmingly common case.
	RepoAuthored Custody = "repo_authored"

	// SharedProjection is installed shared knowledge, proven to be a projection
	// of a canonical pack. It is EXCLUDED from the repository's publication.
	//
	// Excluded, not re-tagged as shared: a repository-scoped publication may
	// READ shared knowledge but may not author it. If a repo build emitted the
	// shared triples itself, then every initialized project would still be
	// writing the shared authority slice — the same defect one layer over,
	// with the collision merely renamed. The shared slice has exactly one
	// author: Sensei's own canonical publication.
	//
	// This is also what makes the writability of the provenance records
	// harmless. A corpus that forges an install record to claim its content is
	// a pack projection gains nothing: the only consequence of the claim is
	// that the content is dropped from that repository's own slice. A lie can
	// remove the liar's knowledge; it cannot inject anything into the shared
	// slice, because no repo build writes there.
	SharedProjection Custody = "shared_projection"

	// Refused is a document that declares itself a managed mirror but has no
	// governed provenance proving what it projects. Its custody is genuinely
	// unknown, and both guesses are wrong: attributing it to the repository
	// recreates the two-author collision, and treating it as shared would
	// publish locally diverged content as canonical shared authority. So it is
	// published by nobody, and the reason is recorded.
	Refused Custody = "refused"
)

// Verdict is the derivation's answer plus the evidence it rests on. Basis and
// Reason are operator-facing and travel verbatim into build output — an
// exclusion or a refusal that cannot be explained is indistinguishable from
// knowledge quietly going missing.
type Verdict struct {
	Custody Custody

	// PackDigest is the pack this content was proven to project. Set only for
	// SharedProjection.
	PackDigest string

	// Basis names the governed record that proved the projection. Set only for
	// SharedProjection.
	Basis string

	// Reason explains a refusal. Set only for Refused.
	Reason string
}

// Excluded reports whether a repository-scoped publication must leave this
// document out of its slice. Both non-repo dispositions are excluded; they
// differ in whether the content has a known owner elsewhere.
func (v Verdict) Excluded() bool {
	return v.Custody == SharedProjection || v.Custody == Refused
}

// Describe renders the verdict for build output.
func (v Verdict) Describe() string {
	switch v.Custody {
	case SharedProjection:
		return fmt.Sprintf("shared projection of pack %s — %s", Short(v.PackDigest), v.Basis)
	case Refused:
		return "REFUSED — " + v.Reason
	default:
		return string(RepoAuthored)
	}
}

// Derive answers who may publish this content.
//
// It keys on the content digest and the project's governed provenance records.
// It deliberately does NOT take the document's path: "the file is called
// meta_principles.yaml" and "the file sits in this repository's corpus" are the
// two rules that produced the defect this package exists to remove. Deriving
// from the digest instead is also strictly stronger — moving or renaming a
// proven projection does not launder it into repository-authored knowledge.
//
// The order of the checks below is the whole safety argument:
//
//  1. PROOF FIRST. If a governed record proves this exact content is a
//     projection of pack X, custody is settled, whatever the content says
//     about itself.
//
//  2. SELF-DECLARATION SECOND, AND ONLY TO REFUSE. The generated marker is
//     file content, so it is exactly as forgeable as the aw:repo tag this
//     package refuses to trust. It is therefore trusted in one direction only:
//     it can cost a document its repository authorship, never grant it shared
//     custody. Granting requires proof from step 1. A corpus that adds the
//     marker to a file it genuinely authored only succeeds in having that file
//     refused — it cannot forge shared authority for it.
//
// Note what step 1 does not require: that the content match the CURRENT
// canonical pack. A project whose mirror projects an older pack still has
// settled custody — the pack authored it, just an earlier revision of the
// pack. Staleness is `sensei principle-pack refresh`'s question, not custody's.
// Conflating the two would make every project's custody flip the moment
// upstream published a principle, which is precisely the whole-graph-fact
// coupling that #176 removed one layer up.
func Derive(root string, content []byte) Verdict {
	digest := Digest(content)

	if ir, ok := LoadInstallRecord(root); ok && ir.PackDigest != "" && ir.PackDigest == digest {
		return Verdict{
			Custody:    SharedProjection,
			PackDigest: digest,
			Basis:      "install record (written by `sensei init`) digests to this content",
		}
	}
	for _, rec := range LoadAdoptionRecords(root) {
		if rec.Target.ResultingDigest != "" && rec.Target.ResultingDigest == digest {
			return Verdict{
				Custody:    SharedProjection,
				PackDigest: rec.Source.PackDigest,
				Basis: fmt.Sprintf("adoption receipt %s (authorized: %s) digests to this content",
					Short(rec.Target.ResultingDigest), recordAuthorization(rec)),
			}
		}
	}

	if !declaresManagedMirror(content) {
		return Verdict{Custody: RepoAuthored}
	}

	return Verdict{
		Custody: Refused,
		Reason: fmt.Sprintf(
			"content digests to %s and declares itself a managed mirror (%q), "+
				"but no install record or adoption receipt in this project proves which pack it projects. "+
				"Its author is unknown: publishing it under this repository would mint a second authoring "+
				"domain for shared identities, and treating it as shared would publish local divergence as "+
				"canonical. Reconcile against canonical with `sensei principle-pack refresh` "+
				"(a legacy mirror with no baseline needs --reconcile-legacy, which requires an operator to "+
				"review the divergence first)",
			Short(digest), GeneratedMarker),
	}
}

// DeclaresManagedMirror reports whether the content announces itself as a
// generated projection of the pack.
//
// Exported because closure asks the same question for a different purpose: a
// file that declares itself a projection is not authored by the repository
// holding it, so that repository's domain must not be expected to publish its
// identities. Two definitions of "is this a managed mirror?" would be two
// things to keep in agreement, and their disagreement is precisely how a
// correct publication fails closure.
func DeclaresManagedMirror(content []byte) bool { return declaresManagedMirror(content) }

// declaresManagedMirror reports whether the content announces itself as a
// generated projection. Only the leading comment block is inspected: the marker
// is a header, and a document that merely quotes the sentence somewhere in a
// principle body — this repository's own knowledge does exactly that — is not
// declaring itself generated.
func declaresManagedMirror(content []byte) bool {
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(trimmed, "#") {
			return false // past the header; no marker
		}
		if strings.Contains(trimmed, GeneratedMarker) {
			return true
		}
	}
	return false
}

func recordAuthorization(rec AdoptionRecord) string {
	if a := strings.TrimSpace(rec.Authorization); a != "" {
		return a
	}
	return "unrecorded"
}
