// SPDX-License-Identifier: Apache-2.0

// Package rdf provides triple-emission helpers for the awareness ontology.
// Designed so a converter can stream N-Triples to a Writer without holding
// the full graph in memory — important once we extend to whole-codebase
// annotation scans.
package rdf

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// Emitter writes N-Triples to an io.Writer and tracks per-class /
// per-predicate counts for build summaries. Not goroutine-safe — a single
// converter owns one emitter.
type Emitter struct {
	bw                *bufio.Writer
	Triples           int
	ByClass           map[string]int
	ByPredicate       map[string]int
	PathStripPrefixes []string // if set, NormPath strips the longest matching prefix from paths

	// Default domain scope applied to nodes that carry NO inline domain scope of
	// their own. Used by the foreign-repo bootstrap: structural extractors emit
	// domain-agnostic YAML, and the import names the domain once (e.g. `awg build
	// --repo github.com/cli/cli`) so every extracted node is scoped to that repo
	// instead of leaking into the untagged home domain. Empty → no default
	// (existing home-domain builds are unchanged).
	DefaultRepo      string
	DefaultDomain    string
	DefaultSourceSet string

	// typedSubjects/scopedSubjects support FinalizeDefaultScope: every node the
	// emitter types is recorded; a node that emits its own aw:domain/aw:repo is
	// marked scoped. At finalize, typed-but-unscoped nodes adopt DefaultRepo — so
	// the *structural* extractor output (SourceFile, CodeSymbol, Test, …), which
	// never calls emitDomainScope, is still attributed to the repo instead of
	// leaking into the untagged home domain.
	typedSubjects  map[string]bool
	scopedSubjects map[string]bool
}

// NewEmitter wraps w in a buffered writer. Caller must call Flush before
// closing w.
func NewEmitter(w io.Writer) *Emitter {
	return &Emitter{
		bw:             bufio.NewWriter(w),
		ByClass:        map[string]int{},
		ByPredicate:    map[string]int{},
		typedSubjects:  map[string]bool{},
		scopedSubjects: map[string]bool{},
	}
}

// Flush forwards to the underlying buffered writer.
func (e *Emitter) Flush() error { return e.bw.Flush() }

// NormPath strips the longest matching entry from PathStripPrefixes so that
// authoredIn literals are relative to a stable root rather than a
// machine-specific checkout directory.
func (e *Emitter) NormPath(path string) string {
	best := path
	bestLen := 0
	for _, raw := range e.PathStripPrefixes {
		prefix := strings.TrimRight(raw, "/") + "/"
		if after, ok := strings.CutPrefix(path, prefix); ok && len(prefix) > bestLen {
			best = after
			bestLen = len(prefix)
		}
	}
	return best
}

// Triple emits one triple. All three arguments must be pre-rendered with
// surrounding < > for IRIs or " " for literals — this function does no
// formatting itself so the caller controls escaping precisely.
func (e *Emitter) Triple(s, p, o string) {
	fmt.Fprintf(e.bw, "%s %s %s .\n", s, p, o)
	e.Triples++
	e.ByPredicate[p]++
	// A node that carries its own domain/repo scope must not be re-scoped by
	// FinalizeDefaultScope.
	if e.scopedSubjects != nil && (p == IRI(PropDomain) || p == IRI(PropRepo)) {
		e.scopedSubjects[s] = true
	}
}

// Typed emits the rdf:type triple for subj as cls and records the class
// count for build summaries. cls must be a full IRI string (not the bare
// class name).
func (e *Emitter) Typed(subj, cls string) {
	e.Triple(subj, IRI(PropType), IRI(cls))
	e.ByClass[cls]++
	if e.typedSubjects != nil {
		e.typedSubjects[subj] = true
	}
}

// FinalizeDefaultScope tags every typed-but-unscoped node with DefaultRepo (as
// aw:domain "repo" + aw:repo <DefaultRepo>). No-op when DefaultRepo is empty, so
// home-domain / public self-builds are unchanged. Call once, before Flush, after
// all nodes for a repo's input have been emitted.
func (e *Emitter) FinalizeDefaultScope() {
	if e.DefaultRepo == "" {
		return
	}
	for subj := range e.typedSubjects {
		if e.scopedSubjects[subj] {
			continue
		}
		e.Triple(subj, IRI(PropDomain), Lit(DomainRepo))
		e.Triple(subj, IRI(PropRepo), Lit(e.DefaultRepo))
	}
}

// IRI renders s as an IRI reference token (<s>). Caller is responsible
// for ensuring s is already a valid IRI — use MintIRI for ID composition.
func IRI(s string) string { return "<" + s + ">" }

// MintIRI composes a class-scoped IRI from a class IRI and an ID. The ID
// is percent-encoded against the N-Triples IRIREF-disallowed set so etcd
// templates ({service_id}), file paths (/golang/foo.go), and any other
// punctuation pass validation. The conversion is reversible:
// DecodeIRIPath inverts it.
func MintIRI(classIRI, id string) string {
	// classIRI ends with the class name like ".../awareness#Invariant"; we
	// want ".../awareness#invariant/<encoded-id>" — i.e. switch the class
	// name to lowercase and append /<id>.
	hashIdx := strings.LastIndex(classIRI, "#")
	if hashIdx < 0 {
		return IRI(classIRI + "/" + EncodeIRIPath(id))
	}
	prefix := classIRI[:hashIdx+1]
	className := classIRI[hashIdx+1:]
	return IRI(prefix + lowerFirst(className) + "/" + EncodeIRIPath(id))
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	if c := s[0]; c >= 'A' && c <= 'Z' {
		return string(c+32) + s[1:]
	}
	return s
}

// EncodeIRIPath percent-encodes the N-Triples IRIREF-disallowed set:
//
//	#x00-#x20 < > " { } | ^ ` \ plus '/' (to keep file-path IDs as a single
//	segment) and '%' (so the encoding is reversible).
//
// We do NOT use net/url.PathEscape — it's too aggressive (escapes ':',
// which N-Triples accepts) and would make IRIs less readable for the small
// debugging savings. The W3C N-Triples grammar
// (https://www.w3.org/TR/n-triples/#grammar) is the authority on what's
// disallowed.
func EncodeIRIPath(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c <= 0x20 || c == '<' || c == '>' || c == '"' || c == '{' || c == '}' ||
			c == '|' || c == '^' || c == '`' || c == '\\' || c == '/' || c == '%' {
			fmt.Fprintf(&b, "%%%02X", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// DecodeIRIPath reverses EncodeIRIPath: every "%XX" hex escape becomes its
// byte. It is the inverse so that EncodeIRIPath(DecodeIRIPath(seg)) == seg for
// any segment EncodeIRIPath produced. A malformed escape (not two hex digits)
// is left verbatim — decoding is best-effort and never errors, so callers that
// pass an already-decoded id (no escapes) get it back unchanged.
func DecodeIRIPath(s string) string {
	if !strings.ContainsRune(s, '%') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			hi, ok1 := unhex(s[i+1])
			lo, ok2 := unhex(s[i+2])
			if ok1 && ok2 {
				b.WriteByte(hi<<4 | lo)
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func unhex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	}
	return 0, false
}

// Lit renders s as an N-Triples string literal with required escapes.
// Only the W3C-minimum escapes are applied; the spike found that
// emitting unescaped quotes / newlines / backslashes inside a literal is
// the most common authoring bug.
func Lit(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\t", `\t`,
		"\r", `\r`,
	)
	return `"` + r.Replace(s) + `"`
}

// IsStableID reports whether s looks like a stable identifier (snake_case
// with dots/underscores/digits/dashes only). The converter uses this to
// decide whether a string referenced from incident_patterns.wrong_fixes
// should become an aw:forbids edge to a ForbiddenFix node, or be kept as
// a rdfs:comment literal annotation on the pattern itself. The spike
// proved this distinction is necessary: wrong_fixes mixes IDs with prose,
// and naive emit produces invalid IRIs containing em-dashes, backticks,
// and quotes.
func IsStableID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '.' || r == '-':
		default:
			return false
		}
	}
	return true
}
