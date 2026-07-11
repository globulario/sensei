// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	awarenesspb "github.com/globulario/sensei/golang/pb"
)

func TestWriteGateSARIF(t *testing.T) {
	findings := []fileFinding{
		{File: "src/payment.go", Warnings: []*awarenesspb.EditWarning{
			{RuleId: "payments.paid_state", Class: "Invariant", Message: "paid must come from processor", Enforcement: "block", Detail: "forbidden pattern matched"},
			{RuleId: "meta.fallback", Class: "ForbiddenFix", Message: "fallback hides failure", Enforcement: "warn"},
		}},
	}
	path := filepath.Join(t.TempDir(), "out.sarif")
	if err := writeGateSARIF(path, "HEAD~1...HEAD", findings); err != nil {
		t.Fatal(err)
	}
	var log sarifLog
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &log); err != nil {
		t.Fatalf("SARIF is not valid JSON: %v", err)
	}
	if log.Version != "2.1.0" || len(log.Runs) != 1 {
		t.Fatalf("bad SARIF envelope: %+v", log)
	}
	r := log.Runs[0]
	if r.Tool.Driver.Name != "Sensei" {
		t.Errorf("driver name = %q", r.Tool.Driver.Name)
	}
	if len(r.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(r.Results))
	}
	// block finding → error level; warn → warning level
	got := map[string]string{}
	for _, res := range r.Results {
		got[res.RuleID] = res.Level
		if len(res.Locations) != 1 || res.Locations[0].PhysicalLocation.ArtifactLocation.URI != "src/payment.go" {
			t.Errorf("bad location on %s: %+v", res.RuleID, res.Locations)
		}
	}
	if got["payments.paid_state"] != "error" {
		t.Errorf("block finding level = %q, want error", got["payments.paid_state"])
	}
	if got["meta.fallback"] != "warning" {
		t.Errorf("warn finding level = %q, want warning", got["meta.fallback"])
	}
	if len(r.Tool.Driver.Rules) != 2 {
		t.Errorf("rules = %d, want 2", len(r.Tool.Driver.Rules))
	}
}

func TestWriteGateSARIF_EmptyClearsAlerts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.sarif")
	if err := writeGateSARIF(path, "HEAD", nil); err != nil {
		t.Fatal(err)
	}
	var log sarifLog
	raw, _ := os.ReadFile(path)
	if err := json.Unmarshal(raw, &log); err != nil {
		t.Fatalf("empty SARIF invalid: %v", err)
	}
	if len(log.Runs) != 1 || len(log.Runs[0].Results) != 0 {
		t.Errorf("empty findings should yield 0 results, got %d", len(log.Runs[0].Results))
	}
}
