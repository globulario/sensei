// SPDX-License-Identifier: AGPL-3.0-only

package plane

import (
	"io"
	"os"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/graphsnapshot"
	"github.com/globulario/sensei/golang/rdf"
)

func LoadGraphIndex(path string) (GraphIndex, error) {
	f, err := os.Open(path)
	if err != nil {
		return GraphIndex{}, err
	}
	defer f.Close()
	return ReadGraphIndex(f)
}

func ReadGraphIndex(r io.Reader) (GraphIndex, error) {
	triples, err := graphsnapshot.Read(r)
	if err != nil {
		return GraphIndex{}, err
	}
	nodes := map[string]GovernedNode{}
	classesBySubject := map[string]map[string]bool{}
	for _, t := range triples {
		if t.Predicate == rdf.PropType && t.ObjectIsIRI && indexedClass(t.Object) != "" {
			if classesBySubject[t.Subject] == nil {
				classesBySubject[t.Subject] = map[string]bool{}
			}
			classesBySubject[t.Subject][indexedClass(t.Object)] = true
		}
	}
	for iri, classes := range classesBySubject {
		class := firstIndexedClass(classes)
		nodes[iri] = GovernedNode{
			IRI:   iri,
			Class: class,
			ID:    nodeID(iri, class),
		}
	}
	for _, t := range triples {
		n, ok := nodes[t.Subject]
		if !ok {
			continue
		}
		switch t.Predicate {
		case rdf.PropLabel:
			if !t.ObjectIsIRI {
				n.Label = t.Object
			}
		case rdf.PropComment:
			if !t.ObjectIsIRI {
				n.Comment = t.Object
			}
		case rdf.PropStatus:
			if !t.ObjectIsIRI {
				n.Status = strings.ToLower(strings.TrimSpace(t.Object))
			}
		case rdf.PropAssertionMethod:
			if !t.ObjectIsIRI {
				n.AssertionMethod = strings.TrimSpace(t.Object)
			}
		case rdf.PropArchitecturalPlane:
			if !t.ObjectIsIRI {
				n.ArchitecturalPlane = strings.TrimSpace(t.Object)
			}
		case rdf.PropAuthoredIn:
			if !t.ObjectIsIRI {
				n.AuthoredIn = append(n.AuthoredIn, strings.TrimSpace(t.Object))
			}
		case rdf.PropSupersededBy:
			n.SupersededBy = append(n.SupersededBy, objectID(t))
		case rdf.PropSupportedByEvidence:
			n.SupportedByEvidence = append(n.SupportedByEvidence, objectID(t))
		case rdf.PropSupports:
			n.Supports = append(n.Supports, objectID(t))
		case rdf.PropRequiresTest:
			n.RequiresTests = append(n.RequiresTests, objectID(t))
		case rdf.PropProducedByTest:
			n.ProducedByTests = append(n.ProducedByTests, objectID(t))
		case rdf.PropFreshness:
			if !t.ObjectIsIRI {
				n.Freshness = strings.TrimSpace(t.Object)
			}
		case rdf.PropLastValidatedAt:
			if !t.ObjectIsIRI {
				n.LastValidatedAt = strings.TrimSpace(t.Object)
			}
		case rdf.PropSourcePath:
			if !t.ObjectIsIRI {
				n.SourcePath = strings.TrimSpace(t.Object)
			}
		}
		nodes[t.Subject] = normalizeNode(n)
	}
	return GraphIndex{Nodes: nodes}, nil
}

func VerifyGraphSnapshot(path, digest, status string) (string, bool, []Reason, error) {
	receipt, err := graphsnapshot.Verify(path, digest, status)
	if err != nil {
		return "", false, nil, err
	}
	reasons := make([]Reason, 0, len(receipt.Reasons))
	for _, r := range receipt.Reasons {
		code := strings.TrimPrefix(r.Code, "graphsnapshot")
		if code == r.Code {
			code = "." + r.Code
		}
		reasons = append(reasons, Reason{Code: "plane.graph" + code, Detail: r.Detail})
	}
	return receipt.DigestSHA256, receipt.Verified, reasons, nil
}

func indexedClass(iri string) string {
	switch iri {
	case rdf.ClassInvariant:
		return "invariant"
	case rdf.ClassContract:
		return "contract"
	case rdf.ClassDecision:
		return "decision"
	case rdf.ClassIntent, rdf.ClassDesignIntent, rdf.ClassOperationalIntent, rdf.ClassProductIntent, rdf.ClassConstraintIntent:
		return "intent"
	case rdf.ClassEvidence:
		return "evidence"
	case rdf.ClassRuntimeEvidence:
		return "runtime_evidence"
	case rdf.ClassTest:
		return "test"
	default:
		return ""
	}
}

func firstIndexedClass(classes map[string]bool) string {
	order := []string{"invariant", "contract", "decision", "intent", "evidence", "runtime_evidence", "test"}
	for _, c := range order {
		if classes[c] {
			return c
		}
	}
	return ""
}

func nodeID(iri, class string) string {
	prefix := ""
	switch class {
	case "invariant":
		prefix = mintPrefix(rdf.ClassInvariant)
	case "contract":
		prefix = mintPrefix(rdf.ClassContract)
	case "decision":
		prefix = mintPrefix(rdf.ClassDecision)
	case "intent":
		prefix = mintPrefix(rdf.ClassIntent)
	case "evidence":
		prefix = mintPrefix(rdf.ClassEvidence)
	case "runtime_evidence":
		prefix = mintPrefix(rdf.ClassRuntimeEvidence)
	case "test":
		prefix = mintPrefix(rdf.ClassTest)
	}
	if prefix != "" && strings.HasPrefix(iri, prefix) {
		return rdf.DecodeIRIPath(iri[len(prefix):])
	}
	if i := strings.LastIndex(iri, "/"); i >= 0 {
		return iri[i+1:]
	}
	if i := strings.LastIndex(iri, "#"); i >= 0 {
		return iri[i+1:]
	}
	return iri
}

func mintPrefix(classIRI string) string {
	return strings.TrimSuffix(strings.Trim(rdf.MintIRI(classIRI, ""), "<>"), "/") + "/"
}

func objectID(t graphsnapshot.Triple) string {
	if t.ObjectIsIRI {
		return nodeID(t.Object, indexedClassFromIRI(t.Object))
	}
	return strings.TrimSpace(t.Object)
}

func indexedClassFromIRI(iri string) string {
	for _, class := range []string{"invariant", "contract", "decision", "intent", "evidence", "runtime_evidence", "test"} {
		if nodeID(iri, class) != iri {
			return class
		}
	}
	return ""
}

func normalizeNode(n GovernedNode) GovernedNode {
	n.AuthoredIn = sortedUnique(n.AuthoredIn)
	n.SupersededBy = sortedUnique(n.SupersededBy)
	n.SupportedByEvidence = sortedUnique(n.SupportedByEvidence)
	n.Supports = sortedUnique(n.Supports)
	n.RequiresTests = sortedUnique(n.RequiresTests)
	n.ProducedByTests = sortedUnique(n.ProducedByTests)
	return n
}

func sortedUnique(in []string) []string {
	seen := map[string]bool{}
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			seen[strings.TrimSpace(s)] = true
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
