// SPDX-License-Identifier: AGPL-3.0-only

package main

// The second boundary: citation verified is not proposition established.
//
// Design doc §8d. verifyEvidenceRefs establishes a SOURCE FACT -- at commit C
// file F contains these bytes. It says nothing about whether those bytes mean
// what the claim says. The B specimen cites entirely real lines and draws the
// wrong architectural conclusion from them, and it passes evidence
// verification exactly as the true claim does. So EVIDENCE_VERIFIED alone may
// not carry a candidate into canonical YAML: the state after a verified
// citation is `candidate + evidence verified + NOT ESTABLISHED`, and it stays
// there until something the claimant does not control establishes P.
//
// Two things can. A registered derivation, re-run HERE against the promotion
// base, that returns DERIVED for the candidate's own typed proposition. Or an
// existing governed-authority entry -- canonical, non-candidate status -- that
// the candidate names as governing it, which is a human decision already
// taken. There is no flag. A claimant-controlled `--established` would be the
// self-approval this boundary exists to refuse.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/architecture/derive"
)

// establishmentVerdict is what crossed, or did not cross, the second boundary.
type establishmentVerdict string

const (
	establishedByDerivation establishmentVerdict = "ESTABLISHED_BY_DERIVATION"
	establishedByAuthority  establishmentVerdict = "ESTABLISHED_BY_GOVERNED_AUTHORITY"
	notEstablished          establishmentVerdict = "NOT_ESTABLISHED"
)

type establishment struct {
	Verdict establishmentVerdict
	Detail  string
}

// deriveForPromotion is the deriver the gate runs. A seam for tests only; the
// default is the real thing against the real pinned tree.
var deriveForPromotion = func(ctx context.Context, repoDir, revision string, p derive.Proposition) (derive.Outcome, string) {
	src, err := derive.NewGitSource(ctx, repoDir, repoDir, revision)
	if err != nil {
		return derive.Unknown, err.Error()
	}
	receipt, _ := derive.Derive(src, p, time.Now())
	return receipt.Outcome, fmt.Sprintf("%s/%s at %s: %s", receipt.DerivationID, receipt.DerivationVersion,
		shortCommit(receipt.Commit), receipt.Detail)
}

// establishCandidate asks what, if anything, establishes the proposition.
//
// The candidate may NAME a derivation or a governing entry. It may not supply
// the answer: the derivation is re-run here against the promotion base, and
// the governing entry must exist canonically in the corpus.
func establishCandidate(ctx context.Context, repoDir, awarenessDir string, candidate map[string]interface{}) establishment {
	if d, ok := candidate["derivation"].(map[string]interface{}); ok {
		p := derive.Proposition{
			Kind: derive.Kind(strFieldVal(d, "kind")), Dir: strFieldVal(d, "dir"),
			Type: strFieldVal(d, "type"), Field: strFieldVal(d, "field"), Lock: strFieldVal(d, "lock"),
		}
		base := promotionBase(repoDir)
		if base == "" {
			return establishment{notEstablished, "a derivation was named but no promotion base exists to run it against"}
		}
		outcome, detail := deriveForPromotion(ctx, repoDir, base, p)
		if outcome == derive.Derived {
			return establishment{establishedByDerivation, detail}
		}
		return establishment{notEstablished, fmt.Sprintf("the named derivation did not establish the proposition: %s -- %s", outcome, detail)}
	}
	if id := strFieldVal(candidate, "governed_by"); id != "" {
		if governedEntryExists(awarenessDir, id) {
			return establishment{establishedByAuthority, "governed by canonical entry " + id}
		}
		return establishment{notEstablished, fmt.Sprintf("governed_by names %q, which is not a canonical, non-candidate entry in %s", id, awarenessDir)}
	}
	return establishment{notEstablished, "nothing independent establishes the proposition: name a `derivation` " +
		"(re-run here against the promotion base) or a `governed_by` canonical entry; verified citations alone do not cross this boundary"}
}

// governedEntryExists reports a canonical, non-candidate entry with this id
// anywhere in the corpus. Read with the same entry-as-a-unit parser domain
// closure uses, so field order cannot confuse it.
func governedEntryExists(awarenessDir, id string) bool {
	id = strings.TrimSpace(id)
	if i := strings.Index(id, ":"); i >= 0 {
		id = id[i+1:] // accept class-qualified ids
	}
	found := false
	_ = filepath.Walk(awarenessDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || found || info.IsDir() || (!strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml")) {
			return nil
		}
		if strings.Contains(path, string(filepath.Separator)+"candidates"+string(filepath.Separator)) {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		_, ids, noncanonical := entryIDsAndStatus(data)
		for _, have := range ids {
			if have == id && !noncanonical[have] {
				found = true
			}
		}
		return nil
	})
	return found
}

// recordNotEstablished writes the refused state INTO the candidate, so it is
// represented rather than merely printed. The candidate stays a candidate; its
// provenance now says what was checked and what was not crossed.
func recordNotEstablished(candidatePath string, candidate map[string]interface{}, ev evidenceResult, est establishment) error {
	prov, _ := candidate["provenance"].(map[string]interface{})
	if prov == nil {
		prov = map[string]interface{}{}
	}
	prov["evidence_verification"] = fmt.Sprintf("%s: %s", ev.Verdict, ev.Detail)
	prov["establishment"] = fmt.Sprintf("%s: %s", est.Verdict, est.Detail)
	prov["last_promotion_attempt"] = time.Now().UTC().Format(time.RFC3339)
	candidate["provenance"] = prov
	return rewriteCandidateEntry(candidatePath, candidate)
}

// entryIDsAndStatus reads each YAML list entry as a unit -- id and status
// paired only after the whole entry is read, so field order cannot attach a
// status to the wrong identity.
//
// DUPLICATE of topLevelKeyIDsAndStatus in globulario/sensei#311, carried here
// so this change does not depend on an unmerged PR. Collapse to one once both
// have landed.
var promoteNonCanonicalStatus = map[string]bool{
	"candidate": true,
}

func entryIDsAndStatus(data []byte) (string, []string, map[string]bool) {
	var top string
	var ids []string
	noncanonical := map[string]bool{}

	// Each list entry is read AS A UNIT: its fields are gathered between one
	// `- ` boundary and the next, and only then are id and status paired. The
	// first version read the file as a stream and attached a status to the
	// last id seen, so an entry whose `status:` preceded its own `id:` handed
	// that status to the PREVIOUS entry -- which both re-created the defect
	// (the candidate stayed required) and excused a canonical neighbour from
	// closure. Field order inside an entry is not a fact about the entry, and
	// a neighbour's formatting must never change an entry's obligation.
	type entry struct {
		id, status string
	}
	var entries []entry
	var cur *entry
	entryIndent := -1
	flush := func() {
		if cur != nil {
			entries = append(entries, *cur)
		}
		cur = nil
	}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, " \t\r")
		if line == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if top == "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "-") &&
			strings.HasSuffix(strings.TrimSpace(line), ":") {
			top = strings.TrimSuffix(strings.TrimSpace(line), ":")
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "- ") || t == "-" {
			// A new entry begins at the list's own indentation. A dash deeper
			// than that is a nested list inside the current entry.
			if entryIndent < 0 || indent <= entryIndent {
				flush()
				entryIndent = indent
				cur = &entry{}
			}
			t = strings.TrimSpace(strings.TrimPrefix(t, "-"))
			indent += 2 // the field column for `- key: value`
		}
		if cur == nil {
			continue
		}
		// Only the entry's OWN fields: those at the entry's field column.
		// Anything deeper belongs to a nested block (a relation, an evidence
		// list) and must not be read as the entry's id or status.
		if indent != entryIndent+2 {
			continue
		}
		switch {
		case strings.HasPrefix(t, "id:"):
			v := strings.Trim(strings.TrimSpace(strings.TrimPrefix(t, "id:")), `"'`)
			if v != "" && !strings.ContainsAny(v, " {}[]") && cur.id == "" {
				cur.id = v
			}
		case strings.HasPrefix(t, "status:"):
			if cur.status == "" {
				cur.status = strings.ToLower(strings.Trim(strings.TrimSpace(strings.TrimPrefix(t, "status:")), `"'`))
			}
		}
	}
	flush()
	for _, e := range entries {
		if e.id == "" {
			continue
		}
		ids = append(ids, e.id)
		if promoteNonCanonicalStatus[e.status] {
			noncanonical[e.id] = true
		}
	}
	return top, ids, noncanonical
}
