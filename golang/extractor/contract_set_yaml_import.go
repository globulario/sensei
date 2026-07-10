// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness-graph/golang/rdf"
)

type frozenContractSet struct {
	Version    int              `yaml:"contract_set_version"`
	TaskID     string           `yaml:"task_id"`
	Repo       string           `yaml:"repo"`
	BaseCommit string           `yaml:"base_commit"`
	Contracts  []frozenContract `yaml:"contracts"`
}

type frozenContract struct {
	ID                  string              `yaml:"id"`
	Kind                string              `yaml:"kind"`
	Confidence          string              `yaml:"confidence"`
	Statement           string              `yaml:"statement"`
	Governs             contractGovernsY    `yaml:"governs"`
	Detect              contractDetectY     `yaml:"detect"`
	RequiredScope       contractScopeY      `yaml:"required_scope"`
	AllowedRelatedScope contractScopeY      `yaml:"allowed_related_scope"`
	OutOfScope          contractScopeY      `yaml:"out_of_scope"`
	RequiredPaths       []requiredPathY     `yaml:"required_paths"`
	MustNotChange       []string            `yaml:"must_not_change"`
	ScopeConfidence     contractConfidenceY `yaml:"scope_confidence"`
	Proof               contractProofY      `yaml:",inline"`
	AWGAnchors          []string            `yaml:"awg_anchors"`
	Invariants          []string            `yaml:"invariants"`
	FailureModes        []string            `yaml:"failure_modes"`
	Intents             []string            `yaml:"intents"`
	RequiredTests       []string            `yaml:"required_tests"`
	Components          []string            `yaml:"components"`
}

type contractGovernsY struct {
	Files         []string `yaml:"files"`
	Symbols       []string `yaml:"symbols"`
	Invariants    []string `yaml:"invariants"`
	FailureModes  []string `yaml:"failure_modes"`
	Intents       []string `yaml:"intents"`
	RequiredTests []string `yaml:"required_tests"`
	Components    []string `yaml:"components"`
}

type contractScopeY struct {
	Files    []string `yaml:"files"`
	Behavior []string `yaml:"behavior"`
}

type contractDetectY struct {
	Type    string `yaml:"type"`
	Pattern string `yaml:"pattern"`
	Message string `yaml:"message"`
}

type contractConfidenceY struct {
	ScopePrecision        string `yaml:"scope_precision"`
	RequiredPathsCoverage string `yaml:"required_paths_coverage"`
}

type contractProofY struct {
	ProofRequired       bool     `yaml:"proof_required"`
	RequiredTestPaths   []string `yaml:"required_test_paths"`
	RequiredTestSymbols []string `yaml:"required_test_symbols"`
	NoNewTestsMeans     string   `yaml:"no_new_tests_means"`
}

type requiredPathY struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
	Severity    string `yaml:"severity,omitempty"`
}

func (p *requiredPathY) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var description string
		if err := node.Decode(&description); err != nil {
			return err
		}
		p.Description = description
		return nil
	case yaml.MappingNode:
		type raw requiredPathY
		var decoded raw
		if err := node.Decode(&decoded); err != nil {
			return err
		}
		*p = requiredPathY(decoded)
		return nil
	default:
		return fmt.Errorf("required_paths entry must be string or mapping")
	}
}

func importFrozenContractSet(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	var set frozenContractSet
	if err := yaml.Unmarshal(data, &set); err != nil {
		return fmt.Errorf("yaml parse: %w", err)
	}

	for _, c := range set.Contracts {
		if c.ID == "" {
			continue
		}
		subj := rdf.MintIRI(rdf.ClassContract, c.ID)
		e.Typed(subj, rdf.ClassContract)

		e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(c.Statement, c.ID)))
		emitOptLit(e, subj, rdf.PropKind, c.Kind)
		emitOptLit(e, subj, rdf.PropConfidence, c.Confidence)
		emitOptLit(e, subj, rdf.PropForTask, set.TaskID)
		emitOptLit(e, subj, rdf.PropSourcePath, set.BaseCommit)
		emitOptLit(e, subj, rdf.PropSourceSet, "eval/multi-swe-bench/contracts")
		emitOptLit(e, subj, rdf.PropOrigin, "coldsource")
		if repo := strings.TrimSpace(set.Repo); repo != "" {
			e.Triple(subj, rdf.IRI(rdf.PropDomain), rdf.Lit(rdf.DomainRepo))
			e.Triple(subj, rdf.IRI(rdf.PropRepo), rdf.Lit(repo))
		}
		e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

		emitContractDetect(e, subj, c.Detect, c.Governs.Files)
		emitOptLits(e, subj, rdf.PropRequiresVerification, c.RequiredScope.Behavior)
		emitOptLits(e, subj, rdf.PropActivationTrigger, c.AllowedRelatedScope.Behavior)
		emitOptLits(e, subj, rdf.PropCoversPath, c.OutOfScope.Files)
		emitOptLits(e, subj, rdf.PropMustFollow, c.MustNotChange)
		if c.Proof.ProofRequired {
			e.Triple(subj, rdf.IRI(rdf.PropProofRequired), rdf.Lit("true"))
		}
		emitOptLit(e, subj, rdf.PropNoNewTestsMeans, c.Proof.NoNewTestsMeans)
		emitOptLits(e, subj, rdf.PropRequiredTestPath, c.Proof.RequiredTestPaths)
		emitOptLits(e, subj, rdf.PropRequiredTestSymbol, c.Proof.RequiredTestSymbols)

		for _, rp := range c.RequiredPaths {
			text := strings.TrimSpace(rp.Description)
			if text == "" {
				text = strings.TrimSpace(rp.ID)
			}
			if text != "" {
				e.Triple(subj, rdf.IRI(rdf.PropRequiresVerification), rdf.Lit(text))
			}
			slotID := contractRequiredPathSlotID(c.ID, rp)
			if slotID == "" {
				continue
			}
			slotIRI := rdf.MintIRI(rdf.ClassProofSlot, slotID)
			e.Typed(slotIRI, rdf.ClassProofSlot)
			e.Triple(slotIRI, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(text, slotID)))
			emitOptLit(e, slotIRI, rdf.PropSlotKind, "required_path")
			emitOptLit(e, slotIRI, rdf.PropSeverity, rp.Severity)
			e.Triple(slotIRI, rdf.IRI(rdf.PropRequired), rdf.Lit("true"))
			e.Triple(slotIRI, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
			e.Triple(subj, rdf.IRI(rdf.PropRequiresProofSlot), slotIRI)
		}
		if scope := renderContractScopeConfidence(c.ScopeConfidence); scope != "" {
			e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(scope))
		}

		emitContractFileAnchors(e, subj, append(
			append(
				append([]string{}, c.Governs.Files...),
				c.RequiredScope.Files...,
			),
			append(c.AllowedRelatedScope.Files, c.Proof.RequiredTestPaths...)...,
		))
		emitSpineAnchors(e, subj, nil, c.Governs.Symbols)
		emitContractKnowledgeAnchors(e, subj, c)
	}

	return nil
}

var nonProofSlotIDChars = regexp.MustCompile(`[^a-z0-9]+`)

func contractRequiredPathSlotID(contractID string, rp requiredPathY) string {
	contractID = strings.TrimSpace(contractID)
	if contractID == "" {
		return ""
	}
	part := strings.TrimSpace(rp.ID)
	if part == "" {
		part = strings.TrimSpace(rp.Description)
	}
	part = proofSlotSlug(part)
	if part == "" {
		return ""
	}
	return "slot.contract." + contractID + "." + part
}

func proofSlotSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = nonProofSlotIDChars.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	return s
}

func emitContractDetect(e *rdf.Emitter, subj string, d contractDetectY, governsFiles []string) {
	if strings.TrimSpace(d.Type) != "regex_forbidden" {
		return
	}
	emitDetect(e, subj, detectRule{
		AppliesToPaths:   governsFiles,
		ForbiddenPattern: d.Pattern,
		Message:          d.Message,
	})
}

func emitContractFileAnchors(e *rdf.Emitter, subj string, files []string) {
	for _, f := range uniqueStrings(files) {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if looksLikeGlob(f) {
			e.Triple(subj, rdf.IRI(rdf.PropCoversPath), rdf.Lit(f))
			continue
		}
		fileSubj := rdf.MintIRI(rdf.ClassSourceFile, f)
		e.Typed(fileSubj, rdf.ClassSourceFile)
		e.Triple(subj, rdf.IRI(rdf.PropExpressedBy), fileSubj)
		e.Triple(fileSubj, rdf.IRI(rdf.PropImplements), subj)
	}
}

func looksLikeGlob(s string) bool {
	return strings.ContainsAny(s, "*?[")
}

func uniqueStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func renderContractScopeConfidence(c contractConfidenceY) string {
	var parts []string
	if s := strings.TrimSpace(c.ScopePrecision); s != "" {
		parts = append(parts, "scope_precision: "+s)
	}
	if s := strings.TrimSpace(c.RequiredPathsCoverage); s != "" {
		parts = append(parts, "required_paths_coverage: "+s)
	}
	return strings.Join(parts, "\n")
}

func emitContractKnowledgeAnchors(e *rdf.Emitter, subj string, c frozenContract) {
	emitRefOrBareEdges(e, subj, rdf.PropConstrainedByInvariant, rdf.ClassInvariant, append(append([]string{}, c.Invariants...), c.Governs.Invariants...))
	emitRefOrBareEdges(e, subj, rdf.PropAffects, rdf.ClassFailureMode, append(append([]string{}, c.FailureModes...), c.Governs.FailureModes...))
	emitRefOrBareEdges(e, subj, rdf.PropRelatedTo, rdf.ClassIntent, append(append([]string{}, c.Intents...), c.Governs.Intents...))
	emitRefOrBareEdges(e, subj, rdf.PropRequiresTest, rdf.ClassTest, append(append([]string{}, c.RequiredTests...), c.Governs.RequiredTests...))
	emitRefOrBareEdges(e, subj, rdf.PropRelatedTo, rdf.ClassComponent, append(append([]string{}, c.Components...), c.Governs.Components...))

	for _, raw := range c.AWGAnchors {
		ref := strings.TrimSpace(raw)
		if ref == "" {
			continue
		}
		iri, ok := knowledgeRefToIRI(ref)
		if !ok {
			continue
		}
		switch anchorClass(ref) {
		case "invariant", "meta_principle":
			e.Triple(subj, rdf.IRI(rdf.PropConstrainedByInvariant), iri)
		case "failure_mode":
			e.Triple(subj, rdf.IRI(rdf.PropAffects), iri)
		case "test", "required_test":
			e.Triple(subj, rdf.IRI(rdf.PropRequiresTest), iri)
		case "component":
			e.Triple(subj, rdf.IRI(rdf.PropRelatedTo), iri)
		default:
			e.Triple(subj, rdf.IRI(rdf.PropRelatedTo), iri)
		}
	}
}

func anchorClass(ref string) string {
	if i := strings.IndexByte(ref, ':'); i >= 0 {
		return strings.TrimSpace(ref[:i])
	}
	return ""
}
