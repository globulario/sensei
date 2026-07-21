// SPDX-License-Identifier: AGPL-3.0-only

package governedimpact

import "testing"

func TestReportCodecRoundTrip(t *testing.T) {
	rep := mustCompare(t, snap(baseTriples(), oblig1, "cert.v1", "comp.v1"), snap(baseTriples(), oblig1, "cert.v1", "comp.v1"))
	b, err := MarshalCanonicalReport(rep)
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := MarshalCanonicalReport(rep)
	if string(b) != string(b2) {
		t.Fatal("report bytes not deterministic")
	}
	got, err := ParseReport(b)
	if err != nil {
		t.Fatal(err)
	}
	rb, _ := MarshalCanonicalReport(got)
	if string(rb) != string(b) {
		t.Fatal("round-trip not byte-identical")
	}
}

func TestParseReportRejectsUnknownFieldAndTrailing(t *testing.T) {
	if _, err := ParseReport([]byte(`{"schema_version":"governedimpact.report/v1","nope":true}`)); err == nil {
		t.Fatal("expected unknown-field rejection")
	}
	rep := mustCompare(t, snap(baseTriples(), oblig1, "c", "d"), snap(baseTriples(), oblig1, "c", "d"))
	b, _ := MarshalCanonicalReport(rep)
	if _, err := ParseReport(append(append([]byte(nil), b...), []byte("{}")...)); err == nil {
		t.Fatal("expected trailing-content rejection")
	}
}
