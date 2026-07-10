// SPDX-License-Identifier: Apache-2.0

package skillimport

type Skill struct {
	Name        string
	Description string
	SourcePath  string
	Body        string
	Category    string
}

type CandidateKind string

const SkillIngestionCandidateSchema = "skill_ingestion_candidate"

const (
	CandidateImplementationPattern CandidateKind = "ImplementationPattern"
	CandidateInvariant             CandidateKind = "InvariantCandidate"
	CandidateForbiddenFix          CandidateKind = "ForbiddenFixCandidate"
	CandidateRequiredTest          CandidateKind = "RequiredTestCandidate"
	CandidateWorkflowContract      CandidateKind = "WorkflowContractCandidate"
	CandidateRequiredEvidence      CandidateKind = "RequiredEvidenceCandidate"
)

type SkillCandidate struct {
	ID                 string
	Class              string
	Label              string
	Status             string
	SourceSkill        string
	SourcePath         string
	Confidence         string
	Rationale          string
	WhenToUse          []string
	MustFollow         []string
	RequiredEvidence   []string
	ForbiddenShortcuts []string
	RequiredTests      []string
	ReferenceFiles     []ReferenceFile
	Tags               []string
}

type ReferenceFile struct {
	Path string
	Role string
}

type ImportOptions struct {
	InputRoot         string
	OutputDir         string
	Repo              string
	SourceSet         string
	DefaultStatus     string
	DefaultConfidence string
	IncludeDeprecated bool
}

type FileIssue struct {
	Path string
	Err  error
}

type DiscoverResult struct {
	Skills  []Skill
	Skipped []string
	Invalid []FileIssue
}

type WriteResult struct {
	Paths []string
}
