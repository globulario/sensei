// SPDX-License-Identifier: Apache-2.0

package rdf

import (
	"bytes"
	"strings"
	"testing"
)

func TestFinalizeDefaultScope(t *testing.T) {
	var buf bytes.Buffer
	e := NewEmitter(&buf)
	e.DefaultRepo = "github.com/o/x"
	// A structural node (typed, no scope of its own) → should adopt DefaultRepo.
	e.Typed("<n:file>", ClassSourceFile)
	// An already-scoped node (shared) → must NOT be re-scoped to DefaultRepo.
	e.Typed("<n:shared>", ClassInvariant)
	e.Triple("<n:shared>", IRI(PropDomain), Lit(DomainShared))
	e.FinalizeDefaultScope()
	if err := e.Flush(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `<n:file> <`+PropRepo+`> "github.com/o/x"`) {
		t.Errorf("structural node not tagged to DefaultRepo:\n%s", out)
	}
	if strings.Contains(out, `<n:shared> <`+PropRepo+`>`) {
		t.Error("shared node was wrongly re-scoped to DefaultRepo")
	}
}

func TestFinalizeDefaultScope_NoopWhenEmpty(t *testing.T) {
	var buf bytes.Buffer
	e := NewEmitter(&buf) // DefaultRepo empty
	e.Typed("<n:file>", ClassSourceFile)
	e.FinalizeDefaultScope()
	_ = e.Flush()
	if strings.Contains(buf.String(), PropRepo) {
		t.Error("FinalizeDefaultScope must be a no-op with an empty DefaultRepo (home build)")
	}
}
