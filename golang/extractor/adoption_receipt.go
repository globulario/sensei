// SPDX-License-Identifier: AGPL-3.0-only

package extractor

import (
	"github.com/globulario/sensei/golang/architecture/adoption"
	"github.com/globulario/sensei/golang/rdf"
)

// emitAdoptionReceipt is the only RDF mapping for adoption provenance. Long
// evidence bodies remain in YAML; the graph carries compact, queryable receipt
// fields and exact source identifiers.
func emitAdoptionReceipt(e *rdf.Emitter, subj, class, id string, raw adoption.Receipt, supersededBy ...string) error {
	receipt := adoption.Normalize(raw)
	if err := adoption.ValidateValues(receipt); err != nil {
		return err
	}

	emitOptLit(e, subj, rdf.PropStatus, receipt.Status)
	emitOptLit(e, subj, rdf.PropPromotionStatus, receipt.PromotionStatus)
	emitOptLit(e, subj, rdf.PropAssertionOrigin, receipt.AssertionOrigin)
	emitOptLit(e, subj, rdf.PropEpistemicStatus, receipt.EpistemicStatus)
	emitOptLit(e, subj, rdf.PropDecisionActor, receipt.DecisionActor)
	emitOptLit(e, subj, rdf.PropDecisionContext, receipt.DecisionContext)
	emitOptLit(e, subj, rdf.PropDecisionPolicy, receipt.DecisionPolicy)
	emitOptLit(e, subj, rdf.PropDecisionTimestamp, receipt.DecisionTimestamp)
	emitOptLit(e, subj, rdf.PropValidForCommit, receipt.ValidForRevision)
	emitOptLit(e, subj, rdf.PropValidForGraphDigest, receipt.ValidForGraphDigest)
	emitOptLit(e, subj, rdf.PropReviewStatus, receipt.ReviewStatus)
	for _, value := range receipt.AdoptionBasis {
		emitOptLit(e, subj, rdf.PropAdoptionBasis, value)
	}
	for _, value := range receipt.SourceReceipts {
		emitOptLit(e, subj, rdf.PropSourcePath, value)
	}
	for _, value := range receipt.CorroborationKinds {
		emitOptLit(e, subj, rdf.PropCorroborationKind, value)
	}
	for _, value := range receipt.RevocationConditions {
		emitOptLit(e, subj, rdf.PropRevocationCondition, value)
	}

	if receipt.ArchitecturalPlane == "" {
		return nil
	}
	if receipt.Status == adoption.PromotionMachineAdopted || receipt.PromotionStatus == adoption.PromotionMachineAdopted {
		emitOptLit(e, subj, rdf.PropArchitecturalPlane, receipt.ArchitecturalPlane)
		return nil
	}
	return emitGovernedArchitecturalPlane(e, subj, class, id, receipt.Status, receipt.ArchitecturalPlane, supersededBy)
}
