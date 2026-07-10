// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	awarenesspb "github.com/globulario/awareness-graph/golang/pb"
	"github.com/globulario/awareness-graph/golang/rdf"
)

// EditCheck evaluates a proposed edit against the active repo-scoped advisory
// rules ("detect" blocks) that apply to the file, in the caller's resolved
// domain. WARNING-ONLY: it never blocks and never edits code.
//
// Scope is delegated entirely to collectImpact, which resolves the domain, fails
// closed on a multi-domain graph with no domain, and returns only the in-scope
// nodes anchored to the file. So a Caddy rule can only warn for a Caddy file in
// the Caddy domain; it never reaches a Globular briefing, and vice versa.
func (s *server) EditCheck(ctx context.Context, req *awarenesspb.EditCheckRequest) (*awarenesspb.EditCheckResponse, error) {
	file := strings.TrimSpace(req.GetFile())
	if file == "" {
		return nil, status.Error(codes.InvalidArgument, "file is required")
	}
	if s.store == nil {
		return nil, status.Error(codes.Unavailable, "store is unavailable")
	}
	start := time.Now()
	content := req.GetProposedContent()

	// Scope + fail-closed handled by collectImpact (identical to Briefing).
	impact, _, _, err := s.collectImpact(ctx, file, strings.TrimSpace(req.GetDomain()))
	if err != nil {
		if _, ok := status.FromError(err); ok && status.Code(err) != codes.Unknown {
			return nil, err // preserve FailedPrecondition for an ambiguous scope
		}
		return nil, status.Errorf(codes.Unavailable, "backend query failed: %v", err)
	}

	// In-scope nodes that could carry a detect block (invariants + forbidden
	// fixes). Class is recorded for the warning.
	inScope := map[string]string{}
	for _, n := range impact.GetDirectInvariants() {
		inScope[n.GetId()] = "Invariant"
	}
	for _, n := range impact.GetForbiddenFixes() {
		inScope[n.GetId()] = "ForbiddenFix"
	}

	rules, err := s.loadDetectRules(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "detect rules query failed: %v", err)
	}

	ids := make([]string, 0, len(inScope))
	for id := range inScope {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var warnings []*awarenesspb.EditWarning
	evaluated := 0
	for _, id := range ids {
		rule, ok := rules[id]
		if !ok {
			continue
		}
		// applies_to_paths refinement: when set, the file must match a glob.
		if len(rule.AppliesToPaths) > 0 && !anyGlobMatch(rule.AppliesToPaths, file) {
			continue
		}
		evaluated++
		detail, tripped := rule.evaluate(content)
		if !tripped {
			continue
		}
		msg := rule.Message
		if msg == "" {
			msg = rule.Label
		}
		warnings = append(warnings, &awarenesspb.EditWarning{
			RuleId:      id,
			Class:       inScope[id],
			Severity:    "warning",
			Message:     msg,
			Detail:      detail,
			Provenance:  rule.Provenance.oneLine(),
			Enforcement: rule.enforcementLevel(),
		})
	}

	return &awarenesspb.EditCheckResponse{
		Warnings:       warnings,
		RulesEvaluated: int32(evaluated),
		GeneratedInMs:  time.Since(start).Milliseconds(),
	}, nil
}

// loadedDetectRule is the reified detect block for one node, plus the provenance
// shown alongside any warning it raises.
type loadedDetectRule struct {
	ID               string
	Label            string
	ForbiddenPattern string
	RequiredPattern  string
	AppliesToPaths   []string
	Message          string
	Enforcement      string // "warn" (default) | "block"
	Provenance       nodeProvenance
}

// enforcementLevel returns the rule's enforcement, defaulting to "warn".
func (r loadedDetectRule) enforcementLevel() string {
	if strings.EqualFold(strings.TrimSpace(r.Enforcement), "block") {
		return "block"
	}
	return "warn"
}

// evaluate runs the rule's patterns against the proposed content. It returns a
// human-readable detail and whether the rule tripped. An unparsable pattern is
// skipped (advisory only — a bad regexp must never break the check), not tripped.
func (r loadedDetectRule) evaluate(content string) (string, bool) {
	if r.ForbiddenPattern != "" {
		if re, err := regexp.Compile(r.ForbiddenPattern); err == nil && re.MatchString(content) {
			return "forbidden pattern matched: " + r.ForbiddenPattern, true
		}
	}
	if r.RequiredPattern != "" {
		if re, err := regexp.Compile(r.RequiredPattern); err == nil && !re.MatchString(content) {
			return "required pattern missing: " + r.RequiredPattern, true
		}
	}
	return "", false
}

// loadDetectRules reads every node carrying a detect block and reifies it,
// keyed by bare node id. Loaded fresh per call — detect rules are few and
// EditCheck is not a hot path, so a process cache is not worth the staleness
// risk.
func (s *server) loadDetectRules(ctx context.Context) (map[string]loadedDetectRule, error) {
	facts, err := s.store.DetectFacts(ctx)
	if err != nil {
		return nil, err
	}
	byNode := map[string]*loadedDetectRule{}
	provByNode := map[string]*nodeProvenance{}
	for _, f := range facts {
		r, ok := byNode[f.NodeIRI]
		if !ok {
			id, ok := awarenessIDFromIRI(f.NodeIRI)
			if !ok {
				continue
			}
			r = &loadedDetectRule{ID: id}
			byNode[f.NodeIRI] = r
			provByNode[f.NodeIRI] = &nodeProvenance{}
		}
		switch f.Predicate {
		case rdf.PropLabel:
			r.Label = f.Object
		case rdf.PropDetectForbiddenPattern:
			r.ForbiddenPattern = f.Object
		case rdf.PropDetectRequiredPattern:
			r.RequiredPattern = f.Object
		case rdf.PropDetectAppliesToPath:
			if s := strings.TrimSpace(f.Object); s != "" {
				r.AppliesToPaths = append(r.AppliesToPaths, s)
			}
		case rdf.PropDetectMessage:
			r.Message = f.Object
		case rdf.PropDetectEnforcement:
			r.Enforcement = f.Object
		}
		applyProvenanceFact(provByNode[f.NodeIRI], f.Predicate, f.Object)
	}
	out := make(map[string]loadedDetectRule, len(byNode))
	for iri, r := range byNode {
		if p, ok := provByNode[iri]; ok {
			r.Provenance = *p
		}
		out[r.ID] = *r
	}
	return out, nil
}

// anyGlobMatch reports whether file matches any of the globs. Supports `*`
// (within a path segment) and `**` (across segments).
func anyGlobMatch(globs []string, file string) bool {
	f := strings.TrimPrefix(strings.TrimSpace(file), "./")
	for _, g := range globs {
		if re := globToRegexp(strings.TrimSpace(g)); re != nil && re.MatchString(f) {
			return true
		}
	}
	return false
}

// globToRegexp converts a path glob into an anchored regexp. `**` matches any
// run including slashes; `*` matches within a single segment; `?` matches one
// non-slash char. Everything else is literal. Returns nil on an empty glob.
func globToRegexp(glob string) *regexp.Regexp {
	if glob == "" {
		return nil
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); i++ {
		switch glob[i] {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(glob[i])))
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil
	}
	return re
}
