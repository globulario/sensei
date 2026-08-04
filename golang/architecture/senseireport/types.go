// SPDX-License-Identifier: AGPL-3.0-only

// Package senseireport implements the canonical producer-side data model for
// SENSEI.md and SENSEI.report.json (docs/design/sensei-md-report.md). It
// holds only the typed shape, assembly, rendering, and validation of a
// report — Build gathers real Sensei sources; RenderMarkdown and
// encoding/json render the SAME already-built value to both output formats,
// never recomputing between them.
//
// This package must never invent a value with no honest source. A field
// with nothing to report is left at its zero value with a documented
// Limitations entry, never filled with a guessed or heuristic value — the
// same discipline dashboardprojection already established for
// sensei.dashboard.projection.v1.
//
// Identity is anchored on evaluated_commit (informational provenance) and
// evaluated_content_digest_sha256 (freshness-authoritative, computed by
// ContentDigest to deliberately exclude SENSEI.md and SENSEI.report.json
// (and .sensei/reports/**) — a committed report can never validly claim to match
// raw HEAD, since committing it changes HEAD to include the report itself).
// There is deliberately no wall-clock timestamp field anywhere in Report:
// that keeps Build a pure function of on-disk state, and makes determinism
// (identical inputs -> byte-identical Markdown and JSON) provable without
// special-casing a volatile field out of the comparison.
package senseireport

// SchemaVersion is the exact, adopted report schema identifier. Schema
// evolution is strict: any field addition, removal, rename, or semantic
// change requires a new identifier, and Validate rejects any other value.
const SchemaVersion = "sensei.report.v1"

// Disposition values for CurrentWork.Disposition — task-scoped only, never
// a repository-wide claim. See Verification.RepositoryWideVerification for
// the (deliberately unconditional, honest) repository-wide answer.
const (
	DispositionVerified   = "VERIFIED"
	DispositionBlocked    = "BLOCKED"
	DispositionUnverified = "UNVERIFIED"
	DispositionIncomplete = "INCOMPLETE"
)

// Freshness values for Verification.ReportFreshness — report-scoped only:
// "does the committed report still match the content it describes." STALE
// here never means "the task is stale"; CurrentWork.Disposition carries
// task staleness/blockedness through its own vocabulary above.
const (
	FreshnessCurrent = "CURRENT"
	FreshnessStale   = "STALE"
	FreshnessUnknown = "UNKNOWN"
)

// RepositoryWideVerificationNotRun is the only value
// Verification.RepositoryWideVerification ever takes in this schema
// version. Sensei's own code never executes go build/go test/any command
// against a target repository anywhere today (a deliberate architectural
// boundary, confirmed across the whole codebase, not an oversight here) —
// claiming a repository-wide verification result would be inventing a
// capability that does not exist, exactly what an earlier architect review
// flagged as a fabricated VERIFIED claim. A future schema version may add a
// real repository verification profile; this one does not pretend to.
const RepositoryWideVerificationNotRun = "NOT_RUN"

// Report is the top-level sensei.report.v1 document — the single value
// SENSEI.md and SENSEI.report.json are both rendered from.
type Report struct {
	SchemaVersion string       `json:"schema_version"`
	GeneratedBy   string       `json:"generated_by"`
	Identity      Identity     `json:"identity"`
	Summary       Summary      `json:"summary"`
	CurrentWork   CurrentWork  `json:"current_work"`
	Findings      []Finding    `json:"findings"`
	Verification  Verification `json:"verification"`
	Memory        Memory       `json:"memory"`
	Reproduction  Reproduction `json:"reproduction"`
	Limitations   []string     `json:"limitations,omitempty"`
}

// Identity binds the report to the exact repository state it describes.
type Identity struct {
	Repository Repository `json:"repository"`
	// EvaluatedCommit is the git HEAD SHA at generation time. Informational
	// provenance ONLY — never compared for freshness, because committing
	// SENSEI.md always advances HEAD to a commit that includes the report
	// itself, which would make every committed report immediately "stale"
	// against its own commit by construction.
	EvaluatedCommit string `json:"evaluated_commit"`
	// EvaluatedCommitStatus mirrors architecture.RevisionResolved /
	// RevisionNotGit / RevisionUnavailable — never fabricated when git
	// resolution fails.
	EvaluatedCommitStatus string `json:"evaluated_commit_status"`
	// EvaluatedContentDigestSHA256 is the freshness-authoritative identity:
	// a canonical digest of repository content EXCLUDING the generated
	// report outputs themselves (SENSEI.md, SENSEI.report.json,
	// .sensei/reports/**). sensei report --check compares against this
	// field, never against EvaluatedCommit.
	EvaluatedContentDigestSHA256 string `json:"evaluated_content_digest_sha256"`
}

// Repository field names deliberately match dashboardprojection.Repository
// (Key, DisplayName) for identity-vocabulary consistency with the existing
// sensei-dashboard product, without importing that package (this report has
// its own, simpler identity shape — see Identity's doc comment on why
// dashboardprojection.Revision does not fit here).
type Repository struct {
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
}

// Summary is a compact, factual count of meaningful state. It must never
// invent an aggregate quality or confidence score.
type Summary struct {
	BlockingFindings         int `json:"blocking_findings"`
	AdvisoryFindings         int `json:"advisory_findings"`
	CandidatesAwaitingReview int `json:"candidates_awaiting_review"`
}

// CurrentWork describes the active governed task, if any. Active == false
// is a normal, honestly-reported state, never papered over with a
// fabricated task.
type CurrentWork struct {
	Active bool `json:"active"`
	// Note carries the literal "no active task" when Active == false, and
	// is empty otherwise.
	Note              string   `json:"note,omitempty"`
	TaskID            string   `json:"task_id,omitempty"`
	Title             string   `json:"title,omitempty"`
	Disposition       string   `json:"disposition,omitempty"` // one of the Disposition* consts
	Scope             []string `json:"scope,omitempty"`
	Authority         string   `json:"authority,omitempty"`
	RemainingBlockers int      `json:"remaining_blockers"`
	PrimaryBlocker    string   `json:"primary_blocker,omitempty"`
}

// Finding is one material, top-level finding. The report must not dump the
// entire graph or every advisory observation here — only what materially
// affects maintainers (see Build's capping behavior).
type Finding struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"` // "blocking" | "advisory"
	Statement string   `json:"statement"`
	Files     []string `json:"files,omitempty"`
}

// Verification presents the evidence supporting CurrentWork.Disposition and
// the report's own freshness — each field scoped explicitly, never
// collapsed into one repository-wide word.
type Verification struct {
	// ReportFreshness answers "does this committed report still match the
	// content it describes" — one of the Freshness* consts.
	ReportFreshness string `json:"report_freshness"`
	// TaskReadiness/Obligations* are populated only when CurrentWork.Active
	// is true and completion.AssessReadiness could run.
	TaskReadiness        string `json:"task_readiness,omitempty"`
	ObligationsSatisfied int    `json:"obligations_satisfied,omitempty"`
	ObligationsTotal     int    `json:"obligations_total,omitempty"`
	// RepositoryWideVerification is always RepositoryWideVerificationNotRun
	// in this schema version. See that constant's doc comment.
	RepositoryWideVerification string `json:"repository_wide_verification"`
}

// Memory summarizes behavioral-memory candidates awaiting review
// (docs/awareness/candidates/**/*.yaml). It must never reproduce the full
// knowledge graph.
type Memory struct {
	CandidatesAwaitingReview int                `json:"candidates_awaiting_review"`
	Highlighted              []CandidateSummary `json:"highlighted,omitempty"`
	Limitations              []string           `json:"limitations,omitempty"`
}

// CandidateSummary is one behavioral-memory candidate, named but not
// reproduced in full.
type CandidateSummary struct {
	ID     string `json:"id"`
	Class  string `json:"class,omitempty"`
	Source string `json:"source"`
}

// Reproduction lists the exact commands needed to regenerate or validate
// the report. Fixed to sensei report / sensei report --check — never
// sensei verify, which does not exist.
type Reproduction struct {
	Commands []string `json:"commands"`
}
