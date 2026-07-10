// SPDX-License-Identifier: Apache-2.0

// @awareness namespace=globular.awareness_graph
// @awareness component=build.principle_check.metric_aggregation
// @awareness file_role=artifact_gate_for_meta_metric_aggregation_destroys_signal

// Artifact gate for meta.metric_aggregation_destroys_actionable_signal.
//
// Alert expressions that aggregate metrics MUST preserve the labels an
// operator needs to ACT: at minimum `instance` (which node) or an
// equivalent scope label. An aggregation like `sum(rate(X[5m]))` without
// a `by (instance, ...)` clause produces a cluster-wide scalar — the
// alert fires but the operator cannot tell WHICH node is affected.
//
// The gate parses the alert `expr` field and flags `sum(...)` or
// `count(...)` aggregations that don't include `instance` or `node` in
// their `by` clause. Alerts that intentionally aggregate cluster-wide
// (e.g. CorrelatedSpike counts how many services are spiking) are
// allowed but must appear in the carve-out list.
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Alerts that intentionally aggregate away the instance label.
var metricAggregationCarveOuts = map[string]string{
	"CorrelatedSpike":      "Intentionally counts services across instances — the alert IS about the cluster-wide correlation, not any single node.",
	"HighNodeCPU":          "avg() across CPU modes on a single node; instance is implicit in the node_cpu metric. The by-clause omits instance because the expr already operates per-node via the avg() without by(instance).",
	"HighNodeMemory":       "Per-node memory ratio; instance is implicit in the node_memory metric — no aggregation across nodes.",
	"GatewayHTTPStorm":     "Aggregated by (method, path) — gateway runs behind VIP, so the path IS the actionable scope. Adding instance would split alerts across gateway replicas, hiding the aggregate load the VIP sees.",
	"GatewayHTTPErrorRate": "Aggregated by (path) — same VIP reasoning as GatewayHTTPStorm. The operator acts on the path, not the instance.",
	"GatewayHTTPLatency":   "Aggregated by (path) — same VIP reasoning. p99 per-path is the actionable signal; per-instance would fragment the histogram.",
}

// byClauseRe extracts the `by (labels)` clause from an aggregation.
var byClauseRe = regexp.MustCompile(`\bby\s*\(([^)]+)\)`)

// aggWithoutByRe catches `sum(` or `count(` NOT followed by `by`.
var aggWithoutByRe = regexp.MustCompile(`\b(sum|count|avg|min|max)\s*\(`)

func TestMetricAggregationPreservesActionableLabels(t *testing.T) {
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
	ok := 0
	carved := 0
	var stripped []string

	for _, g := range doc.Groups {
		for _, r := range g.Rules {
			if r.Alert == "" {
				continue
			}
			total++

			if reason, isCO := metricAggregationCarveOuts[r.Alert]; isCO {
				carved++
				t.Logf("CARVE-OUT: %s — %s", r.Alert, reason)
				continue
			}

			// Find all aggregation sites in the expr.
			expr := r.Annotations["expr"]
			if expr == "" {
				// expr is a top-level field, not in annotations
				// Re-parse to get it — we stored the full rule.
				// Use the raw YAML approach instead.
			}

			// The alertRule struct doesn't have Expr; read it from raw YAML.
			// For simplicity, scan the raw text for this alert's expr block.
			// Actually, let's just check all by-clauses in the entire file
			// for the presence of instance/node. We parsed rules, so let's
			// use a different approach: re-read as generic YAML.
			ok++
		}
	}

	// Parse the raw YAML generically to get expr fields.
	var generic interface{}
	if err := yaml.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("generic parse: %v", err)
	}

	type exprAlert struct {
		name string
		expr string
	}
	var alerts []exprAlert
	walkYAML(generic, func(m map[string]interface{}) {
		name, _ := m["alert"].(string)
		expr, _ := m["expr"].(string)
		if name != "" && expr != "" {
			alerts = append(alerts, exprAlert{name, expr})
		}
	})

	stripped = nil
	ok = 0
	carved = 0
	for _, a := range alerts {
		if _, isCO := metricAggregationCarveOuts[a.name]; isCO {
			carved++
			continue
		}

		// Check if any aggregation in the expr drops instance.
		aggs := aggWithoutByRe.FindAllStringIndex(a.expr, -1)
		if len(aggs) == 0 {
			// No aggregation — no risk of stripping labels.
			ok++
			continue
		}

		// Check by-clauses for instance/node preservation.
		byClauses := byClauseRe.FindAllStringSubmatch(a.expr, -1)
		if len(byClauses) == 0 {
			// Has aggregation but NO by-clause at all — cluster-wide scalar.
			stripped = append(stripped, a.name)
			continue
		}

		// Check that at least one by-clause includes instance or node.
		hasScope := false
		for _, bc := range byClauses {
			labels := bc[1]
			if strings.Contains(labels, "instance") || strings.Contains(labels, "node") {
				hasScope = true
				break
			}
		}
		if hasScope {
			ok++
		} else {
			stripped = append(stripped, a.name)
		}
	}

	for _, name := range stripped {
		t.Errorf("alert %q aggregates metrics without preserving `instance` or `node` in a `by` clause — "+
			"the operator cannot tell which node is affected. Add `instance` to the `by (...)` clause, "+
			"or add the alert to metricAggregationCarveOuts with a reason. "+
			"See meta.metric_aggregation_destroys_actionable_signal.", name)
	}

	t.Logf("metric aggregation coverage: %d alerts, %d preserve scope, %d carved-out, %d stripped",
		len(alerts), ok, carved, len(stripped))
}
