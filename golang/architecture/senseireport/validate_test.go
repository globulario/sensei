// SPDX-License-Identifier: AGPL-3.0-only

package senseireport

import "testing"

func validReport() Report {
	return Report{
		SchemaVersion: SchemaVersion,
		Identity: Identity{
			Repository:                   Repository{Key: "example.com/repo", DisplayName: "repo"},
			EvaluatedCommitStatus:        "resolved",
			EvaluatedContentDigestSHA256: "deadbeef",
		},
		CurrentWork: CurrentWork{Active: false, Note: "no active task"},
		Verification: Verification{
			ReportFreshness:            FreshnessCurrent,
			RepositoryWideVerification: RepositoryWideVerificationNotRun,
		},
		Reproduction: Reproduction{Commands: []string{"sensei report", "sensei report --check"}},
	}
}

func TestValidateAcceptsWellFormedReport(t *testing.T) {
	if errs := Validate(validReport()); len(errs) != 0 {
		t.Fatalf("expected no errors, got %+v", errs)
	}
}

func TestValidateChecksSchemaVersionFirst(t *testing.T) {
	r := validReport()
	r.SchemaVersion = "sensei.report.v0"
	r.Identity = Identity{} // also invalid, but must not be reported
	errs := Validate(r)
	if len(errs) != 1 {
		t.Fatalf("expected exactly one error when schema_version is wrong, got %+v", errs)
	}
	if errs[0].Field != "schema_version" {
		t.Fatalf("expected schema_version to be checked first, got %+v", errs[0])
	}
}

func TestValidateRejectsFabricatedRepositoryWideVerification(t *testing.T) {
	r := validReport()
	r.Verification.RepositoryWideVerification = "VERIFIED"
	errs := Validate(r)
	found := false
	for _, e := range errs {
		if e.Field == "verification.repository_wide_verification" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a rejection of a non-NOT_RUN repository_wide_verification, got %+v", errs)
	}
}

func TestValidateRequiresNoteWhenInactive(t *testing.T) {
	r := validReport()
	r.CurrentWork.Note = ""
	errs := Validate(r)
	found := false
	for _, e := range errs {
		if e.Field == "current_work.note" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected current_work.note to be required when inactive, got %+v", errs)
	}
}

func TestValidateRequiresDispositionWhenActive(t *testing.T) {
	r := validReport()
	r.CurrentWork = CurrentWork{Active: true, TaskID: "task.1"}
	errs := Validate(r)
	found := false
	for _, e := range errs {
		if e.Field == "current_work.disposition" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected current_work.disposition to be required when active, got %+v", errs)
	}
}

func TestValidateRejectsEmptyReproductionCommands(t *testing.T) {
	r := validReport()
	r.Reproduction.Commands = nil
	errs := Validate(r)
	found := false
	for _, e := range errs {
		if e.Field == "reproduction.commands" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected reproduction.commands to be required, got %+v", errs)
	}
}
