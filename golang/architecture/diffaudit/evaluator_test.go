// SPDX-License-Identifier: Apache-2.0

package diffaudit

import (
	"context"
	"testing"
)

type fakeChecker struct {
	checkFileFunc     func(ctx context.Context, file, content, domain string) ([]AuditFinding, error)
	getFileImpactFunc func(ctx context.Context, file, domain string) ([]string, []string, []AuditFinding, error)
}

func (f *fakeChecker) CheckFile(ctx context.Context, file, content, domain string) ([]AuditFinding, error) {
	if f.checkFileFunc != nil {
		return f.checkFileFunc(ctx, file, content, domain)
	}
	return nil, nil
}

func (f *fakeChecker) GetFileImpact(ctx context.Context, file, domain string) ([]string, []string, []AuditFinding, error) {
	if f.getFileImpactFunc != nil {
		return f.getFileImpactFunc(ctx, file, domain)
	}
	return nil, nil, nil, nil
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
}

func TestEvaluateDiff_BlocksOnForbiddenFix(t *testing.T) {
	parsed, err := ParseDiff(sampleValidDiff, DefaultParseOptions())
	if err != nil {
		t.Fatalf("ParseDiff: %v", err)
	}

	checker := &fakeChecker{
		checkFileFunc: func(_ context.Context, file, content, _ string) ([]AuditFinding, error) {
			if file == "cmd/main.go" {
				return []AuditFinding{
					{
						RecordID:    "ff-1",
						RecordClass: "forbidden_fix",
						Disposition: "block",
						FilePath:    file,
						Explanation: "forbidden pattern detected",
					},
				}, nil
			}
			return nil, nil
		},
	}

	res, err := EvaluateDiff(context.Background(), parsed, checker, AuditOptions{})
	if err != nil {
		t.Fatalf("EvaluateDiff: %v", err)
	}
	if res.Decision != DecisionBlock {
		t.Errorf("expected DecisionBlock, got %s", res.Decision)
	}
	if len(res.Findings) != 1 || res.Findings[0].RecordID != "ff-1" {
		t.Errorf("unexpected findings: %+v", res.Findings)
	}
}
