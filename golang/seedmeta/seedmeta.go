// SPDX-License-Identifier: Apache-2.0

package seedmeta

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	NamespaceIRI         = "https://globular.io/awareness#"
	markerClassIRI       = NamespaceIRI + "SeedBuild"
	markerVersionIRI     = NamespaceIRI + "seedMarkerVersion"
	markerDigestIRI      = NamespaceIRI + "seedDigestSha256"
	markerTripleCountIRI = NamespaceIRI + "seedTripleCount"
	markerAuthoredInIRI  = NamespaceIRI + "authoredIn"
	markerVersion        = "v2"
)

type Marker struct {
	Digest      string
	IRI         string
	TripleCount int64
}

func AppendMarker(nt []byte) ([]byte, Marker) {
	base := canonicalize(stripMarkerLines(nt))
	sum := sha256.Sum256(base)
	digest := hex.EncodeToString(sum[:])
	tripleCount := countTriples(base) + 6
	marker := Marker{
		Digest:      digest,
		IRI:         NamespaceIRI + "seedBuild/sha256-" + digest,
		TripleCount: tripleCount,
	}
	var out bytes.Buffer
	out.Write(base)
	out.Write(MarkerTriples(marker))
	return out.Bytes(), marker
}

// MarkerTriples serializes the 6 self-describing marker triples for m as
// N-Triples. It is the single source for the marker's on-graph shape, shared by
// AppendMarker (whole-artifact stamping) and the scoped-update path in
// `sensei build --repo`, which recomputes the whole-graph marker and INSERTs
// only these lines after a domain-scoped DELETE/append — never re-PUTting the
// full graph. Callers MUST pass a Marker whose TripleCount already accounts for
// these 6 lines (as AppendMarker computes it), so the live total the server
// verifies against stays exact.
func MarkerTriples(m Marker) []byte {
	var out bytes.Buffer
	fmt.Fprintf(&out, "<%s> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <%s> .\n", m.IRI, markerClassIRI)
	fmt.Fprintf(&out, "<%s> <http://www.w3.org/2000/01/rdf-schema#label> %q .\n", m.IRI, "Embedded seed sha256 "+m.Digest[:12])
	fmt.Fprintf(&out, "<%s> <%s> %q .\n", m.IRI, markerDigestIRI, m.Digest)
	fmt.Fprintf(&out, "<%s> <%s> %q .\n", m.IRI, markerTripleCountIRI, strconv.FormatInt(m.TripleCount, 10))
	fmt.Fprintf(&out, "<%s> <%s> %q .\n", m.IRI, markerVersionIRI, markerVersion)
	fmt.Fprintf(&out, "<%s> <%s> %q .\n", m.IRI, markerAuthoredInIRI, "generated:seed_marker")
	return out.Bytes()
}

func ParseMarker(nt []byte) (Marker, bool) {
	marker, ok := parseMarker(nt)
	if !ok {
		return Marker{}, false
	}
	if marker.TripleCount == 0 {
		marker.TripleCount = countTriples(canonicalize(nt))
	}
	return marker, true
}

func parseMarker(nt []byte) (Marker, bool) {
	var marker Marker
	for _, raw := range strings.Split(string(nt), "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "<"+NamespaceIRI+"seedBuild/sha256-") {
			continue
		}
		subj, value, pred, ok := parseLiteralLine(line)
		if !ok || value == "" {
			continue
		}
		marker.IRI = subj
		switch pred {
		case markerDigestIRI:
			marker.Digest = value
		case markerTripleCountIRI:
			if n, err := strconv.ParseInt(value, 10, 64); err == nil && n >= 0 {
				marker.TripleCount = n
			}
		}
	}
	if marker.IRI == "" || marker.Digest == "" {
		return Marker{}, false
	}
	return marker, true
}

func canonicalize(nt []byte) []byte {
	lines := strings.Split(string(nt), "\n")
	kept := make([]string, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		kept = append(kept, line)
	}
	if len(kept) == 0 {
		return []byte{}
	}
	sort.Strings(kept)
	var out bytes.Buffer
	for _, line := range kept {
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

func stripMarkerLines(nt []byte) []byte {
	marker, ok := parseMarker(nt)
	if !ok {
		return nt
	}
	needle := "<" + marker.IRI + ">"
	var kept []string
	for _, raw := range strings.Split(string(nt), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, needle+" ") {
			continue
		}
		kept = append(kept, raw)
	}
	return []byte(strings.Join(kept, "\n"))
}

func parseLiteralLine(line string) (subject, value, predicate string, ok bool) {
	if !strings.HasPrefix(line, "<") {
		return "", "", "", false
	}
	subjEnd := strings.Index(line, ">")
	rest := line[subjEnd+1:]
	predStart := strings.Index(rest, "<")
	predEnd := strings.Index(rest, ">")
	firstQuote := strings.Index(line, "\"")
	lastQuote := strings.LastIndex(line, "\"")
	if subjEnd <= 1 || predStart < 0 || predEnd <= predStart || firstQuote < 0 || lastQuote <= firstQuote {
		return "", "", "", false
	}
	return line[1:subjEnd], line[firstQuote+1 : lastQuote], rest[predStart+1 : predEnd], true
}

func countTriples(nt []byte) int64 {
	var count int64
	for _, raw := range strings.Split(string(nt), "\n") {
		if strings.TrimSpace(raw) != "" {
			count++
		}
	}
	return count
}
