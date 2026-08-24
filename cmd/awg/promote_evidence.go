// SPDX-License-Identifier: AGPL-3.0-only

package main

// Independent evidence verification for a promotion candidate.
//
// The rule this file exists to enforce:
//
//	Evidence may be SELECTED by the proposer; its observed content must be
//	obtained independently.
//
// A proposer may say "look at these source lines". It may not say "these lines
// show X" and have that sentence count as the observation. So a typed evidence
// reference names WHERE to look, and this code goes and looks — reading the
// pinned bytes out of git rather than out of the candidate.
//
// And the invariant underneath it:
//
//	An authority-increasing claim may not be established solely from
//	assertions controlled by the claimant.
//
// # What this can and cannot establish
//
// It can establish a SOURCE FACT: at commit C, file F contains exactly this
// text. That is mechanically checkable, the proposer supplies no part of the
// answer, and a fabricated citation fails.
//
// It cannot establish what a source fact MEANS. The B specimen cites entirely
// real lines and draws the wrong architectural conclusion from them -- the bus
// mutex does serialize map access, that is simply not what it is for. Verified
// evidence plus a plausible sentence is not a derivation, and calling it one
// would be the same too-strong claim this whole surface has been correcting.
//
// So EvidenceVerified is deliberately NOT Established, and there is no code
// path here that turns the first into the second.

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// evidenceRef is a typed pointer to something checkable.
//
// Free-text evidence is what let the specimens through: "evidence is a
// non-empty string" is satisfied by "trust me". A reference names a location
// and a claim about its content, both of which can be wrong in a way that is
// detectable.
type evidenceRef struct {
	Kind string `yaml:"kind"`
	// Commit pins the world the reference is about. Without it "the file
	// contains X" is a statement about whatever is checked out right now, which
	// is not a fact about anything.
	Commit string `yaml:"commit"`
	File   string `yaml:"file"`
	// Contains is the exact text the proposer says is there. This is the part
	// that gets checked, and the only part the proposer controls.
	Contains string `yaml:"contains"`
}

// evidenceVerdict is what verification established, and the names are the
// point: none of them says the claim is true.
type evidenceVerdict string

const (
	// evidenceVerified: every reference was found in the pinned source. The
	// citations are real. Whether they support the claim is a separate question
	// this verification does not answer.
	evidenceVerified evidenceVerdict = "EVIDENCE_VERIFIED"
	// evidenceUnverifiable: at least one reference could not be confirmed --
	// the commit, file or text is not there. A fabricated citation lands here.
	evidenceUnverifiable evidenceVerdict = "EVIDENCE_UNVERIFIABLE"
	// evidenceClaimantControlled: every reference points at material the
	// claimant introduced. Structurally, the claim is supported only by the
	// claimant's own assertions, so it may not increase that claimant's
	// authority however real the bytes are.
	//
	// This refusal is DELIBERATELY NARROW and must not be generalised into
	// "evidence from the introducing commit is never valid". Real architecture
	// will be created by agents, and its adoption record will naturally cite
	// the commit that implemented it: the implementing commit is legitimate
	// evidence of WHAT WAS BUILT, and simply cannot alone establish that what
	// was built is correct. The rule is that claimant-controlled evidence may
	// CONTRIBUTE to an authority-increasing claim but may not be its sole
	// establishing basis; this check is one safe implementation of that
	// sentence for the one case measured, not the sentence itself.
	// See design doc §8d.
	evidenceClaimantControlled evidenceVerdict = "EVIDENCE_CLAIMANT_CONTROLLED"
	// evidenceAbsent: no typed references at all. The old free-text field is
	// not evidence; it is a sentence.
	evidenceAbsent evidenceVerdict = "EVIDENCE_ABSENT"
)

type evidenceResult struct {
	Verdict evidenceVerdict
	Checked int
	Detail  string
}

// verifyEvidenceRefs reads each reference out of git and reports what it found.
//
// introducedBy is the commit the candidate itself arrives in, when known. A
// reference whose commit is that same commit is material the claimant brought
// with it, which is the C specimen: real bytes, cited honestly, and still
// incapable of establishing anything about the claimant's own authority.
func verifyEvidenceRefs(ctx context.Context, repoDir string, refs []evidenceRef, introducedBy string) evidenceResult {
	if len(refs) == 0 {
		return evidenceResult{Verdict: evidenceAbsent,
			Detail: "no typed evidence references; a free-text evidence field is a sentence, not an observation"}
	}
	independent := 0
	for i, ref := range refs {
		if !strings.EqualFold(strings.TrimSpace(ref.Kind), "source_fact") {
			return evidenceResult{Verdict: evidenceUnverifiable, Checked: i,
				Detail: fmt.Sprintf("reference %d has kind %q, and source_fact is the only kind this verifier can obtain independently", i, ref.Kind)}
		}
		if strings.TrimSpace(ref.Commit) == "" || strings.TrimSpace(ref.File) == "" || strings.TrimSpace(ref.Contains) == "" {
			return evidenceResult{Verdict: evidenceUnverifiable, Checked: i,
				Detail: fmt.Sprintf("reference %d is incomplete: commit, file and contains are all required", i)}
		}
		// The proposer supplies no part of this. The bytes come from git.
		out, err := exec.CommandContext(ctx, "git", "-C", repoDir, "show",
			strings.TrimSpace(ref.Commit)+":"+strings.TrimSpace(ref.File)).Output()
		if err != nil {
			return evidenceResult{Verdict: evidenceUnverifiable, Checked: i,
				Detail: fmt.Sprintf("reference %d: cannot read %s at %s", i, ref.File, shortCommit(ref.Commit))}
		}
		if !strings.Contains(string(out), strings.TrimSpace(ref.Contains)) {
			return evidenceResult{Verdict: evidenceUnverifiable, Checked: i,
				Detail: fmt.Sprintf("reference %d: %s at %s does not contain the cited text",
					i, ref.File, shortCommit(ref.Commit))}
		}
		if introducedBy == "" || !strings.HasPrefix(strings.TrimSpace(ref.Commit), strings.TrimSpace(introducedBy)) {
			independent++
		}
	}
	if independent == 0 {
		return evidenceResult{Verdict: evidenceClaimantControlled, Checked: len(refs),
			Detail: "every reference points at material introduced by the same change as the claim; " +
				"an authority-increasing claim may not rest solely on assertions the claimant controls"}
	}
	return evidenceResult{Verdict: evidenceVerified, Checked: len(refs),
		Detail: fmt.Sprintf("%d of %d reference(s) verified against material the claimant did not introduce",
			independent, len(refs))}
}

func shortCommit(c string) string {
	c = strings.TrimSpace(c)
	if len(c) > 12 {
		return c[:12]
	}
	return c
}

// establishesSemanticClaim is deliberately a constant false, and the comment is
// the deliverable.
//
// Verified references say the citations are real. A claim about what the code
// is FOR -- why a lock exists, what an abstraction protects, which invariant a
// mechanism serves -- does not follow from the bytes being present, and no
// composition of source facts in this file derives one.
//
// The B specimen is the standing proof: every line it cites is genuinely there,
// and its architectural conclusion is wrong. Any future mechanism that claims
// to establish this class must confront B before it is believed.
//
// Until then a judgment-bearing claim may be RETAINED with its evidence
// verified, and may not increase the authority of the run that proposed it.
func establishesSemanticClaim(evidenceResult) bool { return false }
