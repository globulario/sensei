// SPDX-License-Identifier: AGPL-3.0-only

package inference

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/rdf"
)

type GovernedDirectionOptions struct {
	Root      string
	GraphPath string
	Binding   architecture.ClaimDocumentBinding
}

const (
	governedDirectionRuleID        = "rule.governed_direction_record.v1"
	governedDirectionExtractor     = "governed_direction_graph_extractor"
	governedDirectionInactiveScope = "governed_direction_graph_input"
)

type governedDirectionNode struct {
	IRI                string
	ID                 string
	Class              string
	Label              string
	Status             string
	ArchitecturalPlane string
	Files              []string
	Symbols            []string
	AuthoredIn         []string
}

func LoadGovernedDirectionFacts(opts GovernedDirectionOptions) ([]architecture.Fact, []architecture.Limitation, error) {
	path := strings.TrimSpace(opts.GraphPath)
	if path == "" {
		return nil, []architecture.Limitation{{
			Source:   "governed_direction",
			Scope:    governedDirectionInactiveScope,
			Reason:   "governed direction bridge inactive: --graph-nt not supplied",
			Blocking: false,
		}}, nil
	}
	if !architecture.RepositoryRevisionResolved(opts.Binding) && !architecture.RepositoryTreeResolved(opts.Binding) {
		return nil, nil, fmt.Errorf("governed direction graph input requires resolved repository revision or tree binding")
	}
	if opts.Binding.GraphDigestStatus != architecture.GraphDigestResolved || opts.Binding.GraphDigestSHA256 == "" {
		return nil, nil, fmt.Errorf("governed direction graph input requires resolved graph digest binding")
	}
	receipt, err := graphsnapshot.Verify(path, opts.Binding.GraphDigestSHA256, opts.Binding.GraphDigestStatus)
	if err != nil {
		return nil, nil, err
	}
	if !receipt.Verified {
		return nil, nil, fmt.Errorf("governed direction graph snapshot verification failed: %s", receipt.Reasons[0].Detail)
	}
	triples, err := graphsnapshot.Load(path)
	if err != nil {
		return nil, nil, err
	}
	nodes := parseGovernedDirectionNodes(triples)
	var facts []architecture.Fact
	var limitations []architecture.Limitation
	for _, n := range nodes {
		if reason, ok := governedDirectionRejection(n); ok {
			limitations = append(limitations, architecture.Limitation{
				Source:   classQualifiedRef(n.Class, n.ID),
				Scope:    "governed_direction_record",
				Reason:   reason,
				Blocking: false,
			})
			continue
		}
		if !eligibleGovernedDirectionNode(n) {
			continue
		}
		statement := governedDirectionStatement(n)
		fact, _ := architecture.NewFact(architecture.Fact{
			Kind:      "governed_direction",
			Subject:   n.ID,
			Predicate: statement.Predicate,
			Object:    statement.Object,
			Scope: architecture.Scope{
				Repository: opts.Binding.RepositoryDomain,
				Files:      append([]string{}, n.Files...),
				Symbols:    append([]string{}, n.Symbols...),
			},
			Evidence: architecture.Evidence{
				SourceFile: primaryAuthoredIn(n.AuthoredIn),
			},
			Confidence: 1.0,
			Extractor:  governedDirectionExtractor,
			Meta: map[string]string{
				"about_node":        classQualifiedRef(n.Class, n.ID),
				"governed_node_id":  n.ID,
				"governed_node_iri": n.IRI,
				"governed_class":    n.Class,
				"graph_digest":      opts.Binding.GraphDigestSHA256,
				"authored_in":       strings.Join(n.AuthoredIn, ","),
				"target_scope":      governedDirectionScopeKey(n.Files, n.Symbols),
			},
		}, architecture.Options{
			Root:                   strings.TrimSpace(opts.Root),
			RepositoryDomain:       opts.Binding.RepositoryDomain,
			RepositoryDomainStatus: architecture.RepositoryDomainResolved,
			Revision:               opts.Binding.Revision,
			RevisionStatus:         opts.Binding.RevisionStatus,
			SourceKind:             "governed_authored_awareness",
		})
		facts = append(facts, fact)
	}
	return facts, append(limitations, architecture.Limitation{
		Source:   "governed_direction",
		Scope:    governedDirectionInactiveScope,
		Reason:   "governed direction bridge active: verified graph snapshot consumed",
		Blocking: false,
	}), nil
}

type GovernedDirectionRecordRule struct{}

func (GovernedDirectionRecordRule) Descriptor() RuleDescriptor {
	return RuleDescriptor{
		ID:                  governedDirectionRuleID,
		Version:             "v1",
		Title:               "Governed direction record",
		Description:         "Projects governed desired and intended architectural records into canonical task-consumable directional claims.",
		RequiredFactKinds:   []string{"governed_direction"},
		RequiredPredicates:  []string{"defines_desired_direction_for_scope", "defines_intended_direction_for_scope"},
		OutputPlane:         "desired|intended",
		OutputPredicate:     "defines_desired_direction_for_scope|defines_intended_direction_for_scope",
		ConfidencePolicy:    "governed current record on a verified graph snapshot yields full confidence",
		HumanReviewRequired: true,
		KnownLimitations: []string{
			"The governed record expresses architectural direction, not observed implementation conformance.",
			"Accepted dialogue alone is not sufficient; the record must already be governed and present in the verified graph snapshot.",
		},
	}
}

func (GovernedDirectionRecordRule) Apply(ctx Context) ([]Application, error) {
	var applications []Application
	for _, fact := range ctx.Facts {
		if fact.Kind != "governed_direction" {
			continue
		}
		planeName := ""
		switch fact.Predicate {
		case "defines_desired_direction_for_scope":
			planeName = architecture.PlaneDesired
		case "defines_intended_direction_for_scope":
			planeName = architecture.PlaneIntended
		default:
			continue
		}
		status, unknowns := statusForPremises(ctx, []architecture.Fact{fact})
		claim := baseClaim(governedDirectionRuleID, planeName, architecture.ClaimStatement{
			Subject: fact.Subject, Predicate: fact.Predicate, Object: fact.Object,
		}, []architecture.Fact{fact}, status, append(unknowns,
			"The governed record expresses architectural direction, not observed implementation conformance.",
		), 1.0)
		claim.AboutNodes = governedDirectionAboutNodes(fact)
		claim.Description = "Deterministically derived from a governed architectural direction record on a verified graph snapshot."
		claim.InvalidationConditions = []string{
			"The governed record disappears, changes status, or changes architectural plane.",
			"The governed record scope anchors change.",
			"The verified graph digest changes.",
			"The repository revision changes.",
			"The inference-rule version changes.",
		}
		claim.AlternativeExplanations = []string{
			"The implementation may still diverge from the governed direction.",
		}
		applications = append(applications, Application{
			RuleID:         claim.InferenceRule,
			GroupKey:       fact.Subject + "|" + planeName,
			PremiseFactIDs: claim.PremiseFacts,
			Claim:          claim,
		})
	}
	return applications, nil
}

func MarkGovernedDirectionConflicts(claims []architecture.Claim) []architecture.Claim {
	out := append([]architecture.Claim{}, claims...)
	groups := map[string][]int{}
	for i, claim := range out {
		if claim.InferenceRule != governedDirectionRuleID {
			continue
		}
		key := strings.Join([]string{
			claim.ArchitecturalPlane,
			claim.Statement.Predicate,
			governedDirectionScopeKey(claim.Scope.Files, claim.Scope.Symbols),
		}, "\x1f")
		groups[key] = append(groups[key], i)
	}
	for _, idxs := range groups {
		values := map[string]bool{}
		for _, idx := range idxs {
			values[out[idx].Statement.Object] = true
		}
		if len(values) < 2 {
			continue
		}
		for _, idx := range idxs {
			claim := out[idx]
			claim.EpistemicStatus = architecture.StatusContested
			claim.ConflictsWith = append(claim.ConflictsWith, conflictingGovernedDirectionClaims(out, idxs, idx)...)
			claim.Unknowns = dedupeStrings(append(claim.Unknowns,
				"Multiple governed direction records disagree for the same architectural scope.",
			))
			out[idx] = claim
		}
	}
	return out
}

func parseGovernedDirectionNodes(triples []graphsnapshot.Triple) []governedDirectionNode {
	nodes := map[string]*governedDirectionNode{}
	typeSet := map[string]map[string]bool{}
	for _, t := range triples {
		if t.Predicate != rdf.PropType || !t.ObjectIsIRI {
			continue
		}
		class := governedDirectionIndexedClass(t.Object)
		if class == "" {
			continue
		}
		if typeSet[t.Subject] == nil {
			typeSet[t.Subject] = map[string]bool{}
		}
		typeSet[t.Subject][class] = true
	}
	for subject, classes := range typeSet {
		class := firstGovernedDirectionClass(classes)
		if class == "" {
			continue
		}
		nodes[subject] = &governedDirectionNode{
			IRI:   subject,
			ID:    governedDirectionNodeID(subject, class),
			Class: class,
		}
	}
	for _, t := range triples {
		n := nodes[t.Subject]
		if n == nil {
			continue
		}
		switch t.Predicate {
		case rdf.PropLabel:
			if !t.ObjectIsIRI {
				n.Label = strings.TrimSpace(t.Object)
			}
		case rdf.PropStatus:
			if !t.ObjectIsIRI {
				n.Status = strings.ToLower(strings.TrimSpace(t.Object))
			}
		case rdf.PropArchitecturalPlane:
			if !t.ObjectIsIRI {
				n.ArchitecturalPlane = strings.ToLower(strings.TrimSpace(t.Object))
			}
		case rdf.PropAuthoredIn:
			if !t.ObjectIsIRI {
				n.AuthoredIn = append(n.AuthoredIn, normalizeFileAnchor(t.Object))
			}
		case rdf.PropExpressedBy, rdf.PropAnchoredIn:
			if t.ObjectIsIRI {
				if file, ok := sourceFilePathFromIRI(t.Object); ok {
					n.Files = append(n.Files, file)
				} else if symbol, ok := codeSymbolIDFromIRI(t.Object); ok {
					n.Symbols = append(n.Symbols, symbol)
				}
			}
		}
	}
	var out []governedDirectionNode
	for _, n := range nodes {
		n.Files = dedupeSorted(n.Files)
		n.Symbols = dedupeSorted(n.Symbols)
		n.AuthoredIn = dedupeSorted(n.AuthoredIn)
		out = append(out, *n)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Class == out[j].Class {
			return out[i].ID < out[j].ID
		}
		return out[i].Class < out[j].Class
	})
	return out
}

func eligibleGovernedDirectionNode(n governedDirectionNode) bool {
	if !currentGovernedStatus(n.Status) {
		return false
	}
	if len(n.Files)+len(n.Symbols) == 0 {
		return false
	}
	switch n.ArchitecturalPlane {
	case architecture.PlaneDesired:
		return n.Class == "intent" || n.Class == "contract" || n.Class == "decision"
	case architecture.PlaneIntended:
		return n.Class == "intent" || n.Class == "contract" || n.Class == "decision" || n.Class == "invariant"
	default:
		return false
	}
}

func governedDirectionRejection(n governedDirectionNode) (string, bool) {
	if !currentGovernedStatus(n.Status) {
		return "", false
	}
	switch n.ArchitecturalPlane {
	case architecture.PlaneDesired, architecture.PlaneIntended:
	default:
		return "", false
	}
	if !planeAllowsGovernedDirectionClass(n.ArchitecturalPlane, n.Class) {
		return fmt.Sprintf("governed %s record has unsupported class %s for %s plane", n.ArchitecturalPlane, n.Class, n.ArchitecturalPlane), true
	}
	if len(n.Files)+len(n.Symbols) == 0 {
		return fmt.Sprintf("governed %s record lacks represented source_file or code_symbol anchors", n.ArchitecturalPlane), true
	}
	if len(n.AuthoredIn) == 0 {
		return fmt.Sprintf("governed %s record lacks authoredIn source provenance", n.ArchitecturalPlane), true
	}
	return "", false
}

func governedDirectionStatement(n governedDirectionNode) architecture.ClaimStatement {
	label := strings.TrimSpace(n.Label)
	if label == "" {
		label = n.ID
	}
	predicate := "defines_intended_direction_for_scope"
	if n.ArchitecturalPlane == architecture.PlaneDesired {
		predicate = "defines_desired_direction_for_scope"
	}
	return architecture.ClaimStatement{
		Subject:   n.ID,
		Predicate: predicate,
		Object:    label,
	}
}

func planeAllowsGovernedDirectionClass(plane, class string) bool {
	switch plane {
	case architecture.PlaneDesired:
		return class == "intent" || class == "contract" || class == "decision"
	case architecture.PlaneIntended:
		return class == "intent" || class == "contract" || class == "decision" || class == "invariant"
	default:
		return false
	}
}

func currentGovernedStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "accepted", "active", "approved", "current":
		return true
	default:
		return false
	}
}

func governedDirectionIndexedClass(iri string) string {
	switch iri {
	case rdf.ClassInvariant:
		return "invariant"
	case rdf.ClassContract:
		return "contract"
	case rdf.ClassDecision:
		return "decision"
	case rdf.ClassIntent, rdf.ClassDesignIntent, rdf.ClassOperationalIntent, rdf.ClassProductIntent, rdf.ClassConstraintIntent:
		return "intent"
	default:
		return ""
	}
}

func firstGovernedDirectionClass(classes map[string]bool) string {
	for _, class := range []string{"contract", "decision", "intent", "invariant"} {
		if classes[class] {
			return class
		}
	}
	return ""
}

func governedDirectionNodeID(iri, class string) string {
	var prefix string
	switch class {
	case "invariant":
		prefix = mintPrefix(rdf.ClassInvariant)
	case "contract":
		prefix = mintPrefix(rdf.ClassContract)
	case "decision":
		prefix = mintPrefix(rdf.ClassDecision)
	case "intent":
		prefix = mintPrefix(rdf.ClassIntent)
	}
	if prefix != "" && strings.HasPrefix(iri, prefix) {
		return rdf.DecodeIRIPath(iri[len(prefix):])
	}
	if idx := strings.LastIndexAny(iri, "/#"); idx >= 0 && idx+1 < len(iri) {
		return iri[idx+1:]
	}
	return iri
}

func classQualifiedRef(class, id string) string {
	return class + ":" + id
}

func sourceFilePathFromIRI(iri string) (string, bool) {
	prefix := mintPrefix(rdf.ClassSourceFile)
	if !strings.HasPrefix(iri, prefix) {
		return "", false
	}
	path := normalizeFileAnchor(rdf.DecodeIRIPath(iri[len(prefix):]))
	if path == "" {
		return "", false
	}
	return path, true
}

func codeSymbolIDFromIRI(iri string) (string, bool) {
	prefix := mintPrefix(rdf.ClassCodeSymbol)
	if !strings.HasPrefix(iri, prefix) {
		return "", false
	}
	id := strings.TrimSpace(rdf.DecodeIRIPath(iri[len(prefix):]))
	if id == "" {
		return "", false
	}
	return id, true
}

func normalizeFileAnchor(path string) string {
	path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(path))))
	if path == "." || path == "" || strings.HasPrefix(path, "../") || path == ".." || filepath.IsAbs(path) {
		return ""
	}
	return path
}

func dedupeSorted(in []string) []string {
	seen := map[string]bool{}
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item != "" {
			seen[item] = true
		}
	}
	out := make([]string, 0, len(seen))
	for item := range seen {
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func mintPrefix(classIRI string) string {
	return strings.TrimSuffix(strings.TrimPrefix(rdf.MintIRI(classIRI, ""), "<"), ">")
}

func primaryAuthoredIn(paths []string) string {
	paths = dedupeSorted(paths)
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

func governedDirectionScopeKey(files, symbols []string) string {
	return strings.Join([]string{
		strings.Join(dedupeSorted(files), ","),
		strings.Join(dedupeSorted(symbols), ","),
	}, "|")
}

func governedDirectionAboutNodes(fact architecture.Fact) []string {
	if ref := strings.TrimSpace(fact.Meta["about_node"]); ref != "" {
		return []string{ref}
	}
	return nil
}

func conflictingGovernedDirectionClaims(claims []architecture.Claim, idxs []int, self int) []string {
	var out []string
	for _, idx := range idxs {
		if idx == self {
			continue
		}
		if claims[idx].Statement.Object == claims[self].Statement.Object {
			continue
		}
		out = append(out, claims[idx].ID)
	}
	sort.Strings(out)
	return dedupeStrings(out)
}
