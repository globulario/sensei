// SPDX-License-Identifier: Apache-2.0

package extractor_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/globulario/awareness-graph/golang/extractor"
)

func TestSelfAwarenessSeed_ImportsAndValidates(t *testing.T) {
	var buf bytes.Buffer
	e, err := extractor.ImportAwarenessYAMLs("../../docs/awareness", &buf)
	if err != nil {
		t.Fatalf("ImportAwarenessYAMLs: %v", err)
	}
	if e.Triples == 0 {
		t.Fatal("expected seed triples to be emitted; got 0")
	}
	if errs := extractor.ValidateNTriples(bytes.NewReader(buf.Bytes())); len(errs) > 0 {
		for _, e := range errs {
			t.Errorf("validation: %s", e)
		}
		t.Fatalf("%d N-Triples validation errors", len(errs))
	}
}

func TestSelfAwarenessSeed_MinimumNodeKindsPresent(t *testing.T) {
	out := importSelfAwarenessSeed(t)
	mustContain := []string{
		"awareness#invariant/",
		"awareness#failureMode/",
		"awareness#incidentPattern/",
		"awareness#authoredIn>",
	}
	for _, needle := range mustContain {
		if !strings.Contains(out, needle) {
			t.Fatalf("missing expected seed output fragment %q", needle)
		}
	}
}

func TestSelfAwarenessSeed_VocabularyGuards(t *testing.T) {
	out := importSelfAwarenessSeed(t)
	mustNotContain := []string{
		"awareness#source>",
		"awareness#relatedInvariant>",
	}
	for _, needle := range mustNotContain {
		if strings.Contains(out, needle) {
			t.Fatalf("forbidden legacy predicate present %q", needle)
		}
	}
}

func importSelfAwarenessSeed(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	if _, err := extractor.ImportAwarenessYAMLs("../../docs/awareness", &buf); err != nil {
		t.Fatalf("ImportAwarenessYAMLs: %v", err)
	}
	return buf.String()
}
