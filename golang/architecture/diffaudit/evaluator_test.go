// SPDX-License-Identifier: Apache-2.0

package diffaudit

import (
	"context"
	"testing"
)

type fakeChecker struct {
	checkFileFunc     func(ctx context.Context, file, content, domain string) ([]AuditFinding, error)
	getFileImpactFunc func(ctx context.Context, file, domain string) ([]string, []string, []string, error)
	readBaseFileFunc  func(ctx context.Context, path string) (string, bool, error)
}

func (f *fakeChecker) CheckFile(ctx context.Context, file, content, domain string) ([]AuditFinding, error) {
	if f.checkFileFunc != nil {
		return f.checkFileFunc(ctx, file, content, domain)
	}
	return nil, nil
}

func (f *fakeChecker) GetFileImpact(ctx context.Context, file, domain string) ([]string, []string, []string, error) {
	if f.getFileImpactFunc != nil {
		return f.getFileImpactFunc(ctx, file, domain)
	}
	return nil, nil, nil, nil
}

func (f *fakeChecker) ReadBaseFile(ctx context.Context, path string) (string, bool, error) {
	if f.readBaseFileFunc != nil {
		return f.readBaseFileFunc(ctx, path)
	}
	return " func main() {}\n fmt.Println(\"hello\")\n }\n", true, nil
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
		t.Errorf("expected DecisionPass, got %s", res.Decision)
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
		getFileImpactFunc: func(_ context.Context, file, domain string) ([]string, []string, []string, error) {
			if file == "cmd/main.go" {
				return []string{"cmd/main_test.go"}, nil, nil, nil
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
	if len(res.Findings) == 0 || res.Findings[0].RecordID != "obligation.omitted_required_test" {
		t.Errorf("expected obligation.omitted_required_test finding, got %+v", res.Findings)
	}
	if err := res.Validate(); err != nil {
		t.Errorf("Validate() failed: %v", err)
	}
}
