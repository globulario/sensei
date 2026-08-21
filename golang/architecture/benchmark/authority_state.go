// SPDX-License-Identifier: AGPL-3.0-only

package benchmark

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/senseireport"
	"github.com/globulario/sensei/golang/seedmeta"
)

// AuthorityState binds the Sensei-side authority that GOVERNED a frozen
// benchmark run, as distinct from the target repository's code, which the rest
// of FreezeReceipt already binds.
//
// A freeze that pins only the target repository is not reproducible. Replaying
// it later re-runs whatever authority happens to be installed then — a
// different seed, a different authored corpus, a different Sensei revision —
// against the same frozen code, and reports the resulting numbers as if they
// were comparable. They are not: the independent variable moved and nothing
// recorded that it did. Issue #131 states the requirement directly ("Every run
// must bind exact repository, revision/tree, graph digest/status,
// policy/profile, provider versions"); this type carries the half that was
// missing.
//
// This is deliberately an identity record, not a copy of the authority. It is
// small enough to live inside the receipt and exact enough that drift is
// decidable. Recovering the authority itself from these identities is a
// separate problem and is NOT claimed here.
type AuthorityState struct {
	// CaptureState is the closed vocabulary AuthorityCapture*. It is never
	// empty on a state this package produced: capture that fails records
	// AuthorityCaptureUnavailable plus a typed CaptureReason. An absent
	// authority binding must be a state a reader can see, not a zero value it
	// has to infer — a silent empty launders "we never bound this" into "this
	// matched".
	CaptureState string `json:"capture_state" yaml:"capture_state"`
	// CaptureReason is the typed AuthorityCaptureReason* cause, set exactly
	// when CaptureState is not AuthorityCaptureBound.
	CaptureReason string `json:"capture_reason,omitempty" yaml:"capture_reason,omitempty"`

	// SenseiRevision is the commit of the Sensei checkout that governs the run.
	SenseiRevision string `json:"sensei_revision,omitempty" yaml:"sensei_revision,omitempty"`
	// SenseiTreeDirty records that the working tree carried uncommitted changes
	// at capture time, which means SenseiRevision does NOT identify the code
	// that actually ran. Recorded rather than rejected: the run may still be
	// worth doing, but it can never be certified as reproducible.
	SenseiTreeDirty bool `json:"sensei_tree_dirty,omitempty" yaml:"sensei_tree_dirty,omitempty"`

	// SeedDigestSHA256 and SeedTripleCount identify the compiled graph artifact
	// that answered queries during the run, read from the build transaction
	// stamp. SeedTripleCount stays a string because that is how the stamp
	// records it; parsing it here would invent precision the source lacks.
	SeedDigestSHA256 string `json:"seed_digest_sha256,omitempty" yaml:"seed_digest_sha256,omitempty"`
	SeedTripleCount  string `json:"seed_triple_count,omitempty" yaml:"seed_triple_count,omitempty"`

	// AuthoredCorpusDigestSHA256 covers the human-authored awareness sources
	// under docs/awareness, EXCLUDING docs/awareness/candidates. Candidates are
	// not active authority, so churn there must not read as an authority
	// change; conversely a change to an authored source must, because that is
	// what the seed is derived from.
	AuthoredCorpusDigestSHA256 string `json:"authored_corpus_digest_sha256,omitempty" yaml:"authored_corpus_digest_sha256,omitempty"`

	// GraphBuildCommit and PairedRepoCommit are the source revisions the build
	// transaction stamp says the seed was compiled from. They can legitimately
	// differ from SenseiRevision (a seed is built at some commit and used at
	// later ones), which is exactly why both are recorded.
	GraphBuildCommit string `json:"graph_build_commit,omitempty" yaml:"graph_build_commit,omitempty"`
	PairedRepoCommit string `json:"paired_repo_commit,omitempty" yaml:"paired_repo_commit,omitempty"`

	// TransactionStampSHA256 is the digest of the stamp file itself, so a
	// rewritten stamp is detectable even when every field it reports is equal.
	TransactionStampSHA256 string `json:"transaction_stamp_sha256,omitempty" yaml:"transaction_stamp_sha256,omitempty"`
}

// Closed capture-state vocabulary.
const (
	AuthorityCaptureBound       = "bound"
	AuthorityCaptureUnavailable = "unavailable"
)

// Typed capture reasons (closed; no prose).
const (
	AuthorityCaptureReasonRepoUnset       = "sensei_repo_unset"
	AuthorityCaptureReasonRepoNotGit      = "sensei_repo_not_git"
	AuthorityCaptureReasonRevisionUnread  = "sensei_revision_unreadable"
	AuthorityCaptureReasonCorpusUnread    = "authored_corpus_unreadable"
	AuthorityCaptureReasonStampAbsent     = "build_transaction_stamp_absent"
	AuthorityCaptureReasonStampIncomplete = "build_transaction_stamp_incomplete"
)

// Closed replay-verdict vocabulary.
const (
	// AuthorityReplayMatch: both sides bound, every identity equal.
	AuthorityReplayMatch = "authority_match"
	// AuthorityReplayDrifted: both sides bound and at least one identity moved.
	AuthorityReplayDrifted = "authority_drifted"
	// AuthorityReplayUnverifiable: at least one side could not be bound, so
	// equality is undecidable. Distinct from drifted: we are not asserting the
	// authority changed, we are asserting we cannot tell.
	AuthorityReplayUnverifiable = "authority_unverifiable"
	// AuthorityReplayNotBound: the freeze receipt predates authority binding
	// and carries no authority state at all.
	AuthorityReplayNotBound = "authority_not_bound"
)

// Typed drift codes (closed; each names the identity that moved).
const (
	AuthorityDriftSenseiRevision   = "sensei_revision_drift"
	AuthorityDriftSeedDigest       = "seed_digest_drift"
	AuthorityDriftSeedTripleCount  = "seed_triple_count_drift"
	AuthorityDriftAuthoredCorpus   = "authored_corpus_drift"
	AuthorityDriftGraphBuildCommit = "graph_build_commit_drift"
	AuthorityDriftPairedRepoCommit = "paired_repo_commit_drift"
	AuthorityDriftStampDigest      = "build_transaction_stamp_drift"
	AuthorityDriftDirtyAtFreeze    = "sensei_tree_dirty_at_freeze"
	AuthorityDriftDirtyAtReplay    = "sensei_tree_dirty_at_replay"
)

// AuthorityDrift is the typed result of comparing a frozen authority identity
// with an independently observed one.
type AuthorityDrift struct {
	Verdict string   `json:"verdict" yaml:"verdict"`
	Reasons []Reason `json:"reasons,omitempty" yaml:"reasons,omitempty"`
	// Comparable is true only for AuthorityReplayMatch. It exists so callers
	// answer "may I compare these numbers?" without re-deriving the rule, and
	// so that every non-match state — drifted, unverifiable, not bound — fails
	// closed to the same answer: no.
	Comparable bool `json:"comparable" yaml:"comparable"`
}

// authoredCorpusExcludedPrefixes are paths under docs/awareness that are not
// active authority and so must not contribute to the authored-corpus identity.
var authoredCorpusExcludedPrefixes = []string{"candidates/"}

// CaptureAuthorityState records the authority identity of the Sensei checkout
// at senseiRepo. It never returns an error: a capture that cannot complete is
// itself a fact about the run and is returned as AuthorityCaptureUnavailable
// with a typed reason, so a caller cannot accidentally treat "we failed to
// look" as "there was nothing to see".
func CaptureAuthorityState(senseiRepo string) AuthorityState {
	repo := strings.TrimSpace(senseiRepo)
	if repo == "" {
		return AuthorityState{CaptureState: AuthorityCaptureUnavailable, CaptureReason: AuthorityCaptureReasonRepoUnset}
	}
	if _, err := os.Stat(filepath.Join(repo, ".git")); err != nil {
		return AuthorityState{CaptureState: AuthorityCaptureUnavailable, CaptureReason: AuthorityCaptureReasonRepoNotGit}
	}
	head, err := git(repo, "rev-parse", "HEAD")
	if err != nil {
		return AuthorityState{CaptureState: AuthorityCaptureUnavailable, CaptureReason: AuthorityCaptureReasonRevisionUnread}
	}
	state := AuthorityState{SenseiRevision: strings.TrimSpace(string(head))}

	// A dirty tree does not block capture; it is recorded so verification can
	// refuse to certify the run as reproducible.
	if porcelain, err := git(repo, "status", "--porcelain"); err == nil {
		state.SenseiTreeDirty = len(strings.TrimSpace(string(porcelain))) > 0
	}

	corpusDigest, err := senseireport.ContentDigest(filepath.Join(repo, "docs", "awareness"), authoredCorpusExcludedPrefixes, nil)
	if err != nil {
		state.CaptureState = AuthorityCaptureUnavailable
		state.CaptureReason = AuthorityCaptureReasonCorpusUnread
		return state
	}
	state.AuthoredCorpusDigestSHA256 = corpusDigest

	stampPath := filepath.Join(repo, "golang", "server", "embeddata", "awareness.transaction.tsv")
	stampBytes, err := os.ReadFile(stampPath)
	if err != nil {
		state.CaptureState = AuthorityCaptureUnavailable
		state.CaptureReason = AuthorityCaptureReasonStampAbsent
		return state
	}
	stamp := seedmeta.ParseTransactionStamp(stampBytes)
	if !stamp.Present || stamp.SeedDigest == "" {
		state.CaptureState = AuthorityCaptureUnavailable
		state.CaptureReason = AuthorityCaptureReasonStampIncomplete
		return state
	}
	state.SeedDigestSHA256 = stamp.SeedDigest
	state.SeedTripleCount = stamp.SeedTripleCount
	state.GraphBuildCommit = stamp.AwarenessGraphCommit
	state.PairedRepoCommit = stamp.ServicesCommit
	state.TransactionStampSHA256 = digest(stampBytes)
	state.CaptureState = AuthorityCaptureBound
	return state
}

// VerifyAuthorityState compares the authority frozen with a run against an
// authority observed independently at replay time.
//
// observed MUST come from a fresh CaptureAuthorityState against the checkout
// being replayed on. This function never substitutes a frozen value for a
// missing observed one; doing so is the exact move that turns a provenance
// record into a self-confirming claim.
func VerifyAuthorityState(frozen *AuthorityState, observed AuthorityState) AuthorityDrift {
	if frozen == nil {
		return AuthorityDrift{Verdict: AuthorityReplayNotBound, Reasons: []Reason{{Code: AuthorityReplayNotBound, Detail: "freeze receipt carries no authority state"}}}
	}
	if frozen.CaptureState != AuthorityCaptureBound {
		return AuthorityDrift{Verdict: AuthorityReplayUnverifiable, Reasons: []Reason{{Code: frozenReason(frozen.CaptureReason), Detail: "authority was not bound at freeze time"}}}
	}
	if observed.CaptureState != AuthorityCaptureBound {
		return AuthorityDrift{Verdict: AuthorityReplayUnverifiable, Reasons: []Reason{{Code: frozenReason(observed.CaptureReason), Detail: "authority could not be observed at replay time"}}}
	}

	var reasons []Reason
	// A dirty tree on either side means the recorded revision does not identify
	// the code, so equal revisions prove nothing.
	if frozen.SenseiTreeDirty {
		reasons = append(reasons, Reason{Code: AuthorityDriftDirtyAtFreeze})
	}
	if observed.SenseiTreeDirty {
		reasons = append(reasons, Reason{Code: AuthorityDriftDirtyAtReplay})
	}
	for _, c := range []struct {
		code           string
		frozen, actual string
	}{
		{AuthorityDriftSenseiRevision, frozen.SenseiRevision, observed.SenseiRevision},
		{AuthorityDriftSeedDigest, frozen.SeedDigestSHA256, observed.SeedDigestSHA256},
		{AuthorityDriftSeedTripleCount, frozen.SeedTripleCount, observed.SeedTripleCount},
		{AuthorityDriftAuthoredCorpus, frozen.AuthoredCorpusDigestSHA256, observed.AuthoredCorpusDigestSHA256},
		{AuthorityDriftGraphBuildCommit, frozen.GraphBuildCommit, observed.GraphBuildCommit},
		{AuthorityDriftPairedRepoCommit, frozen.PairedRepoCommit, observed.PairedRepoCommit},
		{AuthorityDriftStampDigest, frozen.TransactionStampSHA256, observed.TransactionStampSHA256},
	} {
		if c.frozen != c.actual {
			reasons = append(reasons, Reason{Code: c.code, Detail: "frozen " + shortID(c.frozen) + " observed " + shortID(c.actual)})
		}
	}
	if len(reasons) > 0 {
		return AuthorityDrift{Verdict: AuthorityReplayDrifted, Reasons: reasons}
	}
	return AuthorityDrift{Verdict: AuthorityReplayMatch, Comparable: true}
}

// frozenReason keeps the verdict's reason code typed even when the recorded
// capture reason is missing, so no path reports an empty code.
func frozenReason(reason string) string {
	if strings.TrimSpace(reason) == "" {
		return AuthorityReplayUnverifiable
	}
	return reason
}

// shortID renders an identity for human reading without implying the full
// value was compared on the prefix alone.
func shortID(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "(absent)"
	}
	if len(v) > 12 {
		return v[:12]
	}
	return v
}

// VerifyWorkspaceAuthority loads a frozen workspace and compares the authority
// it recorded against the authority observed right now at senseiRepo. This is
// the replay gate: it answers whether a re-run against this workspace may be
// compared with the original run's numbers.
func VerifyWorkspaceAuthority(workspace, senseiRepo string) (AuthorityDrift, error) {
	freeze, _, err := LoadWorkspaceFreeze(workspace)
	if err != nil {
		return AuthorityDrift{}, err
	}
	return VerifyAuthorityState(freeze.AuthorityState, CaptureAuthorityState(senseiRepo)), nil
}

// AuthoritySummary renders the authority a receipt froze, for compact status
// output. It reports the binding itself, never a comparison — comparing needs
// an independently observed authority, which a receipt alone cannot supply.
func AuthoritySummary(state *AuthorityState) string {
	if state == nil {
		return AuthorityReplayNotBound
	}
	if state.CaptureState != AuthorityCaptureBound {
		return state.CaptureState + " (" + frozenReason(state.CaptureReason) + ")"
	}
	s := AuthorityCaptureBound + " sensei=" + shortID(state.SenseiRevision) + " seed=" + shortID(state.SeedDigestSHA256) + " corpus=" + shortID(state.AuthoredCorpusDigestSHA256)
	if state.SenseiTreeDirty {
		s += " [" + AuthorityDriftDirtyAtFreeze + "]"
	}
	return s
}
