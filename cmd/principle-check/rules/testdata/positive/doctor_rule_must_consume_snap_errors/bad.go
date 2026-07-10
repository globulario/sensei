// Positive-control fixture for doctor_rule_must_consume_snap_errors.
// Evaluate reads snap.Nodes without consulting any snap error signal
// (HadError/QueryError/LoadError/ReachError/DataErrors).
package badfix

import "github.com/globulario/sensei/cmd/principle-check/rules/testdata/positive/doctor_rule_must_consume_snap_errors/collector"

type Finding struct{ Msg string }

type Config struct{}

type nodeCountRule struct{}

func (r nodeCountRule) Evaluate(snap *collector.Snapshot, cfg Config) []Finding {
	var findings []Finding
	for _, node := range snap.Nodes { // BAD: reasons on snap without checking snap errors
		_ = node
		findings = append(findings, Finding{Msg: "node finding"})
	}
	return findings
}
