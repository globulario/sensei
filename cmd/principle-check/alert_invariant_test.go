// SPDX-License-Identifier: AGPL-3.0-only

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.alert_invariant
// @awareness file_role=artifact_gate_for_meta_alert_must_name_invariant

// Artifact gate for meta.alert_must_name_a_violated_invariant_not_a_crossed_threshold.
//
// Every Prometheus alert in the platform's alerting rules MUST carry an
// `invariant` annotation naming the architectural invariant it detects a
// violation of. Alerts that fire on a threshold alone ("CPU > 90%") give
// the operator a NUMBER but not a MEANING; the operator must reverse-
// engineer what contract the threshold protects. An alert that names its
// invariant ("invariant: resource.cpu_headroom_for_failover_absorption")
// tells the operator what architectural property is at risk.
//
// Alerts that genuinely detect a resource-level condition without a
// mapped invariant are allowed but must appear in the carve-out list
// with a reason. The carve-out is a NAMED gap, not a silent omission.
package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

type alertGroup struct {
	Name  string      `yaml:"name"`
	Rules []alertRule `yaml:"rules"`
}

type alertRule struct {
	Alert       string            `yaml:"alert"`
	Annotations map[string]string `yaml:"annotations"`
}

type alertDoc struct {
	Groups []alertGroup `yaml:"groups"`
}

// Alerts that fire on a resource-level threshold without a mapped
// invariant. Each must carry a reason. Keep this list small — the
// principle says an unmapped alert is architectural debt.
var alertInvariantCarveOuts = map[string]string{
	"HighNodeCPU":    "Resource-level threshold; no single invariant — covers runaway processes, call storms, and external load. Maps to the resource layer, not a specific architectural contract.",
	"HighNodeMemory": "Resource-level threshold; same rationale as HighNodeCPU.",
}

func TestAlertsMustNameInvariant(t *testing.T) {
	repo := requireServicesRepo(t)
	path := filepath.Join(repo, "golang", "monitoring", "prometheus_rules", "globular_alerts.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read alerting rules: %v", err)
	}
	var doc alertDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse alerting rules: %v", err)
	}

	total := 0
	named := 0
	carved := 0
	var missing []string

	for _, g := range doc.Groups {
		for _, r := range g.Rules {
			if r.Alert == "" {
				continue
			}
			total++
			if inv := r.Annotations["invariant"]; inv != "" {
				named++
				continue
			}
			if reason, ok := alertInvariantCarveOuts[r.Alert]; ok {
				carved++
				t.Logf("CARVE-OUT: %s — %s", r.Alert, reason)
				continue
			}
			missing = append(missing, r.Alert)
		}
	}

	for _, name := range missing {
		t.Errorf("alert %q has no `invariant` annotation and is not in the carve-out list. "+
			"Add `invariant: <id>` to its annotations naming the architectural contract it detects "+
			"a violation of, or add it to alertInvariantCarveOuts with a reason. "+
			"See meta.alert_must_name_a_violated_invariant_not_a_crossed_threshold.", name)
	}

	t.Logf("alert invariant coverage: %d total, %d named, %d carved-out, %d missing",
		total, named, carved, len(missing))
}
