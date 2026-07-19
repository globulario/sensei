// SPDX-License-Identifier: AGPL-3.0-only

package diffaudit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

const (
	SchemaV1     = "awareness.diff_audit/v1"
	TrustCaller  = "caller_supplied"
	MaxDiffBytes = 5 * 1024 * 1024 // 5 MB max diff payload
	MaxFileCount = 500
	MaxHunkCount = 1000
)

// Decision represents the canonical verdict of a diff audit.
type Decision string

const (
	DecisionPass         Decision = "pass"
	DecisionReview       Decision = "review"
	DecisionBlock        Decision = "block"
	DecisionCannotVerify Decision = "cannot_verify"
)

// Availability represents whether the graph / evaluation context is complete.
type Availability string

const (
	AvailabilityAvailable    Availability = "available"
	AvailabilityCannotVerify Availability = "cannot_verify"
	AvailabilityUnsupported  Availability = "unsupported"
)

// ChangeKind represents the exact file-level operation in the diff.
type ChangeKind string

const (
	ChangeAdd        ChangeKind = "add"
	ChangeModify     ChangeKind = "modify"
	ChangeDelete     ChangeKind = "delete"
	ChangeRename     ChangeKind = "rename"
	ChangeModeChange ChangeKind = "mode_change"
	ChangeBinary     ChangeKind = "binary"
)

// ReasonCode represents closed, typed limitation and error codes.
type ReasonCode string

const (
	ReasonMalformedDiff          ReasonCode = "malformed_diff"
	ReasonUnsupportedDiffFeature ReasonCode = "unsupported_diff_feature"
	ReasonPathIdentityInvalid    ReasonCode = "path_identity_invalid"
	ReasonRepoContextUnavailable ReasonCode = "repository_context_unavailable"
	ReasonGraphUnavailable       ReasonCode = "graph_unavailable"
	ReasonEvaluatorUnavailable   ReasonCode = "evaluator_unavailable"
	ReasonGovernedCorpusInvalid  ReasonCode = "governed_corpus_invalid"
	ReasonLimitExceeded          ReasonCode = "bounded_input_limit_exceeded"
	ReasonResultValidationFail   ReasonCode = "result_validation_failure"
)

// AuditFinding represents one governed record finding for a diff.
type AuditFinding struct {
	RecordID    string   `json:"record_id"`
	RecordClass string   `json:"record_class"` // invariant, failure_mode, forbidden_fix, required_test, contract
	Disposition string   `json:"disposition"`  // block, review, advisory
	FilePath    string   `json:"file_path"`    // relative path
	HunkIndex   int      `json:"hunk_index,omitempty"`
	StartLine   int      `json:"start_line,omitempty"`
	EndLine     int      `json:"end_line,omitempty"`
	Provenance  string   `json:"provenance,omitempty"`
	Explanation string   `json:"explanation"`
	RelatedIDs  []string `json:"related_ids,omitempty"`
}

// ChangedFileSummary describes one touched file in the audited diff.
type ChangedFileSummary struct {
	Path         string     `json:"path"`
	OldPath      string     `json:"old_path,omitempty"`
	Kind         ChangeKind `json:"kind"`
	OldMode      string     `json:"old_mode,omitempty"`
	NewMode      string     `json:"new_mode,omitempty"`
	HunkCount    int        `json:"hunk_count"`
	LinesAdded   int        `json:"lines_added"`
	LinesDeleted int        `json:"lines_deleted"`
}

// AuditResult represents the canonical v1 result schema ("awareness.diff_audit/v1").
type AuditResult struct {
	Schema              string               `json:"schema"`
	Digest              string               `json:"digest"`            // Self-excluding SHA256 hex digest
	InputDiffDigest     string               `json:"input_diff_digest"` // SHA256 hex of raw diff payload
	InputTrust          string               `json:"input_trust"`       // "caller_supplied"
	Availability        Availability         `json:"availability"`
	Decision            Decision             `json:"decision"`
	ExpectedHead        string               `json:"expected_head,omitempty"`
	ChangedFiles        []ChangedFileSummary `json:"changed_files"`
	Findings            []AuditFinding       `json:"findings"`
	ImplicatedTests     []string             `json:"implicated_tests,omitempty"`
	ImplicatedContracts []string             `json:"implicated_contracts,omitempty"`
	ReasonCodes         []ReasonCode         `json:"reason_codes,omitempty"`
	Limitations         []string             `json:"limitations,omitempty"`
}

// ComputeDigest calculates the canonical self-excluding SHA-256 digest of an AuditResult.
func (r *AuditResult) ComputeDigest() (string, error) {
	// Create a copy without the Digest field
	cp := *r
	cp.Digest = ""

	// Ensure slice fields are sorted deterministically
	sortChangedFiles(cp.ChangedFiles)
	sortAuditFindings(cp.Findings)
	cp.ImplicatedTests = sortedUniqueStrings(cp.ImplicatedTests)
	cp.ImplicatedContracts = sortedUniqueStrings(cp.ImplicatedContracts)
	cp.ReasonCodes = sortedReasonCodes(cp.ReasonCodes)
	cp.Limitations = sortedUniqueStrings(cp.Limitations)

	data, err := json.Marshal(cp)
	if err != nil {
		return "", fmt.Errorf("marshal digest payload: %w", err)
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

func sortChangedFiles(files []ChangedFileSummary) {
	sort.Slice(files, func(i, j int) bool {
		if files[i].Path != files[j].Path {
			return files[i].Path < files[j].Path
		}
		return files[i].Kind < files[j].Kind
	})
}

func sortAuditFindings(findings []AuditFinding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].FilePath != findings[j].FilePath {
			return findings[i].FilePath < findings[j].FilePath
		}
		if findings[i].RecordID != findings[j].RecordID {
			return findings[i].RecordID < findings[j].RecordID
		}
		return findings[i].Explanation < findings[j].Explanation
	})
}

func sortedUniqueStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	m := make(map[string]bool, len(in))
	for _, s := range in {
		if s != "" {
			m[s] = true
		}
	}
	out := make([]string, 0, len(m))
	for s := range m {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func sortedReasonCodes(in []ReasonCode) []ReasonCode {
	if len(in) == 0 {
		return nil
	}
	m := make(map[ReasonCode]bool, len(in))
	for _, r := range in {
		if r != "" {
			m[r] = true
		}
	}
	out := make([]ReasonCode, 0, len(m))
	for r := range m {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i] < out[j]
	})
	return out
}
