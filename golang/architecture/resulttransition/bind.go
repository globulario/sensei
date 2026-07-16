// SPDX-License-Identifier: Apache-2.0

package resulttransition

import (
	"context"
	"fmt"
	"strings"

	"github.com/globulario/sensei/golang/architecture/admission"
	"github.com/globulario/sensei/golang/architecture/binding"
	"github.com/globulario/sensei/golang/architecture/closureprotocol"
)

// ResultMode selects how the exact result is named.
type ResultMode string

const (
	// ResultModeWorktree binds the current (uncommitted) working tree, staged and
	// unstaged tracked changes plus admitted untracked additions, without touching
	// the real index or working tree. It is the default mode.
	ResultModeWorktree ResultMode = "worktree"
	// ResultModeRevision binds an already-committed result revision.
	ResultModeRevision ResultMode = "revision"
)

// BindResultRequest asks for the exact result binding of one admitted,
// scope-verified task.
type BindResultRequest struct {
	RepositoryRoot string
	TaskDirectory  string
	Mode           ResultMode
	// ResultRevision is required for ResultModeRevision and must be empty for
	// ResultModeWorktree.
	ResultRevision string
}

// RepositoryResultBinding is the typed pre-transition record of the exact
// repository result tree. It is intentionally NOT a closureprotocol.ResultBinding:
// completing that frozen type requires a graph_digest_sha256 and generated
// pipeline artifacts that only the result pipeline (later Phase 7 slices) can
// honestly produce. Slice 7.3 lifts this record into the frozen ResultBinding
// once the graph exists; this slice never fabricates the missing fields.
type RepositoryResultBinding struct {
	// BaseRevision is the admitted base commit the result is bound against.
	BaseRevision string
	// PatchDigestSHA256 is the 64-hex SHA-256 of the base->result binary patch.
	PatchDigestSHA256 string
	// ResultTreeDigestSHA256 is the canonical Sensei tree identity (64 lowercase
	// hex) derived from deterministic Git tree material, NOT the native Git OID.
	ResultTreeDigestSHA256 string
	// ResultRevision is the committed result commit id in ResultModeRevision;
	// empty in ResultModeWorktree.
	ResultRevision string
	// GitTreeObjectID is the native Git tree object id (SHA-1 or SHA-256 depending
	// on the repository's object format). It is kept strictly separate from
	// ResultTreeDigestSHA256 and never used as a *_sha256 protocol digest.
	GitTreeObjectID string
}

// BoundRepositoryResult is the verified pre-transition bundle: the exact
// repository result plus the digests of the upstream ledger truth it was proven
// consistent with. A later slice uses these to construct the frozen
// ResultTransitionReceipt; this slice records nothing.
type BoundRepositoryResult struct {
	Mode             ResultMode
	Task             closureprotocol.TaskBinding
	RepositoryResult RepositoryResultBinding
	ObservedChange   admission.ObservedChangeSet

	BaseBindingDigestSHA256           string
	ActorBindingDigestSHA256          string
	AuthorityResolutionDigestSHA256   string
	AdmissionDecisionDigestSHA256     string
	CapabilityConsumptionDigestSHA256 string
	ObservedChangeSetDigestSHA256     string
	ScopeVerificationDigestSHA256     string
}

// BindRepositoryResult loads the verified upstream ledger truth for a task,
// re-materializes the exact committed or worktree result tree, proves the result
// is exactly what Phase 3 observed and scope-verified, and returns the typed
// pre-transition repository result binding. It never mutates the repository and
// never records a ledger event. Any inconsistency fails closed.
func BindRepositoryResult(ctx context.Context, req BindResultRequest) (BoundRepositoryResult, error) {
	root := strings.TrimSpace(req.RepositoryRoot)
	if root == "" {
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: repository root is required")
	}
	taskDir := strings.TrimSpace(req.TaskDirectory)
	if taskDir == "" {
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: task directory is required")
	}
	mode := req.Mode
	if mode == "" {
		mode = ResultModeWorktree
	}

	// 1. Load the verified upstream truth. Each loader verifies the ledger hash
	//    chain and fails closed on a missing or ambiguous event.
	base, err := admission.LoadTaskBaseBinding(taskDir)
	if err != nil {
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: load base binding: %w", err)
	}
	recAuth, err := admission.LoadRecordedAuthority(taskDir)
	if err != nil {
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: load authority resolution: %w", err)
	}
	decision, err := admission.LoadRecordedDecision(taskDir)
	if err != nil {
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: load admission decision: %w", err)
	}
	consumption, err := admission.LoadRecordedConsumption(taskDir)
	if err != nil {
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: load capability consumption: %w", err)
	}
	recordedObserved, err := admission.LoadRecordedObservedChange(taskDir)
	if err != nil {
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: load observed change: %w", err)
	}
	scope, err := admission.LoadRecordedScopeVerification(taskDir)
	if err != nil {
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: load scope verification: %w", err)
	}

	// 2. Single task identity across every record.
	task := base.Task
	if err := sameTask(task, recAuth.Base.Task); err != nil {
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: authority %w", err)
	}
	if err := sameTask(task, consumption.Task); err != nil {
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: consumption %w", err)
	}

	// 3. The task must be scope-verified before a result can be bound.
	if !admission.ScopeVerified(scope) {
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: scope_not_verified: task is not scope-verified")
	}

	// 4. Recompute the upstream digests and prove the recorded events reference
	//    exactly these values. A tampered or forged reference fails closed.
	actorDigest := closureprotocol.MustSemanticDigest(recAuth.Actor)
	authorityDigest := strings.TrimSpace(recAuth.Resolution.AuthorityResolutionDigestSHA256)
	recomputedAuthority, err := closureprotocol.AuthorityResolutionDigest(recAuth.Resolution)
	if err != nil {
		return BoundRepositoryResult{}, err
	}
	if authorityDigest == "" || authorityDigest != recomputedAuthority {
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: authority_resolution_digest_mismatch: recorded resolution digest is not self-consistent")
	}
	decisionDigest := closureprotocol.MustSemanticDigest(decision)
	consumptionDigest := closureprotocol.MustSemanticDigest(consumption)
	baseDigest, err := binding.SemanticDigestBase(base)
	if err != nil {
		return BoundRepositoryResult{}, err
	}
	observedDigest, err := admission.ObservedChangeSetDigest(recordedObserved)
	if err != nil {
		return BoundRepositoryResult{}, err
	}
	scopeDigest, err := admission.ScopeVerificationDigest(scope)
	if err != nil {
		return BoundRepositoryResult{}, err
	}

	checks := []struct {
		got, want, code string
	}{
		{scope.ScopeVerificationDigestSHA256, scopeDigest, "scope_verification_digest_mismatch"},
		{scope.ActorBindingDigestSHA256, actorDigest, "scope_actor_mismatch"},
		{scope.AuthorityResolutionDigestSHA256, authorityDigest, "scope_authority_mismatch"},
		{scope.DecisionDigestSHA256, decisionDigest, "scope_decision_mismatch"},
		{scope.ObservedChangeSetDigestSHA256, observedDigest, "scope_observed_mismatch"},
		{consumption.DecisionDigestSHA256, decisionDigest, "consumption_decision_mismatch"},
		{recordedObserved.ActorBindingDigestSHA256, actorDigest, "observed_actor_mismatch"},
		{recordedObserved.AuthorityResolutionDigestSHA256, authorityDigest, "observed_authority_mismatch"},
	}
	for _, c := range checks {
		if strings.TrimSpace(c.got) != strings.TrimSpace(c.want) {
			return BoundRepositoryResult{}, fmt.Errorf("resulttransition: %s: recorded ledger truth is inconsistent", c.code)
		}
	}

	// 5. The admitted base revision anchors the transition.
	baseRev := strings.TrimSpace(base.Repository.Revision)
	if baseRev == "" {
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: base binding has no revision")
	}
	if strings.TrimSpace(base.Repository.TreeDigestSHA256) != strings.TrimSpace(recordedObserved.BaseTreeDigestSHA256) ||
		strings.TrimSpace(scope.BaseTreeDigestSHA256) != strings.TrimSpace(recordedObserved.BaseTreeDigestSHA256) {
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: base_tree_mismatch: recorded base tree digests disagree")
	}

	// 6. Resolve the exact result treeish, re-materializing the worktree in an
	//    isolated index when no committed revision is named.
	var resultRevision string
	var treeish string
	var cleanup func()
	switch mode {
	case ResultModeRevision:
		rr := strings.TrimSpace(req.ResultRevision)
		if rr == "" {
			return BoundRepositoryResult{}, fmt.Errorf("resulttransition: result revision is required in revision mode")
		}
		full, err := runGit(root, "rev-parse", "--verify", rr+"^{commit}")
		if err != nil {
			return BoundRepositoryResult{}, fmt.Errorf("resulttransition: result revision %q is not a commit: %w", rr, err)
		}
		resultRevision = strings.TrimSpace(full)
		treeish = resultRevision
		cleanup = func() {}
	case ResultModeWorktree:
		if strings.TrimSpace(req.ResultRevision) != "" {
			return BoundRepositoryResult{}, fmt.Errorf("resulttransition: worktree mode takes no result revision")
		}
		t, c, err := worktreeResultTree(root, baseRev)
		if err != nil {
			return BoundRepositoryResult{}, fmt.Errorf("resulttransition: materialize worktree result: %w", err)
		}
		treeish, cleanup = t, c
	default:
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: unknown result mode %q", mode)
	}
	defer cleanup()

	// 7. Re-derive the observed change for the exact result and prove it is
	//    byte-identical to what Phase 3 recorded. This slice may verify the
	//    observation but must never silently replace it.
	reobserved, err := observeTreeish(root, baseRev, treeish, actorDigest, authorityDigest)
	if err != nil {
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: observe result: %w", err)
	}
	reDigest, err := admission.ObservedChangeSetDigest(reobserved)
	if err != nil {
		return BoundRepositoryResult{}, err
	}
	if reDigest != observedDigest {
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: observed_change_mismatch: current result differs from the scope-verified observation")
	}

	// 8. Confine the result to the repository: no rename, no path traversal, no
	//    symlink escaping the repository root.
	if err := confineResult(root, treeish, reobserved); err != nil {
		return BoundRepositoryResult{}, err
	}

	// 9. Canonical result tree identity, kept separate from the native OID.
	resultTree, err := binding.ResolveTreeIdentity(ctx, root, treeish)
	if err != nil {
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: resolve result tree identity: %w", err)
	}
	if !isHex64(resultTree.DigestSHA256) {
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: result tree digest is not a 64-hex sha256")
	}
	if resultTree.DigestSHA256 != strings.TrimSpace(recordedObserved.ResultTreeDigestSHA256) {
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: result_tree_mismatch: canonical result tree differs from the observed result tree")
	}

	// 10. The base->result patch digest.
	patch, err := patchDigest(root, baseRev, treeish)
	if err != nil {
		return BoundRepositoryResult{}, fmt.Errorf("resulttransition: compute patch digest: %w", err)
	}

	return BoundRepositoryResult{
		Mode: mode,
		Task: task,
		RepositoryResult: RepositoryResultBinding{
			BaseRevision:           baseRev,
			PatchDigestSHA256:      patch,
			ResultTreeDigestSHA256: resultTree.DigestSHA256,
			ResultRevision:         resultRevision,
			GitTreeObjectID:        resultTree.GitTreeObjectID,
		},
		ObservedChange:                    reobserved,
		BaseBindingDigestSHA256:           baseDigest,
		ActorBindingDigestSHA256:          actorDigest,
		AuthorityResolutionDigestSHA256:   authorityDigest,
		AdmissionDecisionDigestSHA256:     decisionDigest,
		CapabilityConsumptionDigestSHA256: consumptionDigest,
		ObservedChangeSetDigestSHA256:     observedDigest,
		ScopeVerificationDigestSHA256:     scopeDigest,
	}, nil
}

// ObserveChange re-derives the observed change for a result. When resultRev is
// empty it materializes the worktree in an isolated index. It is the single
// canonical observation producer shared with the admission-v2 command path so
// the two observations cannot drift.
func ObserveChange(repoRoot, baseRev, resultRev, actorDigest, authorityDigest string) (admission.ObservedChangeSet, error) {
	if strings.TrimSpace(baseRev) == "" {
		return admission.ObservedChangeSet{}, fmt.Errorf("resulttransition: recorded base binding has no revision")
	}
	treeish, cleanup, err := resolveResultTreeish(repoRoot, baseRev, resultRev)
	if err != nil {
		return admission.ObservedChangeSet{}, err
	}
	defer cleanup()
	return observeTreeish(repoRoot, baseRev, treeish, actorDigest, authorityDigest)
}

// observeTreeish builds the ObservedChangeSet for an already-resolved result
// treeish (a commit or a written tree object id).
func observeTreeish(repoRoot, baseRev, resultTreeish, actorDigest, authorityDigest string) (admission.ObservedChangeSet, error) {
	baseTree, err := binding.ResolveTreeIdentity(context.Background(), repoRoot, baseRev)
	if err != nil {
		return admission.ObservedChangeSet{}, err
	}
	resultTree, err := binding.ResolveTreeIdentity(context.Background(), repoRoot, resultTreeish)
	if err != nil {
		return admission.ObservedChangeSet{}, err
	}
	files, err := changedFiles(repoRoot, baseRev, resultTreeish)
	if err != nil {
		return admission.ObservedChangeSet{}, err
	}
	return admission.ObservedChangeSet{
		BaseTreeDigestSHA256:            baseTree.DigestSHA256,
		ResultTreeDigestSHA256:          resultTree.DigestSHA256,
		ActorBindingDigestSHA256:        actorDigest,
		AuthorityResolutionDigestSHA256: authorityDigest,
		Files:                           files,
	}, nil
}

func sameTask(a, b closureprotocol.TaskBinding) error {
	if strings.TrimSpace(a.ID) != strings.TrimSpace(b.ID) || strings.TrimSpace(a.SessionID) != strings.TrimSpace(b.SessionID) {
		return fmt.Errorf("task_identity_mismatch: record binds a different task")
	}
	return nil
}

func isHex64(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
