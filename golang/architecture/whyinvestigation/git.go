// SPDX-License-Identifier: AGPL-3.0-only

package whyinvestigation

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/investigation"
)

type GitProvider struct{ Root string }

type executionState string

const (
	executionComplete    executionState = "complete"
	executionPartial     executionState = "partial"
	executionUnavailable executionState = "unavailable"
)

type executionOutcome struct {
	State       executionState
	Reason      string
	Limitations []architecture.Limitation
	Evidence    []investigation.EvidenceReceipt
}

func (GitProvider) Identity() investigation.ProviderBinding {
	return investigation.ProviderBinding{ID: GitProviderID, Version: GitProviderVersion}
}

func (p GitProvider) Capture(ctx context.Context, req CaptureRequest) (Snapshot, error) {
	if _, err := runGit(ctx, p.Root, "rev-parse", "--is-inside-work-tree"); err != nil {
		return Snapshot{}, fmt.Errorf("local Git history unavailable: %w", err)
	}
	if _, err := runGit(ctx, p.Root, "rev-parse", "--verify", req.Range.Start+"^{commit}"); err != nil {
		return Snapshot{}, fmt.Errorf("resolve history range start: %w", err)
	}
	if _, err := runGit(ctx, p.Root, "rev-parse", "--verify", req.Range.End+"^{commit}"); err != nil {
		return Snapshot{}, fmt.Errorf("resolve history range end: %w", err)
	}
	shallow, err := runGit(ctx, p.Root, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return Snapshot{}, fmt.Errorf("determine Git history completeness: %w", err)
	}
	format := "%H%x1f%P%x1f%aI%x1f%cI%x1f%B%x1e"
	out, err := runGit(ctx, p.Root, "log", "--reverse", "--format="+format, req.Range.Start+".."+req.Range.End)
	if err != nil {
		return Snapshot{}, err
	}
	var commits []Commit
	for _, record := range strings.Split(out, "\x1e") {
		fields := strings.Split(strings.TrimSpace(record), "\x1f")
		if len(fields) != 5 {
			continue
		}
		paths, _ := runGit(ctx, p.Root, "show", "--format=", "--name-only", fields[0])
		patch, _ := runGit(ctx, p.Root, "show", "--format=", "--no-ext-diff", fields[0])
		commits = append(commits, Commit{ID: fields[0], Parents: fields[1], AuthorTime: fields[2], CommitterTime: fields[3], Message: fields[4], ChangedPaths: paths, PatchDigest: investigation.SHA256String(patch)})
	}
	sort.Slice(commits, func(i, j int) bool { return commits[i].ID < commits[j].ID })
	data, err := json.Marshal(commits)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Provider: p.Identity(), Digest: investigation.SHA256Bytes(data), Range: req.Range, Incomplete: strings.TrimSpace(shallow) == "true", Commits: commits}, nil
}

func (p GitProvider) Investigate(_ context.Context, snap Snapshot, req CaptureRequest) (Result, error) {
	queryDigest, err := digestQuery(req.Query)
	if err != nil {
		return Result{}, err
	}
	target, err := digestTarget(req, queryDigest)
	if err != nil {
		return Result{}, err
	}
	coverage := investigation.CoverageEntry{ProviderID: GitProviderID, ProviderVersion: GitProviderVersion, Category: investigation.EvidenceSourceControl, TargetDigestSHA256: target, SourceSnapshotDigestSHA256: snap.Digest, SearchedTimeRange: &investigation.TimeRange{Start: req.Range.Start, End: req.Range.End}}
	outcome := executionOutcome{State: executionComplete}
	if snap.Incomplete {
		outcome.State = executionPartial
		outcome.Reason = "local Git history is shallow or incomplete"
		outcome.Limitations = []architecture.Limitation{{Source: GitProviderID, Scope: "history", Reason: outcome.Reason}}
	}
	var evidence []investigation.EvidenceReceipt
	for _, commit := range snap.Commits {
		content, err := json.Marshal(commit)
		if err != nil {
			return Result{}, err
		}
		id := "evidence_" + investigation.SHA256String(GitProviderID + "|" + commit.ID)[:16]
		coverage.ResultEvidenceIDs = append(coverage.ResultEvidenceIDs, id)
		evidence = append(evidence, investigation.EvidenceReceipt{ID: id, Category: investigation.EvidenceSourceControl, Provider: p.Identity(), ProofStrength: investigation.ProofStaticSource, SourceIdentity: "git:commit:" + commit.ID, SourceDigestSHA256: investigation.SHA256String(string(content)), ContentDigestSHA256: investigation.SHA256Bytes(content), CapturedContent: string(content), Scope: architecture.ClaimScope{Repository: req.Repository.RepositoryDomain, Symbols: append(append([]string{}, req.Query.TargetObservationIDs...), req.Query.TargetEvidenceIDs...)}, CapturedAt: req.CapturedAt})
	}
	outcome.Evidence = evidence
	switch outcome.State {
	case executionUnavailable:
		coverage.Status, coverage.Reason = investigation.CoverageUnavailable, outcome.Reason
	case executionPartial:
		if len(evidence) == 0 {
			coverage.Status, coverage.Reason = investigation.CoverageUnavailable, outcome.Reason
		} else {
			coverage.Status = investigation.CoverageSupporting
		}
	default:
		if len(evidence) == 0 {
			coverage.Status = investigation.CoverageNoResult
		} else {
			coverage.Status = investigation.CoverageSupporting
		}
	}
	coverage.Limitations = outcome.Limitations
	return Result{RawEvidence: outcome.Evidence, Coverage: coverage, Limitations: outcome.Limitations}, nil
}

// Extract captures only local Git history and emits an evidence-only WHY document.
func Extract(ctx context.Context, root string, req CaptureRequest) (investigation.Document, error) {
	if err := validateRequest(req); err != nil {
		return investigation.Document{}, err
	}
	p := GitProvider{Root: root}
	snap, err := p.Capture(ctx, req)
	if err != nil {
		if _, gitErr := runGit(ctx, root, "rev-parse", "--is-inside-work-tree"); gitErr == nil {
			return investigation.Document{}, err
		}
		return unavailableDocument(req, err)
	}
	result, err := p.Investigate(ctx, snap, req)
	if err != nil {
		return investigation.Document{}, err
	}
	return composeDocument(req, snap, result)
}

func composeDocument(req CaptureRequest, snap Snapshot, result Result) (investigation.Document, error) {
	query, err := digestQuery(req.Query)
	if err != nil {
		return investigation.Document{}, err
	}
	plan := investigation.Plan{ID: "plan.why.git.v1", Description: "deterministic offline Git history", Queries: []string{query}}
	planData, err := json.Marshal(plan)
	if err != nil {
		return investigation.Document{}, err
	}
	profile := investigation.SHA256String(GitProviderID + "|" + GitProviderVersion)
	doc := investigation.Document{SchemaVersion: "investigation.schema.v1", GeneratedBy: "sensei.whyinvestigation", Mode: investigation.ModeWhy, Binding: investigation.Binding{Repository: req.Repository, EvidenceSnapshotDigestSHA256: snap.Digest, InvestigationPlanDigestSHA256: investigation.SHA256Bytes(planData), ExtractorProfileDigestSHA256: profile, Model: investigation.ModelBinding{Status: investigation.ModelStatusDisabled}, Why: investigation.WhyBinding{HowDocumentDigestSHA256: req.How.Receipt.OutputDocumentDigestSHA256, QueryDigestSHA256: query, TargetObservationIDs: req.Query.TargetObservationIDs, TargetEvidenceIDs: req.Query.TargetEvidenceIDs, HistoryRangeStart: req.Range.Start, HistoryRangeEnd: req.Range.End}}, Plan: plan, Coverage: []investigation.CoverageEntry{result.Coverage}, RawEvidence: result.RawEvidence, Limitations: result.Limitations, Receipt: investigation.RunReceipt{SchemaVersion: "investigation.schema.v1", GeneratedBy: "sensei.whyinvestigation", Repository: req.Repository, GraphDigestSHA256: req.Repository.GraphDigestSHA256, PlanDigestSHA256: investigation.SHA256Bytes(planData), ExtractorProfileDigestSHA256: profile, EvidenceSnapshotDigestSHA256: snap.Digest, Model: investigation.ModelBinding{Status: investigation.ModelStatusDisabled}, PostProcessingVersion: "why.git.v1", TimestampSource: req.CapturedAt, ResourceLimits: map[string]string{"provider": "local_git"}, NondeterminismDeclaration: "deterministic_only"}}
	norm, err := investigation.Normalize(doc)
	if err != nil {
		return investigation.Document{}, err
	}
	digest, err := investigation.CalculateDocumentDigest(norm)
	if err != nil {
		return investigation.Document{}, err
	}
	norm.Receipt.OutputDocumentDigestSHA256 = digest
	if err := investigation.Validate(norm); err != nil {
		return investigation.Document{}, err
	}
	return norm, nil
}

func validateRequest(req CaptureRequest) error {
	if err := investigation.Validate(req.How); err != nil {
		return fmt.Errorf("validate HOW document: %w", err)
	}
	digest, err := investigation.CalculateDocumentDigest(req.How)
	if err != nil || digest != req.How.Receipt.OutputDocumentDigestSHA256 {
		return fmt.Errorf("HOW document digest mismatch")
	}
	if req.Repository.RepositoryDomain != req.How.Binding.Repository.RepositoryDomain || req.Repository.Revision != req.How.Binding.Repository.Revision || req.Range.Start == "" || req.Range.End == "" || req.CapturedAt == "" {
		return fmt.Errorf("invalid WHY repository binding, range, or capture timestamp")
	}
	seen := map[string]bool{}
	for _, f := range req.How.Observations {
		seen[f.ID] = true
	}
	for _, e := range req.How.RawEvidence {
		seen[e.ID] = true
	}
	for _, id := range append(append([]string{}, req.Query.TargetObservationIDs...), req.Query.TargetEvidenceIDs...) {
		if !seen[id] {
			return fmt.Errorf("unknown HOW evidence target %q", id)
		}
	}
	return nil
}
func unavailableDocument(req CaptureRequest, cause error) (investigation.Document, error) {
	query, err := digestQuery(req.Query)
	if err != nil {
		return investigation.Document{}, err
	}
	snapshot := investigation.SHA256String(GitProviderID + "|unavailable|" + req.Repository.RepositoryDomain + "|" + req.Range.Start + "|" + req.Range.End + "|" + query)
	result := Result{Coverage: investigation.CoverageEntry{ProviderID: GitProviderID, ProviderVersion: GitProviderVersion, Category: investigation.EvidenceSourceControl, TargetDigestSHA256: investigation.SHA256String("unavailable|" + query), Status: investigation.CoverageUnavailable, Reason: "local Git repository unavailable"}, Limitations: []architecture.Limitation{{Source: GitProviderID, Scope: "repository", Reason: cause.Error()}}}
	return composeDocument(req, Snapshot{Provider: GitProvider{}.Identity(), Digest: snapshot, Range: req.Range}, result)
}

func runGit(ctx context.Context, root string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func digestQuery(q Query) (string, error) {
	data, err := json.Marshal(q)
	if err != nil {
		return "", err
	}
	return investigation.SHA256Bytes(data), nil
}
func digestTarget(req CaptureRequest, query string) (string, error) {
	data, err := json.Marshal(struct {
		Repository        architecture.ClaimDocumentBinding
		Query, Start, End string
	}{req.Repository, query, req.Range.Start, req.Range.End})
	if err != nil {
		return "", err
	}
	return investigation.SHA256Bytes(data), nil
}
