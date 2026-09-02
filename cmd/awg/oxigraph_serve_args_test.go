// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"slices"
	"testing"
)

// A mutation removing a flag from an inline exec.Command survived the entire
// suite once: the store-level tests proved a flag MATTERED while nothing proved
// we SET it. The argv is built in one place so it can be asserted.
func TestOxigraphServeArgs(t *testing.T) {
	args := oxigraphServeArgs("/var/lib/sensei/oxi", "127.0.0.1:7878")

	if len(args) == 0 || args[0] != "serve" {
		t.Fatalf("argv must begin with the serve subcommand: %v", args)
	}
	for _, want := range [][2]string{{"--location", "/var/lib/sensei/oxi"}, {"--bind", "127.0.0.1:7878"}} {
		i := slices.Index(args, want[0])
		if i < 0 || i+1 >= len(args) || args[i+1] != want[1] {
			t.Fatalf("%s not followed by %q in %v", want[0], want[1], args)
		}
	}
}

// --union-default-graph must NOT be set while transient staging graphs exist.
//
// `sensei build` stages a candidate slice and a SECOND seed marker in
// urn:sensei:graph-staging:<marker> before promoting it in one transaction.
// Union reads would expose that in-flight candidate to every concurrent
// unqualified query and break atomic publication. This is an invariant, not a
// preference: it must fail if someone adds the flag for the (real, but later)
// reason that named-graph publication needs it.
func TestOxigraphServeArgsDoNotEnableUnionReadsWhileStagingGraphsExist(t *testing.T) {
	if args := oxigraphServeArgs("/tmp/oxi", "127.0.0.1:0"); slices.Contains(args, "--union-default-graph") {
		t.Fatalf("union reads enabled while sensei build still stages candidates in "+
			"urn:sensei:graph-staging:<marker>: concurrent readers would observe an "+
			"unpublished candidate and a duplicate marker.\ngot: %v", args)
	}
}
