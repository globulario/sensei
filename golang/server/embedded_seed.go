// SPDX-License-Identifier: AGPL-3.0-only

package main

import "github.com/globulario/awareness-graph/golang/seedmeta"

func normalizedEmbeddedSeed() []byte {
	stamped, _ := seedmeta.AppendMarker(seedNT)
	return stamped
}

func normalizedEmbeddedSeedMarker() (seedmeta.Marker, bool) {
	return seedmeta.ParseMarker(normalizedEmbeddedSeed())
}
