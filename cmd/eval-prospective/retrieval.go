// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/prospective"
	"github.com/globulario/sensei/golang/architecture/prospectivescore"
)

// TaskTextRuleID names the rule that turns a frozen change into the task text
// an author would have typed.
//
// The frozen packages carry the diff and the touched paths; they carry no
// commit subject and no author intent, because the inventory was built from
// diffs. So the task text is composed mechanically from the paths and git's
// own status letters, and the exact string is recorded per change in the run
// artifact.
//
// This is a LOWER BOUND on the context a real author would supply, and the
// report says so. An author typing "make the reload path fail closed when the
// marker is missing" hands retrieval far more to work with than "modify
// golang/server/reload.go" does. Inventing richer intent here would be worse:
// it would be a benchmark-only channel, and the score would measure the
// harness's paraphrasing rather than production's retrieval.
const TaskTextRuleID = "paths_and_git_status.v1"

// TaskTextFor composes the deterministic task description for one change.
func TaskTextFor(change prospective.BlindChange) string {
	var parts []string
	for _, p := range change.Paths {
		parts = append(parts, verbFor(p)+" "+p.Path)
	}
	if len(parts) == 0 {
		return "change " + change.ChangeID
	}
	return strings.Join(parts, "; ")
}

func verbFor(p prospective.PathChange) string {
	switch {
	case !p.ExistedBefore:
		return "add"
	case strings.HasPrefix(p.Status, "D"):
		return "delete"
	case strings.HasPrefix(p.Status, "R"):
		return "rename"
	default:
		return "modify"
	}
}

// ContextClassesFor records which context classes were actually available to
// production before it ran (protocol section 7.4).
//
// It describes the INPUT, not what retrieval did with it. Every class here is
// derived from the frozen package alone, so the answer cannot change because
// production changed.
func ContextClassesFor(change prospective.BlindChange) []string {
	out := []string{prospectivescore.CtxChangeContents, prospectivescore.CtxPackageIdentity, prospectivescore.CtxDirectoryOwnership}
	if strings.Contains(change.Content, "import") {
		out = append(out, prospectivescore.CtxImports)
	}
	// The remaining classes are properties of the graph the surface consults,
	// not of the frozen package, so they are recorded only where the package
	// itself establishes them. Claiming a class production may not have had is
	// how a low score gets attributed to the wrong cause.
	sort.Strings(out)
	return out
}

// preflightResponse is the subset of the production JSON this harness reads.
type preflightResponse struct {
	Status   string `json:"status"`
	Coverage struct {
		DirectAnchorCount int    `json:"direct_anchor_count"`
		Sufficient        bool   `json:"sufficient"`
		Note              string `json:"note"`
	} `json:"coverage"`
	DirectInvariants     []knowledgeNode `json:"direct_invariants"`
	DirectFailureModes   []knowledgeNode `json:"direct_failure_modes"`
	DirectForbiddenFixes []knowledgeNode `json:"direct_forbidden_fixes"`
	DirectRequiredTests  []knowledgeNode `json:"direct_required_tests"`
	DirectIntents        []knowledgeNode `json:"direct_intents"`
	DirectArchitecture   []knowledgeNode `json:"direct_architecture"`
	Authority            struct {
		Verdict             string `json:"authority_verdict"`
		GraphFreshnessState string `json:"graph_freshness_state"`
	} `json:"authority"`
}

type knowledgeNode struct {
	ID    string `json:"id"`
	Class string `json:"class"`
}

// channels pairs each response field with its name, so a surfaced item can
// record which channel delivered it.
func (r preflightResponse) channels() []struct {
	name  string
	nodes []knowledgeNode
} {
	return []struct {
		name  string
		nodes []knowledgeNode
	}{
		{"direct_invariants", r.DirectInvariants},
		{"direct_failure_modes", r.DirectFailureModes},
		{"direct_forbidden_fixes", r.DirectForbiddenFixes},
		{"direct_required_tests", r.DirectRequiredTests},
		{"direct_intents", r.DirectIntents},
		{"direct_architecture", r.DirectArchitecture},
	}
}

// CorpusIndex resolves a surfaced node to an eligible corpus item.
//
// Two vocabularies have to meet here. The corpus enumerates by class and names
// items `class:id`; production returns a node's own class, and MetaPrinciple
// nodes are dual-typed meta.* invariants that surface in the invariant
// partition. A strict qualified match would therefore score every one of the
// corpus's meta_principle items as permanently unsurfaceable — a systematic
// zero that would read as a retrieval failure rather than as a vocabulary
// mismatch in the ruler.
//
// So the qualified match is tried first, and an unqualified match is used only
// when the short id is unique across the whole corpus. Every hit records which
// rule matched it.
type CorpusIndex struct {
	qualified map[string]bool
	byShortID map[string]string
	ambiguous map[string]bool
}

func NewCorpusIndex(ids []string) CorpusIndex {
	idx := CorpusIndex{qualified: map[string]bool{}, byShortID: map[string]string{}, ambiguous: map[string]bool{}}
	for _, id := range ids {
		idx.qualified[id] = true
		short := id
		if i := strings.Index(id, ":"); i >= 0 {
			short = id[i+1:]
		}
		if existing, ok := idx.byShortID[short]; ok && existing != id {
			idx.ambiguous[short] = true
			continue
		}
		idx.byShortID[short] = id
	}
	return idx
}

// Resolve returns the corpus item a surfaced node corresponds to, the match
// rule that found it, and whether it is in the corpus at all.
func (idx CorpusIndex) Resolve(node knowledgeNode) (string, string, bool) {
	qualified := node.Class + ":" + node.ID
	if idx.qualified[qualified] {
		return qualified, prospectivescore.MatchExact, true
	}
	if idx.ambiguous[node.ID] {
		return "", "", false
	}
	if id, ok := idx.byShortID[node.ID]; ok {
		return id, prospectivescore.MatchIDOnly, true
	}
	return "", "", false
}

// Retriever invokes the frozen production surface.
type Retriever struct {
	Bin    string
	Addr   string
	Domain string
	Repo   string
}

// Invoke runs one preflight for one change and classifies what came back.
//
// The status vocabulary is closed and the mapping is fixed here, before any
// score exists:
//
//	invocation failed, every path is new  → no_prospective_channel
//	invocation failed otherwise           → unavailable
//	PREFLIGHT_STATUS_DEGRADED             → degraded
//	PREFLIGHT_STATUS_EMPTY                → empty
//	PREFLIGHT_STATUS_OK, no direct anchor → no_anchors
//	PREFLIGHT_STATUS_OK                   → resolved
//	anything else                         → unavailable
//
// The last line is deliberate: an unrecognised status is not evidence of
// coverage, and a harness that guessed at one would be inventing a result.
func (r Retriever) Invoke(ctx context.Context, pkg prospective.BlindPackage, idx CorpusIndex) prospectivescore.ChangeRun {
	out := prospectivescore.ChangeRun{
		ItemKey:          pkg.ItemKey,
		ContextAvailable: ContextClassesFor(pkg.Change),
	}
	args := []string{"preflight"}
	for _, p := range pkg.Change.Paths {
		args = append(args, "--file", p.Path)
	}
	args = append(args, "--task", TaskTextFor(pkg.Change), "--domain", r.Domain, "--repo", r.Repo, "--addr", r.Addr, "--json")

	started := time.Now()
	// Direct argv, never a shell.
	cmd := exec.CommandContext(ctx, r.Bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	inv := prospectivescore.Invocation{
		Command:  r.Bin + " " + strings.Join(args, " "),
		ExitOK:   err == nil,
		Duration: time.Since(started).Round(time.Millisecond).String(),
	}
	if err != nil {
		inv.Error = strings.TrimSpace(stderr.String())
		if inv.Error == "" {
			inv.Error = err.Error()
		}
		out.Invocations = append(out.Invocations, inv)
		out.RetrievalStatus = prospectivescore.StatusUnavailable
		out.StatusDetail = inv.Error
		if allPathsNew(pkg.Change) {
			out.RetrievalStatus = prospectivescore.StatusNoProspectiveChannel
			out.StatusDetail = "production refused to answer for a change whose paths do not exist at the pinned base: " + inv.Error
		}
		return out
	}
	out.Invocations = append(out.Invocations, inv)

	var resp preflightResponse
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		out.RetrievalStatus = prospectivescore.StatusUnavailable
		out.StatusDetail = fmt.Sprintf("production answered with something this harness cannot read: %v", err)
		return out
	}

	seen := map[string]bool{}
	var outside []string
	for _, ch := range resp.channels() {
		for _, node := range ch.nodes {
			id, rule, ok := idx.Resolve(node)
			if !ok {
				outside = append(outside, node.Class+":"+node.ID)
				continue
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			out.Surfaced = append(out.Surfaced, prospectivescore.Surfaced{
				CorpusItemID: id, SurfacedAs: node.Class + ":" + node.ID, MatchRule: rule, Channel: ch.name,
			})
		}
	}
	sort.Strings(outside)
	out.SurfacedOutsideCorpus = outside
	sort.SliceStable(out.Surfaced, func(i, j int) bool { return out.Surfaced[i].CorpusItemID < out.Surfaced[j].CorpusItemID })

	switch resp.Status {
	case "PREFLIGHT_STATUS_DEGRADED":
		out.RetrievalStatus = prospectivescore.StatusDegraded
	case "PREFLIGHT_STATUS_EMPTY":
		out.RetrievalStatus = prospectivescore.StatusEmpty
	case "PREFLIGHT_STATUS_OK":
		if resp.Coverage.DirectAnchorCount == 0 {
			out.RetrievalStatus = prospectivescore.StatusNoAnchors
		} else {
			out.RetrievalStatus = prospectivescore.StatusResolved
		}
	default:
		out.RetrievalStatus = prospectivescore.StatusUnavailable
		out.StatusDetail = "production reported the unrecognised status " + resp.Status
	}
	if out.StatusDetail == "" {
		out.StatusDetail = strings.TrimSpace(resp.Coverage.Note)
	}
	return out
}

func allPathsNew(c prospective.BlindChange) bool {
	if len(c.Paths) == 0 {
		return false
	}
	for _, p := range c.Paths {
		if p.ExistedBefore {
			return false
		}
	}
	return true
}
