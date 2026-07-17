// SPDX-License-Identifier: Apache-2.0

package inference

import (
	"fmt"
	"sort"

	"github.com/globulario/sensei/golang/architecture"
)

func BuildClaimDocument(ctx Context, applications []Application) (architecture.ClaimDocument, error) {
	factByID := map[string]architecture.Fact{}
	for _, f := range ctx.Facts {
		factByID[f.ID] = f
	}
	used := map[string]bool{}
	claims := make([]architecture.Claim, 0, len(applications))
	limitations := relevantLimitations(ctx.Limitations, nil)
	if !architecture.RepositoryRevisionResolved(ctx.Binding) && !architecture.RepositoryTreeResolved(ctx.Binding) {
		limitations = append(limitations, architecture.Limitation{Source: "repository", Scope: "revision", Reason: "repository revision is not resolved", Blocking: true})
	}
	if !architecture.RepositoryGraphResolved(ctx.Binding) {
		limitations = append(limitations, architecture.Limitation{Source: "graph", Scope: "graph_digest", Reason: "graph digest is not resolved", Blocking: true})
	}
	for _, app := range applications {
		c := app.Claim
		var premiseFacts []architecture.Fact
		for _, id := range c.PremiseFacts {
			f, ok := factByID[id]
			if !ok {
				return architecture.ClaimDocument{}, fmt.Errorf("unknown premise fact %s", id)
			}
			used[id] = true
			premiseFacts = append(premiseFacts, f)
			if f.Provenance == nil {
				limitations = append(limitations, architecture.Limitation{Source: f.ID, Scope: "fact_provenance", Reason: "premise fact lacks explicit provenance", Blocking: true})
			}
		}
		status, unknowns := statusForPremises(ctx, premiseFacts)
		if status == architecture.StatusUnknown {
			c.EpistemicStatus = architecture.StatusUnknown
			c.Unknowns = dedupeStrings(append(c.Unknowns, unknowns...))
			c.Freshness = ""
		}
		claims = append(claims, c)
	}
	ids := sortedKeys(used)
	receipts := make([]architecture.ClaimFactReceipt, 0, len(ids))
	for _, id := range ids {
		f := factByID[id]
		prov := architecture.Provenance{
			RepositoryDomainStatus: architecture.RepositoryDomainUnknown,
			RevisionStatus:         architecture.RevisionUnavailable,
			SourceDigestStatus:     architecture.SourceDigestUnavailable,
			SourceKind:             "unknown",
		}
		if f.Provenance != nil {
			prov = *f.Provenance
		}
		receipts = append(receipts, architecture.ClaimFactReceipt{Fact: f, Provenance: prov})
	}
	doc := architecture.ClaimDocument{
		SchemaVersion: "1",
		GeneratedBy:   "sensei infer-claims",
		Binding:       ctx.Binding,
		FactReceipts:  receipts,
		Claims:        claims,
		Limitations:   dedupeLimitations(limitations),
	}
	return architecture.NormalizeClaimDocument(doc)
}

func relevantLimitations(limitations []architecture.Limitation, sources map[string]bool) []architecture.Limitation {
	var out []architecture.Limitation
	for _, lim := range limitations {
		if sources == nil || lim.Blocking || sources[lim.Source] {
			out = append(out, lim)
		}
	}
	return out
}

func dedupeLimitations(in []architecture.Limitation) []architecture.Limitation {
	seen := map[string]architecture.Limitation{}
	var keys []string
	for _, lim := range in {
		key := fmt.Sprintf("%s\x00%s\x00%s\x00%t", lim.Source, lim.Scope, lim.Reason, lim.Blocking)
		if _, ok := seen[key]; !ok {
			seen[key] = lim
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	out := make([]architecture.Limitation, 0, len(keys))
	for _, key := range keys {
		out = append(out, seen[key])
	}
	return out
}
