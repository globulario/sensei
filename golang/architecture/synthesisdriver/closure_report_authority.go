// SPDX-License-Identifier: AGPL-3.0-only

package synthesisdriver

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/interpretationclosure"
	"github.com/globulario/sensei/golang/architecture/synthesis"
)

// ClosureReportAuthorityConfig composes interpretation authority from owners
// Sensei already has. Report is the exact task closure report whose semantic
// digest is stored on synthesis.Session. GoProbes are optional deterministic
// challenge queries, never authored outcomes; the authority executes them
// against RepositoryRoot itself.
type ClosureReportAuthorityConfig struct {
	Report                    closure.Report
	GoProbes                  []interpretationclosure.GoProbe
	ChallengePlanDigestSHA256 string
}

// ClosureReportAuthority is the production pre-synthesis evidence owner. It
// does not ask O2 whether O2's interpretation is correct. It recomputes the
// bound task closure identity, projects canonical architecture.Claim
// epistemic states from that report, executes optional Go fact probes, and
// derives scope/proof observations before asking interpretationclosure policy
// to fold those observations into an advisory/governing receipt.
type ClosureReportAuthority struct {
	report              closure.Report
	reportDigest        string
	goProbes            []interpretationclosure.GoProbe
	challengePlanDigest string
}

var _ InterpretationAuthority = (*ClosureReportAuthority)(nil)

func NewClosureReportAuthority(config ClosureReportAuthorityConfig) (*ClosureReportAuthority, error) {
	digest, err := closureprotocol.SemanticDigest(config.Report)
	if err != nil {
		return nil, fmt.Errorf("synthesisdriver: digest interpretation closure report: %w", err)
	}
	probes := append([]interpretationclosure.GoProbe(nil), config.GoProbes...)
	challengeDigest := ""
	if len(probes) != 0 {
		computed, err := interpretationclosure.ChallengePlanDigest(interpretationclosure.ChallengePlan{
			SchemaVersion: interpretationclosure.ChallengePlanSchemaVersion,
			GoProbes:      probes,
		})
		if err != nil {
			return nil, fmt.Errorf("synthesisdriver: validate interpretation challenge plan: %w", err)
		}
		if config.ChallengePlanDigestSHA256 != "" && config.ChallengePlanDigestSHA256 != computed {
			return nil, fmt.Errorf("synthesisdriver: interpretation challenge plan digest %q does not match recomputed %q", config.ChallengePlanDigestSHA256, computed)
		}
		challengeDigest = computed
	} else if config.ChallengePlanDigestSHA256 != "" {
		return nil, fmt.Errorf("synthesisdriver: interpretation challenge digest was supplied without any challenge probes")
	}
	return &ClosureReportAuthority{report: config.Report, reportDigest: digest, goProbes: probes, challengePlanDigest: challengeDigest}, nil
}

func (a *ClosureReportAuthority) Assess(ctx context.Context, request InterpretationAuthorityRequest) (interpretationclosure.Receipt, error) {
	if a == nil {
		return interpretationclosure.Receipt{}, fmt.Errorf("synthesisdriver: nil closure-report interpretation authority")
	}
	if err := ctx.Err(); err != nil {
		return interpretationclosure.Receipt{}, err
	}

	// Detect mutation of the report value after composition and bind it to the
	// same task-closure artifact the O1 session already names.
	currentDigest, err := closureprotocol.SemanticDigest(a.report)
	if err != nil {
		return interpretationclosure.Receipt{}, fmt.Errorf("synthesisdriver: redigest interpretation closure report: %w", err)
	}
	if currentDigest != a.reportDigest {
		return interpretationclosure.Receipt{}, fmt.Errorf("synthesisdriver: interpretation closure report changed after authority construction: %q != %q", currentDigest, a.reportDigest)
	}
	if request.Session.ClosureDigestSHA256 != a.reportDigest {
		return interpretationclosure.Receipt{}, fmt.Errorf("synthesisdriver: interpretation authority closure report digest %q does not match session closure digest %q", a.reportDigest, request.Session.ClosureDigestSHA256)
	}

	findings, err := a.truthFindings(ctx, request)
	if err != nil {
		return interpretationclosure.Receipt{}, err
	}
	completeness := a.completeness(request.Interpretation)
	proofs := unresolvedRequiredProofs(request.Interpretation.RequiredProofObligations, a.reportDigest)

	// Minimal realization concerns the implementation plan/candidate, which
	// does not exist yet at this boundary. Claiming minimality from an
	// interpretation's source references would confuse premise scope with
	// implementation breadth. Preserve unknown here; it is neutral by policy
	// and downstream candidate verification remains independent.
	realization := interpretationclosure.RealizationAssessment{
		Status:             interpretationclosure.RealizationUnknown,
		EvidenceReferences: []string{"closure-report:" + a.reportDigest},
		Detail:             "pre-synthesis closure makes no implementation minimality claim",
	}

	return interpretationclosure.Certify(interpretationclosure.Input{
		InterpretationDigestSHA256: request.Interpretation.InterpretationDigestSHA256,
		RepositoryRevision:         request.Session.BaseRevision,
		GraphAuthorityDigestSHA256: request.Session.GraphAuthorityDigestSHA256,
		ClosureDigestSHA256:        a.reportDigest,
		TruthFindings:              findings,
		Completeness:               completeness,
		Realization:                realization,
		ProofObservations:          proofs,
	})
}

func (a *ClosureReportAuthority) truthFindings(ctx context.Context, request InterpretationAuthorityRequest) ([]interpretationclosure.TruthFinding, error) {
	binding := make(map[string]bool, len(request.Interpretation.BindingInvariants))
	for _, id := range request.Interpretation.BindingInvariants {
		id = strings.TrimSpace(id)
		if id != "" {
			binding[id] = true
		}
	}

	// First project Sensei's canonical claim epistemic state. The local
	// TruthStatus is only the three-way deterministic challenge result:
	// supported, contradicted, or unknown. Contested/stale/superseded remain
	// unknown rather than being forced into either proof or contradiction.
	findings := make([]interpretationclosure.TruthFinding, 0, len(binding)+len(a.goProbes))
	covered := map[string]bool{}
	for claimID := range binding {
		matches := relevantClaimMatches(a.report.RelevantClaims, claimID)
		if len(matches) == 0 {
			continue
		}
		status := interpretationclosure.TruthUnknown
		detail := "canonical claim state is unresolved for contradiction purposes"
		refs := make([]string, 0, len(matches))
		for _, match := range matches {
			refs = append(refs, "closure-report:"+a.reportDigest+"#claim:"+match.ID)
			switch match.EpistemicStatus {
			case architecture.StatusRefuted:
				status = interpretationclosure.TruthContradicted
				detail = "canonical architectural claim is refuted"
			case architecture.StatusSupported:
				if status != interpretationclosure.TruthContradicted {
					status = interpretationclosure.TruthSupported
					detail = "canonical architectural claim is supported"
				}
			case architecture.StatusContested, architecture.StatusUnknown, architecture.StatusStale, architecture.StatusSuperseded:
				if status != interpretationclosure.TruthContradicted && status != interpretationclosure.TruthSupported {
					status = interpretationclosure.TruthUnknown
				}
			}
		}
		findings = append(findings, interpretationclosure.TruthFinding{
			ClaimID:            claimID,
			CheckKind:          "canonical_claim_epistemic_status",
			Status:             status,
			EvidenceReferences: refs,
			Detail:             detail,
		})
		covered[claimID] = true
	}

	// A challenge plan may only challenge an actual binding invariant in this
	// interpretation. This prevents an irrelevant easy fact from being
	// supplied merely to make the receipt look mechanically grounded.
	if len(a.goProbes) != 0 {
		for _, probe := range a.goProbes {
			if !binding[strings.TrimSpace(probe.ClaimID)] {
				return nil, fmt.Errorf("synthesisdriver: Go interpretation probe claim %q is not a binding invariant of interpretation %q", probe.ClaimID, request.Interpretation.InterpretationID)
			}
		}
		probes := append([]interpretationclosure.GoProbe(nil), a.goProbes...)
		for i := range probes {
			probes[i].EvidenceReferences = []string{
				"challenge-plan:" + a.challengePlanDigest,
				"repository-revision:" + request.Session.BaseRevision,
			}
		}
		goFindings := interpretationclosure.CheckGoTruth(ctx, request.RepositoryRoot, probes)
		findings = append(findings, goFindings...)
		for _, finding := range goFindings {
			covered[strings.TrimSpace(finding.ClaimID)] = true
		}
	}

	// Every binding invariant receives an explicit challenge outcome. No
	// checker is represented as unknown, never as pass. This is what lets O1
	// later verify receipt coverage without turning decidability into a
	// prerequisite for architecture.
	ids := make([]string, 0, len(binding))
	for id := range binding {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, claimID := range ids {
		if covered[claimID] {
			continue
		}
		findings = append(findings, interpretationclosure.UnknownTruth(
			claimID,
			"unknown",
			"no_deterministic_checker",
			"no deterministic repository checker or canonical claim match is available for this governing invariant",
		))
	}
	return findings, nil
}

func relevantClaimMatches(claims []closure.ClaimReceipt, reference string) []closure.ClaimReceipt {
	out := make([]closure.ClaimReceipt, 0, 1)
	for _, claim := range claims {
		if claim.ID == reference || claim.PropositionKey == reference {
			out = append(out, claim)
		}
	}
	return out
}

func (a *ClosureReportAuthority) completeness(interp synthesis.Interpretation) interpretationclosure.CompletenessAssessment {
	references := make([]string, 0, len(interp.SourceReferences))
	for _, reference := range interp.SourceReferences {
		references = append(references, reference.Reference)
	}
	return a.completenessForReferences(references)
}

// completenessForReferences derives the required surface exclusively from
// closure.Report, the existing owner. It never asks the interpretation to
// describe what it thinks is required; interpretation only supplies what it
// disclosed.
func (a *ClosureReportAuthority) completenessForReferences(references []string) interpretationclosure.CompletenessAssessment {
	evidence := "closure-report:" + a.reportDigest
	disclosed := disclosedFileReferences(references)
	required := append([]string(nil), a.report.ScopeReceipt.Files...)

	missingFromClosure := append([]string(nil), a.report.ScopeReceipt.MissingFiles...)
	missingFromClosure = append(missingFromClosure, a.report.ScopeReceipt.MissingSymbols...)
	missingFromClosure = append(missingFromClosure, a.report.ScopeReceipt.MissingComponents...)
	missingFromClosure = append(missingFromClosure, a.report.ScopeReceipt.MissingClaims...)
	missingFromClosure = append(missingFromClosure, a.report.ScopeReceipt.MissingPropositions...)
	if len(missingFromClosure) != 0 {
		return interpretationclosure.CompletenessAssessment{
			Status:             interpretationclosure.CompletenessIncomplete,
			EvidenceReferences: []string{evidence},
			MissingSurface:     missingFromClosure,
			Detail:             "the authoritative task closure report itself records unresolved scope",
		}
	}

	// A closed/conditionally-closed report with no file surface establishes
	// an explicitly empty file requirement. Open/uncertifiable with no file
	// surface cannot prove scope completeness.
	if len(required) == 0 {
		switch a.report.Verdict {
		case closure.VerdictClosed, closure.VerdictConditionallyClosed:
			required = []string{}
		default:
			required = nil
		}
	}
	return interpretationclosure.AssessCompleteness(disclosed, required, evidence)
}

func disclosedFileReferences(references []string) []string {
	out := make([]string, 0, len(references))
	for _, ref := range references {
		if path, ok := sourceReferenceFile(ref); ok {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return compactStrings(out)
}

// sourceReferenceFile recognizes only explicit file-shaped references. It is
// intentionally conservative: an awareness node may represent a file, but
// without a resolver proving that relation this function must not guess and
// silently upgrade it to scope evidence.
func sourceReferenceFile(reference string) (string, bool) {
	ref := strings.TrimSpace(reference)
	for _, prefix := range []string{"file:", "source_file:"} {
		if strings.HasPrefix(ref, prefix) {
			ref = strings.TrimSpace(strings.TrimPrefix(ref, prefix))
			break
		}
	}
	if hash := strings.IndexByte(ref, '#'); hash >= 0 {
		ref = ref[:hash]
	}
	// Accept the common path:line anchor only when the suffix is numeric.
	if colon := strings.LastIndexByte(ref, ':'); colon > 0 {
		if _, err := strconv.Atoi(ref[colon+1:]); err == nil {
			ref = ref[:colon]
		}
	}
	ref = filepath.ToSlash(filepath.Clean(strings.TrimSpace(ref)))
	if ref == "." || ref == "" || filepath.IsAbs(ref) || ref == ".." || strings.HasPrefix(ref, "../") {
		return "", false
	}
	// A bare awareness/contract identifier must not accidentally become a
	// file. Requiring a slash or a recognizable source extension keeps the
	// projection conservative while allowing top-level files such as go.mod.
	if !strings.Contains(ref, "/") && filepath.Ext(ref) == "" && ref != "go.mod" && ref != "go.sum" {
		return "", false
	}
	return ref, true
}

func compactStrings(in []string) []string {
	out := in[:0]
	for _, value := range in {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}

func unresolvedRequiredProofs(ids []string, closureDigest string) []interpretationclosure.ProofObservation {
	out := make([]interpretationclosure.ProofObservation, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		out = append(out, interpretationclosure.ProofObservation{
			ObligationID:         id,
			RequiredForAuthority: true,
			Status:               interpretationclosure.ProofUnresolved,
			EvidenceReferences:   []string{"closure-report:" + closureDigest},
			Detail:               "no interpretation-proof discharge resolver is composed for this obligation",
		})
	}
	return out
}
