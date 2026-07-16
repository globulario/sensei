// SPDX-License-Identifier: AGPL-3.0-only

package tasksession

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/authority"
	"github.com/globulario/sensei/golang/architecture/closure"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/architecture/identity"
)

// Limitations recorded on the task ledger when typed mutation authority could
// not be resolved during preparation.
const (
	limitationIdentityNotEnrolled = "agent_identity_not_enrolled"
	limitationAuthorityUnresolved = "agent_authority_unresolved"
)

// resolvedAuthority bundles the artifacts an authority_resolved event carries.
type resolvedAuthority struct {
	Resolution closureprotocol.AuthorityResolution
	Actor      closureprotocol.ActorBinding
	ChangePlan closureprotocol.ChangePlan
}

// resolveTaskAuthority attempts to resolve typed mutation authority for a task
// during preparation. It fails soft: a nil result with a non-empty limitation
// means no authority_resolved event should be appended and the legacy
// limitations stand. It never mints identity — an unenrolled agent yields
// agent_identity_not_enrolled. A non-nil result is recorded even when the
// resolution refuses individual operations; the refusal is carried in the
// resolution and surfaced when advancing the task.
func resolveTaskAuthority(repoRoot, taskRoot string, taskReq TaskRequest, base closureprotocol.BaseBinding, evaluatedAt time.Time) (*resolvedAuthority, string) {
	idRoot := identity.Root(repoRoot)
	id, ok, err := identity.LoadManifest(idRoot)
	if err != nil || !ok {
		return nil, limitationIdentityNotEnrolled
	}
	plan := changePlanFromScope(taskReq)
	if len(plan.Operations) == 0 {
		// Inspect-only task: no mutation to govern, nothing to resolve.
		return nil, ""
	}
	index, err := authority.LoadPolicyIndex(repoRoot)
	if err != nil {
		return nil, limitationAuthorityUnresolved
	}
	verified, err := authority.VerifyActorBinding(id.ActorBinding(), identity.Resolver(idRoot), index, evaluatedAt)
	if err != nil {
		return nil, limitationAuthorityUnresolved
	}
	applicability, closureDigest, err := loadTaskApplicability(taskRoot, plan)
	if err != nil {
		return nil, limitationAuthorityUnresolved
	}
	resolution, err := admission.ResolveAuthority(index, admission.ResolveAuthorityInput{
		Actor:                            id.ActorBinding(),
		VerifiedActor:                    verified,
		Base:                             base,
		ChangePlan:                       plan,
		Applicability:                    applicability,
		PolicyID:                         base.Policies.Admission,
		ClosureAssessmentDigestSHA256:    closureDigest,
		AuthorityPolicyGraphDigestSHA256: closureprotocol.MustSemanticDigest(index),
		EvaluatedAt:                      evaluatedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, limitationAuthorityUnresolved
	}
	return &resolvedAuthority{Resolution: resolution, Actor: id.ActorBinding(), ChangePlan: plan}, ""
}

// changePlanFromScope synthesizes a typed change plan from the task scope. Only
// mutating operations are governed; reads do not consume mutation authority.
func changePlanFromScope(req TaskRequest) closureprotocol.ChangePlan {
	var ops []closureprotocol.ChangeOperation
	risk := normalizeChangeRisk(req.RiskClass)
	for i, f := range req.Scope.Files {
		if f.Operation != admission.OperationModify {
			continue
		}
		ops = append(ops, closureprotocol.ChangeOperation{
			OperationID:       fmt.Sprintf("op.%d", i),
			Kind:              closureprotocol.OperationModify,
			TargetKind:        "source_file",
			Target:            f.Path,
			SelectedMechanism: closureprotocol.MechanismRepositoryEdit,
			RiskClass:         risk,
		})
	}
	return closureprotocol.ChangePlan{PlanID: "plan." + req.TaskID, Operations: ops}
}

// normalizeChangeRisk passes the declared risk through so the resolver enforces
// the grant's risk ceiling; an unspecified risk defaults to the highest class
// the repository-repair grant permits.
func normalizeChangeRisk(s string) string {
	if strings.TrimSpace(s) == "" {
		return closure.RiskArchitectureSensitive
	}
	return strings.TrimSpace(s)
}

// loadTaskApplicability reuses the closure assessment's covers-path-matched
// authority bindings — never a hard-coded domain — and groups them by target
// file into the admission applicability type. It also returns the closure report
// digest, recorded as the resolution's closure-assessment provenance.
func loadTaskApplicability(taskRoot string, plan closureprotocol.ChangePlan) ([]authority.AuthorityApplicability, string, error) {
	reportPath := filepath.Join(taskRoot, "convergence", "latest", "closure-after-dialogue.yaml")
	reportBytes, err := os.ReadFile(reportPath)
	if err != nil {
		return nil, "", err
	}
	report, err := closure.LoadReport(reportPath)
	if err != nil {
		return nil, "", err
	}
	opByFile := map[string]string{}
	for _, op := range plan.Operations {
		opByFile[op.Target] = op.OperationID
	}
	byFile := map[string]*authority.AuthorityApplicability{}
	for _, r := range report.AuthorityBindings {
		opID, ok := opByFile[r.TargetFile]
		if !ok {
			continue
		}
		a := byFile[r.TargetFile]
		if a == nil {
			a = &authority.AuthorityApplicability{OperationID: opID, TargetFile: r.TargetFile}
			byFile[r.TargetFile] = a
		}
		a.AuthorityDomainIDs = appendUniqueString(a.AuthorityDomainIDs, r.AuthorityDomainID)
		for _, m := range r.RequiredRuntimeMechanismIDs {
			a.RequiredRuntimeMechanismIDs = appendUniqueString(a.RequiredRuntimeMechanismIDs, m)
		}
		if len(r.RelationPath) > 0 {
			a.RelationPaths = append(a.RelationPaths, append([]string(nil), r.RelationPath...))
		}
	}
	out := make([]authority.AuthorityApplicability, 0, len(byFile))
	for _, op := range plan.Operations {
		if a := byFile[op.Target]; a != nil {
			out = append(out, *a)
		}
	}
	sum := sha256.Sum256(reportBytes)
	return out, hex.EncodeToString(sum[:]), nil
}

func appendUniqueString(in []string, v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return in
	}
	for _, x := range in {
		if x == v {
			return in
		}
	}
	return append(in, v)
}
