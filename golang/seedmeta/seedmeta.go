// SPDX-License-Identifier: Apache-2.0

package seedmeta

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	base := normalize(stripMarkerLines(nt))
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
	fmt.Fprintf(&out, "<%s> <http://www.w3.org/1999/02/22-rdf-syntax-ns#type> <%s> .\n", marker.IRI, markerClassIRI)
	fmt.Fprintf(&out, "<%s> <http://www.w3.org/2000/01/rdf-schema#label> %q .\n", marker.IRI, "Embedded seed sha256 "+digest[:12])
	fmt.Fprintf(&out, "<%s> <%s> %q .\n", marker.IRI, markerDigestIRI, digest)
	fmt.Fprintf(&out, "<%s> <%s> %q .\n", marker.IRI, markerTripleCountIRI, strconv.FormatInt(tripleCount, 10))
	fmt.Fprintf(&out, "<%s> <%s> %q .\n", marker.IRI, markerVersionIRI, markerVersion)
	fmt.Fprintf(&out, "<%s> <%s> %q .\n", marker.IRI, markerAuthoredInIRI, "generated:seed_marker")
	return out.Bytes(), marker
}

func ParseMarker(nt []byte) (Marker, bool) {
	marker, ok := parseMarker(nt)
	if !ok {
		return Marker{}, false
	}
	if marker.TripleCount == 0 {
		marker.TripleCount = countTriples(normalize(nt))
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

func normalize(nt []byte) []byte {
	b := bytes.TrimSpace(nt)
	if len(b) == 0 {
		return []byte{}
	}
	out := make([]byte, 0, len(b)+1)
	out = append(out, b...)
	out = append(out, '\n')
	return out
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
