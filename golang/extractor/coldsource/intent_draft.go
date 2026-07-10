// SPDX-License-Identifier: Apache-2.0

package coldsource

import (
	"context"
	"sort"
	"strings"
)

// INTENT DRAFTER — the proposer half of intent mining. It turns gathered
// rule-bearing excerpts (intent_sources.go) into PROPOSED IntentCandidates. The
// real proposer is an LLM (intent_draft_llm.go); EchoIntentDrafter is a
// deterministic, no-key stand-in that exercises the pipeline end to end and makes
// NO quality judgement.
//
// CONTRACT (enforced by validateIntentDraft, independent of the drafter):
//   - every SourceCitation MUST be one of the provided excerpt citations — the
//     proposer cannot fabricate a stated source (the intent cage);
//   - at least one source citation is required;
//   - CodeAnchors are NOT cage-checked here — they are the proposer's GUESS at
//     where the rule lives, and are verified downstream by GroundIntent (symbol
//     resolution). Fabricated/weak anchors surface as unresolved/symbol_absent.
//
// The drafter proposes; GroundIntent grounds; a human approves. A drafter can
// never mint an accepted intent: status is always candidate.

// intentDraft is the raw shape a drafter emits, before routing into the
// IntentCandidate sources/evidence split.
type intentDraft struct {
	IntentID              string
	Claim                 string
	Category              string
	SourceCitations       []string // MUST be from the provided excerpts (cage)
	CodeAnchors           []string // inferred file:<path> the rule lives in (grounded, not caged)
	RelatedInvariants     []string
	RelatedMetaPrinciples []string
}

// IntentDrafter turns excerpts into proposed candidates.
type IntentDrafter interface {
	DraftIntents(ctx context.Context, excerpts []IntentExcerpt) ([]intentDraft, error)
}

// validateIntentDraft enforces the intent cage. Returns violations (empty == ok).
func validateIntentDraft(d intentDraft, allowed map[string]string) []string {
	var v []string
	if strings.TrimSpace(d.Claim) == "" {
		v = append(v, "empty claim")
	}
	if len(d.SourceCitations) == 0 {
		v = append(v, "no source citation: every intent must cite a gathered excerpt")
	}
	for _, sc := range d.SourceCitations {
		if _, ok := allowed[sc]; !ok {
			v = append(v, "fabricated source not in gathered excerpts: "+sc)
		}
	}
	return v
}

// materialize routes a validated draft into a grounded-ready IntentCandidate:
// source citations split by their excerpt KIND into Sources vs Evidence, and
// inferred CodeAnchors into Evidence.Code/Tests.
func materialize(d intentDraft, kindByCitation map[string]string) IntentCandidate {
	c := IntentCandidate{
		IntentID:              d.IntentID,
		Claim:                 d.Claim,
		Category:              d.Category,
		RelatedInvariants:     d.RelatedInvariants,
		RelatedMetaPrinciples: d.RelatedMetaPrinciples,
		Status:                "candidate",
		ExtractedByLLM:        true,
	}
	for _, sc := range d.SourceCitations {
		switch kindByCitation[sc] {
		case "docs", "schemas":
			c.Sources.Docs = append(c.Sources.Docs, citationToPath(sc))
		case "comments":
			c.Sources.Comments = append(c.Sources.Comments, citationToPath(sc))
		case "prs":
			c.Sources.Docs = append(c.Sources.Docs, sc) // pr:<id>:<cid>, tiered as maintainer_intent
		case "tests":
			c.Evidence.Tests = append(c.Evidence.Tests, sc)
		case "commits":
			c.Evidence.Commits = append(c.Evidence.Commits, sc)
		}
	}
	for _, a := range d.CodeAnchors {
		if isTestPath(citationToPath(a)) {
			c.Evidence.Tests = append(c.Evidence.Tests, a)
		} else {
			c.Evidence.Code = append(c.Evidence.Code, a)
		}
	}
	return c
}

// citationToPath strips a "file:" prefix and a trailing ":<line>" so a source
// tier classifier / grounder sees a clean repo-relative path.
func citationToPath(c string) string {
	c = strings.TrimPrefix(c, "file:")
	if i := strings.LastIndexByte(c, ':'); i >= 0 {
		if isAllDigits(c[i+1:]) {
			c = c[:i]
		}
	}
	return c
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// EchoIntentDrafter is the deterministic, no-LLM proposer. It emits one candidate
// per gathered excerpt (bounded), citing exactly that excerpt and — for code
// sources — proposing the same file as a code anchor. It makes NO quality
// judgement; it exists to prove the pipeline without a key.
type EchoIntentDrafter struct{ Max int }

// DraftIntents implements IntentDrafter.
func (e EchoIntentDrafter) DraftIntents(_ context.Context, excerpts []IntentExcerpt) ([]intentDraft, error) {
	max := e.Max
	if max <= 0 {
		max = 12
	}
	// Stable order so the echo output is deterministic.
	sort.SliceStable(excerpts, func(i, j int) bool { return excerpts[i].Citation < excerpts[j].Citation })
	var out []intentDraft
	for _, ex := range excerpts {
		if len(out) >= max {
			break
		}
		d := intentDraft{
			IntentID:        "intent." + sanitizeID(ex.Citation),
			Claim:           ex.Text,
			Category:        "operational_deployment",
			SourceCitations: []string{ex.Citation},
		}
		// For file-anchored code/schema/test sources, propose the file as an anchor
		// so GroundIntent has something to resolve.
		if strings.HasPrefix(ex.Citation, "file:") {
			d.CodeAnchors = []string{"file:" + citationToPath(ex.Citation)}
		}
		out = append(out, d)
	}
	return out, nil
}

// DraftAndCageIntents runs a proposer over the excerpts, applies the intent cage
// (every source citation must be a gathered excerpt), materializes the survivors
// into grounded-ready candidates, dedups by intent id, and caps at max. Returns
// the candidates and the count of cage-rejected drafts. It writes nothing.
func DraftAndCageIntents(ctx context.Context, dr IntentDrafter, excerpts []IntentExcerpt, max int) ([]IntentCandidate, int, error) {
	allowed, kind := excerptIndex(excerpts)
	drafts, err := dr.DraftIntents(ctx, excerpts)
	if err != nil {
		return nil, 0, err
	}
	var out []IntentCandidate
	seen := map[string]bool{}
	rejected := 0
	for _, d := range drafts {
		if len(validateIntentDraft(d, allowed)) > 0 {
			rejected++
			continue
		}
		c := materialize(d, kind)
		id := c.IntentID
		if id == "" {
			id = c.Claim
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, c)
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out, rejected, nil
}

// excerptIndex builds the lookup maps the cage and materialize need.
func excerptIndex(excerpts []IntentExcerpt) (allowed map[string]string, kind map[string]string) {
	allowed = make(map[string]string, len(excerpts))
	kind = make(map[string]string, len(excerpts))
	for _, e := range excerpts {
		allowed[e.Citation] = e.Text
		kind[e.Citation] = e.Kind
	}
	return allowed, kind
}
