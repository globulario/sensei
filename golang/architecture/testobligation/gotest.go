// SPDX-License-Identifier: AGPL-3.0-only

package testobligation

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/globulario/sensei/golang/rdf"
)

// goTestEvent is the subset of `go test -json` (test2json) we consume. The
// Action field is what carries pass/fail/skip — a structured signal, so
// nothing here parses human console wording. Output is captured only to
// recover a skip's stated reason for the evidence trail, never to decide an
// outcome.
type goTestEvent struct {
	Action  string `json:"Action"`
	Package string `json:"Package"`
	Test    string `json:"Test"`
	Output  string `json:"Output"`
}

// GoTestResult is what the stream said about one (package, test) pair. Its
// fields stay unexported: callers pass the map back into ResolveGoObligations
// rather than inspecting outcomes directly, which keeps the anchor-matching
// rules in one place.
type GoTestResult struct {
	outcome Outcome
	reason  string
}

// ParseGoTestJSON reads a `go test -json` stream and returns observed results
// keyed by "<package-relative-dir>:<TestName>".
//
// modulePath is stripped from the event's Package so the key is repo-relative
// and comparable with a graph anchor's directory.
//
// Subtests are folded into their parent: a skipped subtest does not make the
// parent skipped, because the parent's own Action already reports what the
// whole test did. Only top-level results are recorded.
func ParseGoTestJSON(r io.Reader, modulePath string) (map[string]GoTestResult, error) {
	results := map[string]GoTestResult{}
	// Skip output accumulates per (package, test) because test2json emits the
	// reason as an "output" event before the terminal "skip" event.
	lastOutput := map[string]string{}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var ev goTestEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			// A malformed line is not a reason to discard an otherwise usable
			// run, but it must not be silently treated as a result either — it
			// simply contributes nothing.
			continue
		}
		if ev.Test == "" || strings.Contains(ev.Test, "/") {
			continue // package-level event, or a subtest
		}
		key := goTestKey(ev.Package, ev.Test, modulePath)
		switch ev.Action {
		case "output":
			// test2json brackets a test's own output with its own framing
			// ("=== RUN", "--- SKIP: Name (0.00s)", "=== PAUSE", …). The
			// t.Skip message is the last NON-framing line, so framing must be
			// ignored rather than simply taking the last output — otherwise
			// the recorded reason is "--- SKIP: TestName (0.00s)", which
			// restates the outcome and drops the only thing worth keeping.
			if s := strings.TrimSpace(ev.Output); s != "" && !isGoTestFraming(s) {
				lastOutput[key] = s
			}
		case "pass":
			results[key] = GoTestResult{outcome: OutcomePass}
		case "fail":
			results[key] = GoTestResult{outcome: OutcomeFail}
		case "skip":
			results[key] = GoTestResult{outcome: OutcomeSkipped, reason: skipReason(lastOutput[key])}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read go test json: %w", err)
	}
	return results, nil
}

// goTestKey builds "<repo-relative-dir>:<TestName>" from an event.
func goTestKey(pkg, test, modulePath string) string {
	dir := strings.TrimPrefix(pkg, modulePath)
	dir = strings.Trim(dir, "/")
	if dir == "" {
		dir = "."
	}
	return dir + ":" + test
}

// isGoTestFraming reports whether a line is test2json's own scaffolding rather
// than something the test wrote. These prefixes are part of the tool's output
// format, not of the test's message.
func isGoTestFraming(line string) bool {
	return strings.HasPrefix(line, "=== ") || strings.HasPrefix(line, "--- ") ||
		strings.HasPrefix(line, "PASS") || strings.HasPrefix(line, "FAIL") ||
		strings.HasPrefix(line, "ok ")
}

// skipReason trims the "file.go:81: " location prefix off a skip message so the
// stored reason reads as the reason rather than as a location. Best effort: an
// unrecognized shape is kept verbatim rather than dropped, because losing the
// reason is worse than reporting it untidily.
func skipReason(out string) string {
	s := strings.TrimSpace(out)
	if i := strings.Index(s, ".go:"); i >= 0 {
		if j := strings.Index(s[i:], ": "); j >= 0 {
			s = s[i+j+2:]
		}
	}
	return strings.TrimSpace(s)
}

// AnchorKey reduces a graph test anchor to the "<dir>:<TestName>" form that
// ParseGoTestJSON produces.
//
// It deliberately matches on directory rather than exact filename. `go test
// -json` reports a package, not the file a test is defined in, so the file
// component of an anchor cannot be reconstructed from the stream. Matching on
// the package directory means a test that moved between files in the same
// package still counts as observed — which is the honest reading, since the
// question is whether the proof ran, not where it is written.
//
// ok is false for an anchor with no "path:Name" shape (a semantic id), which
// cannot be matched against a Go run at all.
func AnchorKey(anchor string) (key string, ok bool) {
	file, name, found := strings.Cut(normalizeAnchor(anchor), ":")
	if !found || file == "" || name == "" {
		return "", false
	}
	return path.Dir(file) + ":" + name, true
}

// normalizeAnchor puts an anchor into the "dir/file.go:TestName" form the
// matcher expects, regardless of which spelling the caller received.
//
// It decodes the IRI path encoding first. A server that has not yet decoded on
// its side hands back "golang%2Fserver%2Ffoo_test.go:TestBar", and the escaped
// slashes make path.Dir see a single segment — so every anchor would silently
// key to "." and report "no result" for a test that did run. That failure is
// invisible in the verdict (both spellings refuse to certify) and wrong only
// in the reason, which is exactly the kind of quiet inaccuracy this tool
// exists to catch. Decoding here keeps the matcher correct against any server
// version rather than depending on someone upstream having done it.
//
// The "file.go::TestName" spelling the corpus also uses collapses onto the
// single-colon form after that.
func normalizeAnchor(anchor string) string {
	decoded := rdf.DecodeIRIPath(anchor)
	if strings.Contains(decoded, "::") {
		return strings.Replace(decoded, "::", ":", 1)
	}
	return decoded
}

// IsGoAnchor reports whether an anchor names a Go test, i.e. whether a
// `go test -json` stream is the right evidence source for it. Anchors in other
// languages are not Go's to answer for.
func IsGoAnchor(anchor string) bool {
	file, _, found := strings.Cut(normalizeAnchor(anchor), ":")
	return found && strings.HasSuffix(file, "_test.go")
}

// ResolveGoObligations maps required anchors onto observed Go results.
//
// An anchor with no observed result becomes Unavailable with a stated reason
// rather than being dropped, so the report distinguishes "ran and passed" from
// "nobody looked". A non-Go anchor is likewise Unavailable: this evidence
// source cannot speak for it, and treating silence from the wrong runner as
// success is exactly the laundering being prevented.
func ResolveGoObligations(anchors []string, results map[string]GoTestResult) []Obligation {
	out := make([]Obligation, 0, len(anchors))
	for _, anchor := range anchors {
		o := Obligation{Anchor: anchor, Required: true}
		switch {
		case !IsGoAnchor(anchor):
			o.Outcome = OutcomeUnavailable
			o.Reason = "no results source for this anchor's language"
		default:
			key, ok := AnchorKey(anchor)
			if !ok {
				o.Outcome = OutcomeUnavailable
				o.Reason = "anchor is not a file:test reference"
				break
			}
			res, seen := results[key]
			if !seen {
				o.Outcome = OutcomeUnavailable
				o.Reason = "no result for this test in the supplied run"
				break
			}
			o.Outcome = res.outcome
			o.Reason = res.reason
		}
		out = append(out, o)
	}
	return out
}
