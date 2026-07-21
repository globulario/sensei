// SPDX-License-Identifier: AGPL-3.0-only

package main

import "testing"

func TestBenchmarkFreezeRequiresTaskRepoOracleAndOutput(t *testing.T) {
	if code := runBenchmarkFreezeExternal(nil); code != 2 {
		t.Fatalf("expected usage exit 2, got %d", code)
	}
}

func TestBenchmarkReconstructRequiresFrozenWorkspace(t *testing.T) {
	if code := runBenchmarkReconstruct(nil); code != 2 {
		t.Fatalf("expected usage exit 2, got %d", code)
	}
}

func TestBenchmarkEvaluateRequiresOracleReviewAndMapping(t *testing.T) {
	if code := runBenchmarkEvaluateExternal(nil); code != 2 {
		t.Fatalf("expected usage exit 2, got %d", code)
	}
}

func TestBenchmarkStatusRequiresWorkspace(t *testing.T) {
	if code := runBenchmarkStatusExternal(nil); code != 2 {
		t.Fatalf("expected usage exit 2, got %d", code)
	}
}
