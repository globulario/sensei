// SPDX-License-Identifier: AGPL-3.0-only

// serve_compatibility.go decides whether an occupied Oxigraph or
// awareness-graph listen address is safe for `sensei serve` to reuse
// (docs/design/serve-runtime-compatibility.md, issue #118).
//
// The rule is exact match or hard fail — never approximate: an occupied
// port whose runtime descriptor is absent, unreadable, or names a dead
// process is treated identically to a descriptor that names a genuinely
// different checkout/store/marker. "The port merely responds" is never
// sufficient authority to reuse it.
package main

import (
	"errors"
	"fmt"

	"github.com/globulario/sensei/golang/runtimedescriptor"
)

// checkOxigraphCompatibility reports whether the Oxigraph process already
// listening on addr is safe to reuse: it must have been started by a
// `sensei serve` invocation targeting the SAME data directory. Repository
// domain is deliberately not compared here — one Oxigraph store may
// legitimately hold multiple repository domains
// (docs/design/checkout-repository-domain-binding.md); it is the
// awareness-graph service's MARKER binding that is harmful to share, not
// the underlying store.
func checkOxigraphCompatibility(addr, wantDataDir string) (bool, error) {
	got, err := runtimedescriptor.Read(runtimedescriptor.KindOxigraph, addr)
	if err != nil {
		return false, formatUnidentifiedOccupant(runtimedescriptor.KindOxigraph, addr, err)
	}
	want := runtimedescriptor.Descriptor{DataDir: wantDataDir}
	if got.DataDir != want.DataDir {
		return false, formatIncompatibleReuseError(runtimedescriptor.KindOxigraph, addr, got, want)
	}
	return true, nil
}

// checkAwarenessCompatibility reports whether the awareness-graph process
// already listening on addr is safe to reuse: it must have been started
// with the exact same Oxigraph query URL, graph-marker-file path,
// repository root, and repository domain this invocation would use
// (including both sides leaving repo-root/domain unset).
func checkAwarenessCompatibility(addr, wantOxigraphURL, wantMarkerFile, wantRepoRoot, wantRepoDomain string) (bool, error) {
	got, err := runtimedescriptor.Read(runtimedescriptor.KindAwarenessGraph, addr)
	if err != nil {
		return false, formatUnidentifiedOccupant(runtimedescriptor.KindAwarenessGraph, addr, err)
	}
	want := runtimedescriptor.Descriptor{
		OxigraphQueryURL: wantOxigraphURL,
		GraphMarkerFile:  wantMarkerFile,
		RepoRoot:         wantRepoRoot,
		RepoDomain:       wantRepoDomain,
	}
	if got.OxigraphQueryURL != want.OxigraphQueryURL ||
		got.GraphMarkerFile != want.GraphMarkerFile ||
		got.RepoRoot != want.RepoRoot ||
		got.RepoDomain != want.RepoDomain {
		return false, formatIncompatibleReuseError(runtimedescriptor.KindAwarenessGraph, addr, got, want)
	}
	return true, nil
}

// formatUnidentifiedOccupant builds the diagnostic for an occupied port
// with no provable descriptor (acceptance criteria #1/#8): never silently
// reused, and the message names the address and the read failure.
func formatUnidentifiedOccupant(kind runtimedescriptor.Kind, addr string, readErr error) error {
	return fmt.Errorf(
		"%s: address %s is already in use, but no compatible runtime descriptor was found (%v) — "+
			"an occupied listener is never reused solely because it responds. "+
			"Stop the process using %s, or point this invocation at a free address.",
		kind, addr, readErr, addr,
	)
}

// formatIncompatibleReuseError builds the diagnostic for acceptance
// criterion #8: names the occupied address, the owning PID, and both the
// running and requested value for every field that disagreed.
func formatIncompatibleReuseError(kind runtimedescriptor.Kind, addr string, got, want runtimedescriptor.Descriptor) error {
	msg := fmt.Sprintf("%s: address %s is already in use by pid %d, but it is not compatible with this invocation:\n", kind, addr, got.PID)
	diff := func(name, gotVal, wantVal string) {
		if gotVal != wantVal {
			msg += fmt.Sprintf("  %s: running=%q requested=%q\n", name, gotVal, wantVal)
		}
	}
	switch kind {
	case runtimedescriptor.KindOxigraph:
		diff("data directory", got.DataDir, want.DataDir)
	case runtimedescriptor.KindAwarenessGraph:
		diff("oxigraph query url", got.OxigraphQueryURL, want.OxigraphQueryURL)
		diff("graph marker file", got.GraphMarkerFile, want.GraphMarkerFile)
		diff("repo root", got.RepoRoot, want.RepoRoot)
		diff("repo domain", got.RepoDomain, want.RepoDomain)
	}
	msg += fmt.Sprintf("Stop the process using %s, or point this invocation at a free address with a compatible configuration.", addr)
	return errors.New(msg)
}
