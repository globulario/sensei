// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"slices"
	"testing"
)

// A mutation removing --union-default-graph from the inline exec.Command
// survived the entire suite: the store-level tests prove the flag MATTERS,
// nothing proved we SET it. This closes that gap.
func TestOxigraphServeArgsSetUnionDefaultGraph(t *testing.T) {
	args := oxigraphServeArgs("/var/lib/sensei/oxi", "127.0.0.1:7878")

	if !slices.Contains(args, "--union-default-graph") {
		t.Fatalf("--union-default-graph missing: every unqualified query in this "+
			"codebase would return zero rows against a named-graph store, and an "+
			"empty read surface is indistinguishable from an empty store.\ngot: %v", args)
	}
	for _, want := range [][2]string{{"--location", "/var/lib/sensei/oxi"}, {"--bind", "127.0.0.1:7878"}} {
		i := slices.Index(args, want[0])
		if i < 0 || i+1 >= len(args) || args[i+1] != want[1] {
			t.Fatalf("%s not followed by %q in %v", want[0], want[1], args)
		}
	}
	if len(args) == 0 || args[0] != "serve" {
		t.Fatalf("argv must begin with the serve subcommand: %v", args)
	}
}
