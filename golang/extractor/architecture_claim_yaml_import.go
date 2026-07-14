// SPDX-License-Identifier: Apache-2.0

package extractor

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/architecture"
	"github.com/globulario/sensei/golang/rdf"
)

type architectureClaimsEnvelope struct {
	ArchitectureClaims architecture.ClaimDocument `yaml:"architecture_claims"`
}

func importArchitectureClaims(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var env architectureClaimsEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	doc, err := architecture.NormalizeClaimDocument(env.ArchitectureClaims)
	if err != nil {
		return fmt.Errorf("validate architecture_claims: %w", err)
	}
	for _, c := range doc.Claims {
		emitArchitectureClaim(e, path, doc.Binding, c)
	}
	return nil
}

func emitArchitectureClaim(e *rdf.Emitter, path string, binding architecture.ClaimDocumentBinding, c architecture.Claim) {
	subj := rdf.MintIRI(rdf.ClassArchitectureClaim, c.ID)
	e.Typed(subj, rdf.ClassArchitectureClaim)
	e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(c.Label, c.ID)))
	emitOptLit(e, subj, rdf.PropComment, c.Description)
	e.Triple(subj, rdf.IRI(rdf.PropClaimSubject), rdf.Lit(c.Statement.Subject))
	e.Triple(subj, rdf.IRI(rdf.PropClaimPredicate), rdf.Lit(c.Statement.Predicate))
	e.Triple(subj, rdf.IRI(rdf.PropClaimObject), rdf.Lit(c.Statement.Object))
	e.Triple(subj, rdf.IRI(rdf.PropArchitecturalPlane), rdf.Lit(c.ArchitecturalPlane))
	e.Triple(subj, rdf.IRI(rdf.PropAssertionOrigin), rdf.Lit(c.AssertionOrigin))
	e.Triple(subj, rdf.IRI(rdf.PropEpistemicStatus), rdf.Lit(c.EpistemicStatus))
	emitOptLit(e, subj, rdf.PropGeneratedByInferenceRule, c.InferenceRule)
	for _, id := range c.PremiseFacts {
		e.Triple(subj, rdf.IRI(rdf.PropDerivedFromFact), rdf.Lit(id))
	}
	for _, id := range c.DependsOnClaims {
		e.Triple(subj, rdf.IRI(rdf.PropDependsOnClaim), rdf.MintIRI(rdf.ClassArchitectureClaim, id))
	}
	for _, ref := range c.SupportingEvidence {
		e.Triple(subj, rdf.IRI(rdf.PropSupportedByEvidence), evidenceRefIRI(ref))
	}
	for _, ref := range c.RefutingEvidence {
		e.Triple(subj, rdf.IRI(rdf.PropRefutedByEvidence), evidenceRefIRI(ref))
	}
	for _, id := range c.ConflictsWith {
		e.Triple(subj, rdf.IRI(rdf.PropConflictsWith), rdf.MintIRI(rdf.ClassArchitectureClaim, id))
	}
	if c.SupersededBy != "" {
		e.Triple(subj, rdf.IRI(rdf.PropSupersededBy), rdf.MintIRI(rdf.ClassArchitectureClaim, c.SupersededBy))
	}
	for _, ref := range c.AboutNodes {
		if iri, ok := claimReferenceIRI(ref); ok {
			e.Triple(subj, rdf.IRI(rdf.PropAboutNode), iri)
		}
	}
	for _, s := range c.AlternativeExplanations {
		e.Triple(subj, rdf.IRI(rdf.PropHasAlternativeExplanation), rdf.Lit(s))
	}
	for _, s := range c.Unknowns {
		e.Triple(subj, rdf.IRI(rdf.PropHasUnknown), rdf.Lit(s))
	}
	for _, s := range c.InvalidationConditions {
		e.Triple(subj, rdf.IRI(rdf.PropHasInvalidationCondition), rdf.Lit(s))
	}
	emitOptLit(e, subj, rdf.PropValidForCommit, binding.Revision)
	emitOptLit(e, subj, rdf.PropValidForGraphDigest, binding.GraphDigestSHA256)
	e.Triple(subj, rdf.IRI(rdf.PropConfidenceScore), rdf.Lit(strconv.FormatFloat(c.Confidence, 'f', -1, 64)))
	emitOptLit(e, subj, rdf.PropFreshness, c.Freshness)
	emitOptLit(e, subj, rdf.PropLastValidatedAt, c.LastValidatedAt)
	e.Triple(subj, rdf.IRI(rdf.PropHumanReviewRequired), rdf.Lit(strconv.FormatBool(c.HumanReviewRequired)))
	e.Triple(subj, rdf.IRI(rdf.PropPromotionStatus), rdf.Lit(c.PromotionStatus))
	e.Triple(subj, rdf.IRI(rdf.PropSourceKind), rdf.Lit("generated_candidate"))
	e.Triple(subj, rdf.IRI(rdf.PropSourcePath), rdf.Lit(e.NormPath(path)))
	e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))
	if c.Scope.Domain != "" {
		e.Triple(subj, rdf.IRI(rdf.PropDomain), rdf.Lit(c.Scope.Domain))
	}
	repo := coalesce(c.Scope.Repository, c.Scope.Repo, binding.RepositoryDomain)
	if repo != "" {
		e.Triple(subj, rdf.IRI(rdf.PropRepo), rdf.Lit(repo))
	}
	emitOptLit(e, subj, rdf.PropSourceSet, c.Scope.SourceSet)
	emitClaimAnchors(e, subj, c.Scope.Files, c.Scope.Symbols)
}

func emitClaimAnchors(e *rdf.Emitter, subj string, files, symbols []string) {
	for _, f := range files {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		ensureNode(e, rdf.ClassSourceFile, f)
		e.Triple(subj, rdf.IRI(rdf.PropAnchoredIn), rdf.MintIRI(rdf.ClassSourceFile, f))
	}
	for _, s := range symbols {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		ensureNode(e, rdf.ClassCodeSymbol, s)
		e.Triple(subj, rdf.IRI(rdf.PropAnchoredIn), rdf.MintIRI(rdf.ClassCodeSymbol, s))
	}
}

func evidenceRefIRI(ref string) string {
	class, id, ok := architecture.ParseClassQualifiedReference(ref)
	if ok && class == "evidence" {
		return rdf.MintIRI(rdf.ClassEvidence, id)
	}
	return rdf.MintIRI(rdf.ClassEvidence, ref)
}

func claimReferenceIRI(ref string) (string, bool) {
	class, id, ok := architecture.ParseClassQualifiedReference(ref)
	if !ok {
		return "", false
	}
	switch class {
	case "invariant":
		return rdf.MintIRI(rdf.ClassInvariant, id), true
	case "failure_mode":
		return rdf.MintIRI(rdf.ClassFailureMode, id), true
	case "incident_pattern":
		return rdf.MintIRI(rdf.ClassIncidentPattern, id), true
	case "intent":
		return rdf.MintIRI(rdf.ClassIntent, id), true
	case "symbol":
		return rdf.MintIRI(rdf.ClassSymbol, id), true
	case "source_file":
		return rdf.MintIRI(rdf.ClassSourceFile, id), true
	case "code_symbol":
		return rdf.MintIRI(rdf.ClassCodeSymbol, id), true
	case "forbidden_fix":
		return rdf.MintIRI(rdf.ClassForbiddenFix, id), true
	case "test":
		return rdf.MintIRI(rdf.ClassTest, id), true
	case "meta_principle":
		return rdf.MintIRI(rdf.ClassInvariant, id), true
	case "component":
		return rdf.MintIRI(rdf.ClassComponent, id), true
	case "boundary":
		return rdf.MintIRI(rdf.ClassBoundary, id), true
	case "contract":
		return rdf.MintIRI(rdf.ClassContract, id), true
	case "decision":
		return rdf.MintIRI(rdf.ClassDecision, id), true
	case "evidence":
		return rdf.MintIRI(rdf.ClassEvidence, id), true
	case "proof_obligation":
		return rdf.MintIRI(rdf.ClassProofObligation, id), true
	case "proof_slot":
		return rdf.MintIRI(rdf.ClassProofSlot, id), true
	case "design_pattern":
		return rdf.MintIRI(rdf.ClassDesignPattern, id), true
	case "implementation_pattern":
		return rdf.MintIRI(rdf.ClassImplementationPattern, id), true
	case "pattern_misuse":
		return rdf.MintIRI(rdf.ClassPatternMisuse, id), true
	case "architecture_claim":
		return rdf.MintIRI(rdf.ClassArchitectureClaim, id), true
	case "open_question":
		return rdf.MintIRI(rdf.ClassOpenQuestion, id), true
	case "architect_answer":
		return rdf.MintIRI(rdf.ClassArchitectAnswer, id), true
	case "evidence_probe":
		return rdf.MintIRI(rdf.ClassEvidenceProbe, id), true
	case "runtime_evidence":
		return rdf.MintIRI(rdf.ClassRuntimeEvidence, id), true
	case "authority_domain":
		return rdf.MintIRI(rdf.ClassAuthorityDomain, id), true
	case "authority_surface":
		return rdf.MintIRI(rdf.ClassAuthoritySurface, id), true
	case "state_object":
		return rdf.MintIRI(rdf.ClassStateObject, id), true
	case "repair_plan":
		return rdf.MintIRI(rdf.ClassRepairPlan, id), true
	default:
		return "", false
	}
}
