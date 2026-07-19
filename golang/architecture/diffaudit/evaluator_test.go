// SPDX-License-Identifier: Apache-2.0

package diffaudit

import (
	"context"
	"testing"
)

type fakeChecker struct {
	checkFileFunc     func(ctx context.Context, file, content, domain string) ([]AuditFinding, error)
	getFileImpactFunc func(ctx context.Context, file, domain string) ([]Requirement, []Requirement, []string, error)
	readBaseFileFunc  func(ctx context.Context, path string) (string, bool, error)
}

func (f *fakeChecker) CheckFile(ctx context.Context, file, content, domain string) ([]AuditFinding, error) {
	if f.checkFileFunc != nil {
		return f.checkFileFunc(ctx, file, content, domain)
	}
	return nil, nil
}

func (f *fakeChecker) GetFileImpact(ctx context.Context, file, domain string) ([]Requirement, []Requirement, []string, error) {
	if f.getFileImpactFunc != nil {
		return f.getFileImpactFunc(ctx, file, domain)
	}
	return nil, nil, nil, nil
}

func (f *fakeChecker) ReadBaseFile(ctx context.Context, path string) (string, bool, error) {
	if f.readBaseFileFunc != nil {
		return f.readBaseFileFunc(ctx, path)
	}
	return "1\n2\n3\n4\n5\n6\n7\n8\n9\n func main() {\n \tfmt.Println(\"hello\")\n }", true, nil
}

func TestEvaluateDiff_PassesCleanDiff(t *testing.T) {
	parsed, err := ParseDiff(sampleValidDiff, DefaultParseOptions())
	if err != nil {
		t.Fatalf("ParseDiff: %v", err)
	}

	res, err := EvaluateDiff(context.Background(), parsed, &fakeChecker{}, AuditOptions{})
	if err != nil {
		t.Fatalf("EvaluateDiff: %v", err)
	}
	if res.Decision != DecisionPass {
		t.Errorf("expected DecisionPass, got %s. ReasonCodes: %+v, Limitations: %+v, Findings: %+v", res.Decision, res.ReasonCodes, res.Limitations, res.Findings)
	}
	if res.Digest == "" {
		t.Error("digest is empty")
	}
	if err := res.Validate(); err != nil {
		t.Errorf("Validate() failed: %v", err)
	}
}

func TestEvaluateDiff_NilCheckerForcesCannotVerify(t *testing.T) {
	parsed, err := ParseDiff(sampleValidDiff, DefaultParseOptions())
	if err != nil {
		t.Fatalf("ParseDiff: %v", err)
	}

	res, err := EvaluateDiff(context.Background(), parsed, nil, AuditOptions{})
	if err != nil {
		t.Fatalf("EvaluateDiff: %v", err)
	}
	if res.Decision != DecisionCannotVerify || res.Availability != AvailabilityCannotVerify {
		t.Errorf("expected cannot_verify for nil checker, got decision=%s, availability=%s", res.Decision, res.Availability)
	}
	if err := res.Validate(); err != nil {
		t.Errorf("Validate() failed: %v", err)
	}
}

func TestEvaluateDiff_OmittedCompanionFileObligation(t *testing.T) {
	parsed, err := ParseDiff(sampleValidDiff, DefaultParseOptions())
	if err != nil {
		t.Fatalf("ParseDiff: %v", err)
	}

	checker := &fakeChecker{
		getFileImpactFunc: func(_ context.Context, file, domain string) ([]Requirement, []Requirement, []string, error) {
			if file == "cmd/main.go" {
				return []Requirement{{ID: "test-1", Path: "cmd/main_test.go"}}, nil, nil, nil
			}
			return nil, nil, nil, nil
		},
	}

	res, err := EvaluateDiff(context.Background(), parsed, checker, AuditOptions{})
	if err != nil {
		t.Fatalf("EvaluateDiff: %v", err)
	}
	if res.Decision != DecisionReview {
		t.Errorf("expected DecisionReview for missing companion test file, got %s", res.Decision)
	}
	if len(res.Findings) == 0 || res.Findings[0].RecordID != "test-1" {
		t.Errorf("expected test-1 finding, got %+v", res.Findings)
	}
	if err := res.Validate(); err != nil {
		t.Errorf("Validate() failed: %v", err)
	}
}

func TestEvaluateDiff_HunkOverlapError(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,1 +1,1 @@
-old1
+new1
@@ -1,1 +1,1 @@
-old1
+new2
`
	parsed, err := ParseDiff(diff, DefaultParseOptions())
	if err != nil {
		t.Fatalf("ParseDiff: %v", err)
	}

	checker := &fakeChecker{
		readBaseFileFunc: func(_ context.Context, _ string) (string, bool, error) {
			return "old1\n", true, nil
		},
	}

	res, err := EvaluateDiff(context.Background(), parsed, checker, AuditOptions{})
	if err != nil {
		t.Fatalf("EvaluateDiff: %v", err)
	}
	if res.Decision != DecisionCannotVerify || res.Availability != AvailabilityCannotVerify {
		t.Errorf("expected cannot_verify due to hunk overlap, got decision=%s, availability=%s", res.Decision, res.Availability)
	}
}

func TestEvaluateDiff_OmittedCompanionImplementationFile(t *testing.T) {
	// A contract file is modified in the diff
	diff := `diff --git a/docs/contracts/auth.yaml b/docs/contracts/auth.yaml
--- a/docs/contracts/auth.yaml
+++ b/docs/contracts/auth.yaml
@@ -1,1 +1,2 @@
 # auth contract
+version: v2
`
	parsed, err := ParseDiff(diff, DefaultParseOptions())
	if err != nil {
		t.Fatalf("ParseDiff: %v", err)
	}

	checker := &fakeChecker{
		getFileImpactFunc: func(_ context.Context, file, domain string) ([]Requirement, []Requirement, []string, error) {
			if file == "docs/contracts/auth.yaml" {
				return nil, []Requirement{{
					ID:           "contract.auth",
					Path:         "docs/contracts/auth.yaml",
					RelatedPaths: []string{"golang/auth/auth.go"},
				}}, nil, nil
			}
			return nil, nil, nil, nil
		},
		readBaseFileFunc: func(_ context.Context, _ string) (string, bool, error) {
			return "# auth contract\n", true, nil
		},
	}

	res, err := EvaluateDiff(context.Background(), parsed, checker, AuditOptions{})
	if err != nil {
		t.Fatalf("EvaluateDiff: %v", err)
	}
	if res.Decision != DecisionBlock {
		t.Errorf("expected DecisionBlock due to missing companion implementation file, got %s", res.Decision)
	}
	if len(res.Findings) == 0 || res.Findings[0].RecordID != "contract.auth" {
		t.Errorf("expected contract.auth block finding, got %+v", res.Findings)
	}
}

func TestEvaluateDiff_DeletedGovernedTarget(t *testing.T) {
	// An implementation file is deleted in the diff
	diff := `diff --git a/golang/auth/auth.go b/golang/auth/auth.go
deleted file mode 100644
--- a/golang/auth/auth.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package auth
-func Authenticate() {}
`
	parsed, err := ParseDiff(diff, DefaultParseOptions())
	if err != nil {
		t.Fatalf("ParseDiff: %v", err)
	}

	checker := &fakeChecker{
		getFileImpactFunc: func(_ context.Context, file, domain string) ([]Requirement, []Requirement, []string, error) {
			if file == "golang/auth/auth.go" {
				return nil, []Requirement{{
					ID:           "contract.auth",
					Path:         "docs/contracts/auth.yaml",
					RelatedPaths: []string{"golang/auth/auth.go"},
				}}, nil, nil
			}
			return nil, nil, nil, nil
		},
		readBaseFileFunc: func(_ context.Context, _ string) (string, bool, error) {
			return "package auth\nfunc Authenticate() {}\n", true, nil
		},
	}

	res, err := EvaluateDiff(context.Background(), parsed, checker, AuditOptions{})
	if err != nil {
		t.Fatalf("EvaluateDiff: %v", err)
	}
	if res.Decision != DecisionBlock {
		t.Errorf("expected DecisionBlock due to deleted implementation file without contract change, got %s", res.Decision)
	}
	if len(res.Findings) == 0 || res.Findings[0].RecordID != "contract.auth" {
		t.Errorf("expected contract.auth block finding, got %+v", res.Findings)
	}
}
