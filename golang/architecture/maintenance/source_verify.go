// SPDX-License-Identifier: AGPL-3.0-only

package maintenance

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
)

func VerifyRepositoryRevision(root string, doc architecture.ClaimDocumentBinding) LaneState {
	if strings.TrimSpace(root) == "" {
		return LaneState{State: LaneUnknown, Reasons: []Reason{{Code: "binding.revision_unavailable", Detail: "repository root not supplied"}}}
	}
	rev, status, _ := architecture.ResolveRevision(root, true)
	if status != architecture.RevisionResolved || rev == "" || doc.RevisionStatus != architecture.RevisionResolved || doc.Revision == "" {
		return LaneState{State: LaneUnknown, Reasons: []Reason{{Code: "binding.revision_unavailable", Detail: "repository revision cannot be verified"}}}
	}
	if rev != doc.Revision {
		return LaneState{State: LaneStale, Reasons: []Reason{{Code: "binding.revision_mismatch", Detail: fmt.Sprintf("document revision %s observed %s", doc.Revision, rev)}}}
	}
	return LaneState{State: LaneCurrent, Reasons: []Reason{{Code: "binding.current", Detail: "repository revision matches"}}}
}

func VerifyGraphDigest(doc, observed architecture.ClaimDocumentBinding) LaneState {
	if observed.GraphDigestStatus != architecture.GraphDigestResolved || observed.GraphDigestSHA256 == "" || doc.GraphDigestStatus != architecture.GraphDigestResolved || doc.GraphDigestSHA256 == "" {
		return LaneState{State: LaneUnknown, Reasons: []Reason{{Code: "binding.graph_digest_unavailable", Detail: "graph digest cannot be verified"}}}
	}
	if observed.GraphDigestSHA256 != doc.GraphDigestSHA256 {
		return LaneState{State: LaneStale, Reasons: []Reason{{Code: "binding.graph_digest_mismatch", Detail: fmt.Sprintf("document graph digest %s observed %s", doc.GraphDigestSHA256, observed.GraphDigestSHA256)}}}
	}
	return LaneState{State: LaneCurrent, Reasons: []Reason{{Code: "binding.current", Detail: "graph digest matches"}}}
}

func VerifySourceReceipt(root string, r architecture.ClaimFactReceipt) LaneState {
	if r.Provenance.SourceKind != "source_file" {
		return LaneState{State: LaneAbsent, Reasons: []Reason{{Code: "premise.current", Detail: "non-source fact does not require filesystem verification"}}}
	}
	if r.Provenance.SourceDigestStatus != architecture.SourceDigestResolved || r.Provenance.SourceDigest == "" {
		return LaneState{State: LaneUnknown, Reasons: []Reason{{Code: "premise.source_digest_unavailable", Detail: r.Fact.ID + " lacks resolved source digest"}}}
	}
	source := filepath.Clean(filepath.FromSlash(r.Fact.Evidence.SourceFile))
	if source == "." || strings.HasPrefix(source, ".."+string(filepath.Separator)) || source == ".." || filepath.IsAbs(source) {
		return LaneState{State: LaneUnknown, Reasons: []Reason{{Code: "premise.source_digest_unavailable", Detail: r.Fact.ID + " source path is unsafe"}}}
	}
	digest, err := architecture.SourceDigestSHA256(root, filepath.ToSlash(source))
	if err != nil {
		return LaneState{State: LaneStale, Reasons: []Reason{{Code: "premise.source_missing", Detail: err.Error()}}}
	}
	if digest != r.Provenance.SourceDigest {
		return LaneState{State: LaneStale, Reasons: []Reason{{Code: "premise.source_digest_changed", Detail: r.Fact.ID + " source digest changed"}}}
	}
	return LaneState{State: LaneCurrent, Reasons: []Reason{{Code: "premise.current", Detail: r.Fact.ID + " source digest matches"}}}
}
