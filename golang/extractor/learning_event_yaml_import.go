// SPDX-License-Identifier: AGPL-3.0-only

package extractor

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/awareness-graph/golang/rdf"
)

type yamlLearningEventDoc struct {
	LearningEvent yamlLearningEvent `yaml:"learning_event"`
}

type yamlLearningEvent struct {
	ID                       string                         `yaml:"id"`
	Task                     string                         `yaml:"task"`
	Mode                     string                         `yaml:"mode"`
	Model                    string                         `yaml:"model"`
	RunSignature             string                         `yaml:"run_signature"`
	LearningEvidence         string                         `yaml:"learning_evidence"`
	LearningAllowed          *bool                          `yaml:"learning_allowed"`
	PromotionAllowed         *bool                          `yaml:"promotion_allowed"`
	CertificationStatus      string                         `yaml:"certification_status"`
	Certifiable              *bool                          `yaml:"certifiable"`
	GoverningContractID      string                         `yaml:"governing_contract_id"`
	HumanReviewRequired      *bool                          `yaml:"human_review_required"`
	MissingEvidence          []string                       `yaml:"missing_evidence"`
	PromotedLessonCandidates []string                       `yaml:"promoted_lesson_candidates"`
	Lesson                   string                         `yaml:"lesson"`
	Diagnosis                yamlLearningEventDiagnosis     `yaml:"diagnosis"`
	Decision                 yamlLearningEventDecision      `yaml:"decision"`
	Certification            yamlLearningEventCertification `yaml:"certification"`
	Current                  yamlLearningEventSnapshot      `yaml:"current"`
	Previous                 yamlLearningEventSnapshot      `yaml:"previous"`
}

type yamlLearningEventDiagnosis struct {
	PrimaryFailureMode string `yaml:"primary_failure_mode"`
}

type yamlLearningEventDecision struct {
	Action string `yaml:"action"`
}

type yamlLearningEventCertification struct {
	CertificationStatus string   `yaml:"certification_status"`
	Certifiable         *bool    `yaml:"certifiable"`
	GoverningContractID string   `yaml:"governing_contract_id"`
	HumanReviewRequired *bool    `yaml:"human_review_required"`
	MissingEvidence     []string `yaml:"missing_evidence"`
	Reason              string   `yaml:"reason"`
}

type yamlLearningEventSnapshot struct {
	Score int `yaml:"score"`
}

func importLearningEvent(e *rdf.Emitter, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	var doc yamlLearningEventDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("yaml parse: %w", err)
	}
	if doc.LearningEvent.ID == "" {
		return nil
	}

	ev := doc.LearningEvent
	subj := rdf.MintIRI(rdf.ClassLearningEvent, ev.ID)
	e.Typed(subj, rdf.ClassLearningEvent)
	e.Typed(subj, rdf.ClassOutcomeFeedback)

	e.Triple(subj, rdf.IRI(rdf.PropLabel), rdf.Lit(coalesce(ev.Task, ev.ID)))
	emitOptLit(e, subj, rdf.PropForTask, ev.Task)
	emitOptLit(e, subj, rdf.PropMode, ev.Mode)
	emitOptLit(e, subj, rdf.PropModelName, ev.Model)
	emitOptLit(e, subj, rdf.PropRunSignature, ev.RunSignature)
	emitOptLit(e, subj, rdf.PropLearningEvidence, ev.LearningEvidence)
	emitOptLit(e, subj, rdf.PropCertificationStatus, firstNonEmpty(ev.CertificationStatus, ev.Certification.CertificationStatus))
	emitOptLit(e, subj, rdf.PropPrimaryFailureMode, ev.Diagnosis.PrimaryFailureMode)
	emitOptLit(e, subj, rdf.PropDecision, ev.Decision.Action)
	emitOptInt(e, subj, rdf.PropCurrentScore, ev.Current.Score)
	emitOptInt(e, subj, rdf.PropPreviousScore, ev.Previous.Score)
	emitOptBool(e, subj, rdf.PropLearningAllowed, ev.LearningAllowed)
	emitOptBool(e, subj, rdf.PropPromotionAllowed, ev.PromotionAllowed)
	emitOptBool(e, subj, rdf.PropCertifiable, firstNonNilBool(ev.Certifiable, ev.Certification.Certifiable))
	emitOptBool(e, subj, rdf.PropHumanReviewRequired, firstNonNilBool(ev.HumanReviewRequired, ev.Certification.HumanReviewRequired))
	emitOptLits(e, subj, rdf.PropMissingEvidence, firstNonEmptySlice(ev.MissingEvidence, ev.Certification.MissingEvidence))

	comment := strings.TrimSpace(ev.Lesson)
	if reason := strings.TrimSpace(ev.Certification.Reason); reason != "" {
		if comment != "" {
			comment += "\n\n"
		}
		comment += reason
	}
	if comment != "" {
		e.Triple(subj, rdf.IRI(rdf.PropComment), rdf.Lit(comment))
	}
	e.Triple(subj, rdf.IRI(rdf.PropAuthoredIn), rdf.Lit(e.NormPath(path)))

	contractID := strings.TrimSpace(firstNonEmpty(ev.GoverningContractID, ev.Certification.GoverningContractID))
	if contractID != "" {
		contractIRI := rdf.MintIRI(rdf.ClassContract, contractID)
		e.Triple(subj, rdf.IRI(rdf.PropGovernedByContract), contractIRI)
		e.Triple(subj, rdf.IRI(rdf.PropUsedKnowledgeNode), contractIRI)
	}
	for _, ref := range ev.PromotedLessonCandidates {
		if iri, ok := knowledgeRefToIRI(strings.TrimSpace(ref)); ok {
			e.Triple(subj, rdf.IRI(rdf.PropUsedKnowledgeNode), iri)
		}
	}

	return nil
}

func emitOptBool(e *rdf.Emitter, subj, prop string, v *bool) {
	if v == nil {
		return
	}
	if *v {
		e.Triple(subj, rdf.IRI(prop), rdf.Lit("true"))
		return
	}
	e.Triple(subj, rdf.IRI(prop), rdf.Lit("false"))
}

func emitOptInt(e *rdf.Emitter, subj, prop string, v int) {
	if v == 0 {
		return
	}
	e.Triple(subj, rdf.IRI(prop), rdf.Lit(fmt.Sprintf("%d", v)))
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func firstNonEmptySlice(vals ...[]string) []string {
	for _, v := range vals {
		if len(v) > 0 {
			return v
		}
	}
	return nil
}

func firstNonNilBool(vals ...*bool) *bool {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}
