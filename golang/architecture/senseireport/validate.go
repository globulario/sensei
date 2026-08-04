// SPDX-License-Identifier: AGPL-3.0-only

package senseireport

import "fmt"

// ValidationError is one structural or referential defect found by
// Validate. It never represents a judgment about the repository's
// architecture -- only about the Report value's own internal consistency.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Validate checks a Report for internal structural and referential
// consistency. It never validates architectural correctness -- only that
// the document itself is well-formed enough to be trusted as a report.
// Schema version is checked first, matching dashboardprojection.Validate's
// convention: every other check is meaningless against a document that
// does not even claim to be this schema.
func Validate(r Report) []ValidationError {
	var errs []ValidationError

	if r.SchemaVersion != SchemaVersion {
		errs = append(errs, ValidationError{
			Field:   "schema_version",
			Message: fmt.Sprintf("must be %q, got %q", SchemaVersion, r.SchemaVersion),
		})
		return errs
	}

	if r.Identity.Repository.Key == "" {
		errs = append(errs, ValidationError{Field: "identity.repository.key", Message: "must not be empty"})
	}
	if r.Identity.EvaluatedCommitStatus == "" {
		errs = append(errs, ValidationError{Field: "identity.evaluated_commit_status", Message: "must not be empty"})
	}
	if r.Identity.EvaluatedContentDigestSHA256 == "" {
		errs = append(errs, ValidationError{Field: "identity.evaluated_content_digest_sha256", Message: "must not be empty"})
	}

	switch r.Verification.ReportFreshness {
	case FreshnessCurrent, FreshnessStale, FreshnessUnknown:
	default:
		errs = append(errs, ValidationError{
			Field:   "verification.report_freshness",
			Message: fmt.Sprintf("must be one of CURRENT|STALE|UNKNOWN, got %q", r.Verification.ReportFreshness),
		})
	}

	if r.Verification.RepositoryWideVerification != RepositoryWideVerificationNotRun {
		errs = append(errs, ValidationError{
			Field:   "verification.repository_wide_verification",
			Message: fmt.Sprintf("this schema version only ever reports %q, got %q", RepositoryWideVerificationNotRun, r.Verification.RepositoryWideVerification),
		})
	}

	if r.CurrentWork.Active {
		switch r.CurrentWork.Disposition {
		case DispositionVerified, DispositionBlocked, DispositionUnverified, DispositionIncomplete:
		default:
			errs = append(errs, ValidationError{
				Field:   "current_work.disposition",
				Message: fmt.Sprintf("active task must have one of VERIFIED|BLOCKED|UNVERIFIED|INCOMPLETE, got %q", r.CurrentWork.Disposition),
			})
		}
		if r.CurrentWork.TaskID == "" {
			errs = append(errs, ValidationError{Field: "current_work.task_id", Message: "active task must name a task_id"})
		}
	} else {
		if r.CurrentWork.Note == "" {
			errs = append(errs, ValidationError{Field: "current_work.note", Message: "an inactive current_work must state a note (e.g. \"no active task\"), never leave the reader guessing"})
		}
		if r.CurrentWork.Disposition != "" {
			errs = append(errs, ValidationError{Field: "current_work.disposition", Message: "must be empty when no task is active"})
		}
	}

	for i, f := range r.Findings {
		if f.Kind != "blocking" && f.Kind != "advisory" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("findings[%d].kind", i),
				Message: fmt.Sprintf("must be \"blocking\" or \"advisory\", got %q", f.Kind),
			})
		}
		if f.Statement == "" {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("findings[%d].statement", i), Message: "must not be empty"})
		}
	}

	if len(r.Reproduction.Commands) == 0 {
		errs = append(errs, ValidationError{Field: "reproduction.commands", Message: "must not be empty"})
	}

	return errs
}
