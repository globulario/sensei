// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/scip-code/scip/bindings/go/scip"
	"google.golang.org/protobuf/proto"
)

func TestRunScipIngest_RejectsEmptyIndexByDefault(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	data, err := proto.Marshal(&scip.Index{})
	if err != nil {
		t.Fatalf("marshal empty index: %v", err)
	}
	scipPath := filepath.Join(dir, "index.scip")
	if err := os.WriteFile(scipPath, data, 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	outDir := filepath.Join(dir, "generated")

	code := runScipIngest([]string{"--scip", scipPath, "--out", outDir, "--quiet"})
	if code != 1 {
		t.Fatalf("runScipIngest code=%d, want 1", code)
	}
	if _, err := os.Stat(filepath.Join(outDir, "code_symbols.yaml")); !os.IsNotExist(err) {
		t.Fatalf("code_symbols.yaml exists err=%v, want no generated file", err)
	}
}

func TestRunScipIngest_AllowsEmptyIndexWhenExplicit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	data, err := proto.Marshal(&scip.Index{})
	if err != nil {
		t.Fatalf("marshal empty index: %v", err)
	}
	scipPath := filepath.Join(dir, "index.scip")
	if err := os.WriteFile(scipPath, data, 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	outDir := filepath.Join(dir, "generated")

	code := runScipIngest([]string{"--scip", scipPath, "--out", outDir, "--quiet", "--allow-empty"})
	if code != 0 {
		t.Fatalf("runScipIngest code=%d, want 0", code)
	}
	if _, err := os.Stat(filepath.Join(outDir, "code_symbols.yaml")); err != nil {
		t.Fatalf("code_symbols.yaml missing: %v", err)
	}
}
