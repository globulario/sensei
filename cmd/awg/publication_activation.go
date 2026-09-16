// SPDX-License-Identifier: AGPL-3.0-only

// publication_activation.go owns the ACTIVATION TRANSITION: the moment a generation
// becomes the one this domain serves.
//
// Slice G4 of the graph-identity front asked for the five CLI lifecycles to share one
// publication primitive. Measured, the difference was narrower and sharper than "five
// lifecycles":
//
//	step                        build  import  rebuild  governance
//	stage/promote/verify/drop     y      -        -         -
//	verify the live graph         y      -        y         y
//	WRITE THE MARKER              y      -        y         y
//	write a publication receipt   y      -        -      (its own richer record)
//	RECORD THE ACTIVE POINTER     y      -        -         -
//
// All three verify before writing the marker, so the staging transaction is not what
// diverged. What diverged is the last step: only `build` recorded which generation it
// had activated. `rebuild` and `governance activate` made a generation live and left
// the registry's ACTIVE pointer naming the PREVIOUS one, after which every reader that
// resolves through the registry refuses this graph. Fail-closed is right; giving the
// operator no reason is not.
//
// So this is the shared primitive, and it is deliberately small: everything upstream
// of it is legitimately different work (a staged scoped promotion is not a whole-store
// reload), while everything from "the graph is verified" onward must be identical, or
// two commands leave the system in two different states while both reporting success.
package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/globulario/sensei/golang/seedmeta"
)

// activateGeneration publishes the marker and records the ACTIVE generation pointer.
//
// Ordering is the property, not an implementation detail: the marker is written FIRST,
// so the pointer can never name a generation whose marker was never published. A failed
// marker write therefore leaves the pointer exactly where it was.
//
// A failure to record the pointer does NOT fail the activation. The store holds the
// generation and its marker is written, so reporting failure would be untrue; the
// consequence of a stale pointer is that readers refuse, which is the fail-closed
// behaviour the pointer exists to provide. It is stated loudly instead.
//
// An empty domain is the case `rebuild` and `governance activate` are in: they publish
// a graph without knowing which governed domain it belongs to. That is reported rather
// than skipped, because the observable effect — readers refusing a graph that is
// otherwise healthy — is indistinguishable from a broken endpoint unless someone says
// so.
func activateGeneration(out io.Writer, markerPath string, marker seedmeta.Marker, domain string, registry domainRegistrySelection) error {
	if err := seedmeta.WriteMarkerFile(markerPath, marker); err != nil {
		return fmt.Errorf("publish graph marker: %w", err)
	}
	fmt.Fprintf(out, "  graph marker:    %s\n", markerPath)

	if strings.TrimSpace(domain) == "" {
		fmt.Fprintf(out, "  ACTIVE generation pointer: NOT updated — this command published %s without naming a governed domain.\n", marker.Digest)
		fmt.Fprintf(out, "    Readers that resolve through the domain registry will REFUSE this graph until it is declared.\n")
		fmt.Fprintf(out, "    Declare it by setting active_generation: %s for the domain this graph serves.\n", marker.Digest)
		return nil
	}
	if err := recordActiveGeneration(registry.Path(), domain, marker.Digest); err != nil {
		fmt.Fprintf(out, "  ACTIVE generation pointer: NOT updated for %s: %v\n", domain, err)
		fmt.Fprintf(out, "    Readers will REFUSE this graph until the registry declares active_generation: %s\n", marker.Digest)
		return nil
	}
	fmt.Fprintf(out, "  ACTIVE generation: %s for %s\n", marker.Digest, domain)
	return nil
}
