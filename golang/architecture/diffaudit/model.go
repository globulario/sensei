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
	Domain              string               `json:"domain,omitempty"`
	Task                string               `json:"task,omitempty"`
	GraphCommit         string               `json:"graph_commit,omitempty"` // observed authority commit of the rule snapshot; binds the digest to the graph that produced it
	ChangedFiles        []ChangedFileSummary `json:"changed_files"`
	Findings            []AuditFinding       `json:"findings"`
	ImplicatedTests     []string             `json:"implicated_tests,omitempty"`
	ImplicatedContracts []string             `json:"implicated_contracts,omitempty"`
	ReasonCodes         []ReasonCode         `json:"reason_codes,omitempty"`
	Limitations         []string             `json:"limitations,omitempty"`
}

// Validate asserts that the AuditResult conforms to the canonical v1 specification.
func (r *AuditResult) Validate() error {
	if r.Schema != SchemaV1 {
		return fmt.Errorf("invalid schema %q, expected %q", r.Schema, SchemaV1)
	}
	if r.InputTrust != TrustCaller {
		return fmt.Errorf("invalid input trust %q, expected %q", r.InputTrust, TrustCaller)
	}
	if r.Digest == "" {
		return fmt.Errorf("digest is required")
	}

	// Validate closed vocabularies
	switch r.Availability {
	case AvailabilityAvailable, AvailabilityCannotVerify, AvailabilityUnsupported:
	default:
		return fmt.Errorf("invalid availability: %q", r.Availability)
	}

	switch r.Decision {
	case DecisionPass, DecisionReview, DecisionBlock, DecisionCannotVerify:
	default:
		return fmt.Errorf("invalid decision: %q", r.Decision)
	}

	for _, f := range r.Findings {
		if f.RecordID == "" {
			return fmt.Errorf("finding record_id is empty")
		}
		if f.RecordClass == "" {
			return fmt.Errorf("finding record_class is empty")
		}
		switch f.Disposition {
		case "block", "review", "advisory", "cannot_verify":
		default:
			return fmt.Errorf("invalid finding disposition: %q", f.Disposition)
		}
	}

	// Decision consistency checks
	hasBlock := false
	hasReview := false
	hasCannotVerify := false
	for _, f := range r.Findings {
		if f.Disposition == "block" {
			hasBlock = true
		} else if f.Disposition == "review" {
			hasReview = true
		} else if f.Disposition == "cannot_verify" {
			hasCannotVerify = true
		}
	}

	if (r.Availability != AvailabilityAvailable || len(r.ReasonCodes) > 0) && r.Decision == DecisionPass {
		return fmt.Errorf("invalid state: decision cannot be pass when availability is %s or reason codes are present", r.Availability)
	}

	// An available result must be bound to the rule snapshot that produced it.
	// Availability stays available only when every graph-backed impact query
	// succeeded, so a missing graph_commit means the verdict is unanchored.
	if r.Availability == AvailabilityAvailable && r.GraphCommit == "" {
		return fmt.Errorf("invalid state: availability is available but graph_commit is empty; an available result must be bound to a rule snapshot")
	}

	if hasBlock && r.Decision != DecisionBlock {
		return fmt.Errorf("decision mismatch: expected block, got %s", r.Decision)
	}
	if !hasBlock && hasReview && r.Decision != DecisionReview && r.Decision != DecisionCannotVerify {
		return fmt.Errorf("decision mismatch: expected review or cannot_verify, got %s", r.Decision)
	}
	if hasCannotVerify && r.Decision != DecisionCannotVerify {
		return fmt.Errorf("decision mismatch: expected cannot_verify, got %s", r.Decision)
	}

	computed, err := r.ComputeDigest()
	if err != nil {
		return fmt.Errorf("failed to compute validation digest: %w", err)
	}
	if computed != r.Digest {
		return fmt.Errorf("result digest mismatch: calculated %s != stored %s", computed, r.Digest)
	}

	return nil
}

// ComputeDigest calculates the canonical self-excluding SHA-256 digest of an AuditResult.
func (r *AuditResult) ComputeDigest() (string, error) {
	cp := *r
	cp.Digest = ""

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
		if findings[i].HunkIndex != findings[j].HunkIndex {
			return findings[i].HunkIndex < findings[j].HunkIndex
		}
		return findings[i].RecordClass < findings[j].RecordClass
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
