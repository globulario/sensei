// SPDX-License-Identifier: AGPL-3.0-only

package derive

import (
	"strings"
	"testing"
)

const ownerPkg = `package gitx

import (
	"context"
	"os/exec"
)

func Head(ctx context.Context, dir string) ([]byte, error) {
	return exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
}

func Status(ctx context.Context, dir string) ([]byte, error) {
	return exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain").Output()
}
`

const bystanderPkg = `package report

import "os/exec"

// Not git, so not a subject of a proposition about git.
func Pager() *exec.Cmd { return exec.Command("less") }

// An executable held in a variable: this derivation cannot see what it is, and
// says so in Limits rather than assuming it is harmless.
func Run(bin string) *exec.Cmd { return exec.Command(bin, "--version") }
`

const violatorPkg = `package doctor

import (
	"context"
	"os/exec"
)

func Check(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "git", "rev-parse", "HEAD").Output()
}
`

func confinement(paths ...string) Proposition {
	if len(paths) == 0 {
		paths = []string{"internal"}
	}
	return Proposition{Kind: KindCommandInvocationConfinedTo,
		Command: "git", Owner: "internal/gitx", SearchPaths: paths}
}

// The second family, on a tree where the boundary holds.
//
// A different SPECIES of fact from lock discipline — ownership of an external
// process boundary rather than a synchronization relation — travelling the same
// machinery. That is the point of adding it.
func TestCommandConfinementDerivesWhenTheBoundaryHolds(t *testing.T) {
	src := pinned(t, map[string]string{
		"internal/gitx/git.go":      ownerPkg,
		"internal/report/report.go": bystanderPkg,
	})
	receipt, est := Derive(src, confinement(), at("2026-08-23T12:00:00Z"))
	if receipt.Outcome != Derived || est == nil {
		t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
	}

	// The extent split, which matters far more here than for locks: the
	// derivation must read every file in the subtree to find invocations, and
	// says something about only the files that contain one.
	if len(receipt.Inputs) != 2 {
		t.Fatalf("inputs = %v", receipt.Inputs)
	}
	subjects := receipt.SubjectFiles()
	if len(subjects) != 1 || subjects[0] != "internal/gitx/git.go" {
		t.Fatalf("subjects = %v; report.go invokes `less` and a variable, neither of which is git", subjects)
	}
	for _, s := range receipt.Subjects {
		if s.Role != "invocation-site" {
			t.Errorf("subject role %q", s.Role)
		}
	}
}

// A real boundary violation is refuted, with the sites named.
func TestCommandConfinementRefutesABypass(t *testing.T) {
	src := pinned(t, map[string]string{
		"internal/gitx/git.go":      ownerPkg,
		"internal/doctor/doctor.go": violatorPkg,
	})
	receipt, est := Derive(src, confinement(), at("2026-08-23T12:00:00Z"))
	if receipt.Outcome != Refuted {
		t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
	}
	if est != nil {
		t.Fatal("a refuted confinement produced an Established")
	}
	if !strings.Contains(receipt.Detail, "internal/doctor/doctor.go") {
		t.Fatalf("the refutation does not name the bypass: %q", receipt.Detail)
	}
	// Subjects still describe where the command IS invoked, including the
	// violating site: that is what the proposition ranges over.
	if len(receipt.SubjectFiles()) != 2 {
		t.Fatalf("subjects = %v", receipt.SubjectFiles())
	}
}

// A narrower search is a WEAKER claim, not a cheaper one.
//
// Searching only the owner's own package would "confirm" confinement by not
// looking where a violation lives. The proposition records where it looked, so
// the two results are different propositions rather than the same one at
// different cost.
func TestANarrowSearchIsAWeakerClaimNotACheaperOne(t *testing.T) {
	files := map[string]string{
		"internal/gitx/git.go":      ownerPkg,
		"internal/doctor/doctor.go": violatorPkg,
	}
	src := pinned(t, files)

	wide, _ := Derive(src, confinement("internal"), at("2026-08-23T12:00:00Z"))
	if wide.Outcome != Refuted {
		t.Fatalf("wide search: %s", wide.Outcome)
	}
	narrow, _ := Derive(src, confinement("internal/gitx"), at("2026-08-23T12:00:00Z"))
	if narrow.Outcome != Derived {
		t.Fatalf("narrow search: %s (%s)", narrow.Outcome, narrow.Detail)
	}
	// The narrow one derived, and its own text says where it looked, so nobody
	// can read it as the wide claim.
	if !strings.Contains(narrow.Detail, "internal/gitx") {
		t.Fatalf("the narrow result does not record its search area: %q", narrow.Detail)
	}
	if !strings.Contains(narrow.Proposition.String(), "internal/gitx") {
		t.Fatalf("the proposition does not carry its search area: %q", narrow.Proposition.String())
	}
}

// An executable this derivation cannot see is not evidence of confinement.
func TestAVariableExecutableIsNotObservable(t *testing.T) {
	src := pinned(t, map[string]string{"internal/report/report.go": bystanderPkg})
	receipt, _ := Derive(src, confinement(), at("2026-08-23T12:00:00Z"))
	if receipt.Outcome != Unknown {
		t.Fatalf("%s: %s", receipt.Outcome, receipt.Detail)
	}
	if !strings.Contains(receipt.Detail, "nothing to establish") {
		t.Fatalf("detail = %q", receipt.Detail)
	}
	limited := false
	for _, l := range receipt.CompletenessScope {
		if strings.Contains(l, "variable") {
			limited = true
		}
	}
	if !limited {
		t.Fatalf("the limits do not mention an unobservable executable: %v", receipt.CompletenessScope)
	}
}

// Both families travel the same machinery: propose, derive, subjects, admit,
// revalidate, anchor. That is the evidence the architecture generalises.
func TestBothFamiliesTravelTheSameMachinery(t *testing.T) {
	src := pinned(t, map[string]string{
		"internal/event/bus.go": busGo,
		"internal/gitx/git.go":  ownerPkg,
	})
	for name, p := range map[string]Proposition{
		"lock discipline":     lockProp(),
		"command confinement": confinement(),
	} {
		receipt, est := Derive(src, p, at("2026-08-23T12:00:00Z"))
		if receipt.Outcome != Derived || est == nil {
			t.Fatalf("%s: %s (%s)", name, receipt.Outcome, receipt.Detail)
		}
		stored, err := Admit(receipt)
		if err != nil {
			t.Fatalf("%s: admit: %v", name, err)
		}
		re, reEst := stored.Revalidate(src, at("2026-08-23T13:00:00Z"))
		if re.Outcome != Derived || reEst == nil {
			t.Fatalf("%s: revalidate: %s", name, re.Outcome)
		}
		anchor, err := AnchorFor(*reEst, src.Commit())
		if err != nil {
			t.Fatalf("%s: anchor: %v", name, err)
		}
		if len(anchor.Files()) == 0 {
			t.Fatalf("%s: anchored no files", name)
		}
		if len(re.CompletenessScope) == 0 {
			t.Fatalf("%s: no completeness scope", name)
		}
	}
}
