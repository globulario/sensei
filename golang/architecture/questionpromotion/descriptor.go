// SPDX-License-Identifier: Apache-2.0

package questionpromotion

import "path/filepath"

// CandidateDescriptor is UNTRUSTED claimed routing metadata read from a discovered lineage's
// receipt. It confers NO authority: the values decide only whether a FAILED candidate is
// relevant enough to a requested scope to report, never whether any candidate is admitted.
// Only VerifyCommittedPromotion establishes verified scope and authority. An unreadable
// candidate has Readable=false and no claimed identity — it must be treated as unrelated
// (not as a defect against a specific scope).
type CandidateDescriptor struct {
	LineageID        string
	ClaimedDomain    string
	ClaimedFiles     []string
	ClaimedTaskID    string
	ClaimedSessionID string
	Readable         bool
}

// LoadCandidateDescriptor reads the untrusted claimed routing metadata for one discovered
// lineage. It performs NO verification and confers NO authority. It never returns an error:
// an unreadable/absent/malformed candidate yields Readable=false so the caller treats it as
// unrelated debris rather than a scoped defect.
func LoadCandidateDescriptor(repoRoot, lineageID string) CandidateDescriptor {
	promotionDir := filepath.Join(repoRoot, ".sensei", "project", "promotions", lineageID)
	rc, err := loadReceipt(filepath.Join(promotionDir, receiptFileName))
	if err != nil {
		return CandidateDescriptor{LineageID: lineageID, Readable: false}
	}
	return CandidateDescriptor{
		LineageID:        lineageID,
		ClaimedDomain:    rc.EffectiveScopeDomain,
		ClaimedFiles:     append([]string(nil), rc.EffectiveScopeFiles...),
		ClaimedTaskID:    rc.Task.ID,
		ClaimedSessionID: rc.Task.SessionID,
		Readable:         true,
	}
}
