// SPDX-License-Identifier: Apache-2.0

package main

import "github.com/globulario/sensei/golang/seedmeta"

func normalizedEmbeddedSeed() []byte {
	stamped, _ := seedmeta.AppendMarker(seedNT)
	return stamped
}

func normalizedEmbeddedSeedMarker() (seedmeta.Marker, bool) {
	return seedmeta.ParseMarker(normalizedEmbeddedSeed())
}
