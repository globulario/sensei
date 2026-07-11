// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/rdf"
	yaml "gopkg.in/yaml.v3"
)

// sensei suggest-realizations — the conservative candidate generator.
//
// It scans the compiled graph for implementation contracts (kind http/grpc/
// grpc_web/rest) and architectural contracts, scores each pair on hard,
// reproducible evidence, and writes candidateRealizesContract entries — and ONLY
// candidates. It NEVER emits realizesContract and NEVER promotes. Path/name
// overlap can at most produce a low-confidence candidate; name overlap alone
// produces nothing. Promotion is a separate, human step.

type implC struct {
	id, kind, name, rw, domain string
	files                      []string
}

type archC struct {
	id, name, rw, domain string
	files                []string
	covers               []string
	violatedBy           []string
}

type realizationCandidate struct {
	Implementation string   `yaml:"implementation"`
	Realizes       string   `yaml:"realizes"`
	Source         string   `yaml:"source"`
	Confidence     string   `yaml:"confidence"`
	Evidence       []string `yaml:"evidence"`
}

func runSuggestRealizations(args []string) int {
	fs := flag.NewFlagSet("sensei suggest-realizations", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	svcRepoFlag := fs.String("services-repo", "", "path to services repo (auto-detect)")
	agRepoFlag := fs.String("ag-repo", "", "path to awareness-graph repo (auto-detect)")
	output := fs.String("output", "", "candidate file to write (default: <ag-repo>/docs/awareness/architecture/contract_realization_candidates.yaml)")
	check := fs.Bool("check", false, "regenerate in memory, diff the committed file, exit 1 if stale")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei suggest-realizations [flags]

Propose candidateRealizesContract edges (implementation → architectural contract)
from conservative graph evidence. Candidates only — never realizesContract, never
promoted. Review and promote with the authored contract_realizations schema.
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	svcRepo, _ := resolveServicesRepo(*svcRepoFlag)
	agRepo, _ := resolveAGRepo(*agRepoFlag, svcRepo)
	if agRepo == "" {
		fmt.Fprintln(os.Stderr, "sensei suggest-realizations: cannot find awareness-graph repo; use --ag-repo")
		return 1
	}
	inputDirs, intentDir, err := collectInputDirs(svcRepo, agRepo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei suggest-realizations: %v\n", err)
		return 1
	}
	ntBytes, _, _, genErr := generateNT(inputDirs, intentDir, svcRepo, agRepo)
	if genErr != nil {
		fmt.Fprintf(os.Stderr, "sensei suggest-realizations: %v\n", genErr)
		return 1
	}

	impls, archs, fmFiles, authoritative := parseContractsFromNT(ntBytes)
	// A pair that already has a review DECISION (promoted lives in realizations;
	// rejected / needs_* live in the reviews file) is settled — never re-propose
	// it. Merge those into the skip set so a rejected lead does not come back.
	reviewsPath := filepath.Join(agRepo, "docs", "contract_realization_reviews.yaml")
	if raw, rerr := os.ReadFile(reviewsPath); rerr == nil {
		for pair := range decidedPairs(raw) {
			authoritative[pair] = true
		}
	}
	cands := suggestCandidates(impls, archs, fmFiles, authoritative)
	out, err := renderCandidates(cands)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei suggest-realizations: render: %v\n", err)
		return 1
	}

	target := *output
	if target == "" {
		target = filepath.Join(agRepo, "docs", "awareness", "architecture", "contract_realization_candidates.yaml")
	}
	if *check {
		committed, _ := os.ReadFile(target)
		if !bytes.Equal(bytes.TrimSpace(committed), bytes.TrimSpace(out)) {
			fmt.Fprintf(os.Stderr, "suggest-realizations: STALE — %s differs from a fresh run; rerun to regenerate\n", target)
			return 1
		}
		fmt.Fprintf(os.Stderr, "suggest-realizations: fresh (%d candidates)\n", len(cands))
		return 0
	}
	if err := os.WriteFile(target, out, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "sensei suggest-realizations: write: %v\n", err)
		return 1
	}
	byConf := map[string]int{}
	for _, c := range cands {
		byConf[c.Confidence]++
	}
	fmt.Fprintf(os.Stderr, "suggest-realizations: wrote %d candidate(s) to %s (high=%d medium=%d low=%d)\n",
		len(cands), target, byConf["high"], byConf["medium"], byConf["low"])
	return 0
}

// suggestCandidates is the pure, deterministic scorer. It is the unit under test.
func suggestCandidates(impls map[string]*implC, archs map[string]*archC, fmFiles map[string][]string, authoritative map[string]bool) []realizationCandidate {
	var out []realizationCandidate
	for _, im := range impls {
		for _, ar := range archs {
			if im.id == ar.id || authoritative[im.id+"|"+ar.id] {
				continue
			}
			// Never cross repo domains: a Globular surface cannot realize a
			// foreign-repo (e.g. cli/cli benchmark) architectural contract, even
			// if a coversPath glob coincidentally overlaps (both use internal/).
			if im.domain != ar.domain {
				continue
			}
			c, ok := score(im, ar, fmFiles)
			if !ok {
				continue
			}
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Implementation != out[j].Implementation {
			return out[i].Implementation < out[j].Implementation
		}
		return out[i].Realizes < out[j].Realizes
	})
	return out
}

// score applies the conservative evidence rules to one (impl, arch) pair.
func score(im *implC, ar *archC, fmFiles map[string][]string) (realizationCandidate, bool) {
	var ev []string
	strong, medium, weak := 0, 0, 0

	// strong: shares an exact source file
	if f := firstShared(im.files, ar.files); f != "" {
		strong++
		ev = append(ev, "shares source file "+f)
	}
	// strong: an impl source file matches an architectural coversPath/detect glob
	if im.files != nil {
		for _, f := range im.files {
			for _, g := range ar.covers {
				if globMatch(g, f) {
					strong++
					ev = append(ev, "source file "+f+" matches contract path "+g)
					break
				}
			}
		}
	}
	// medium: same directory
	if d := firstSharedDir(im.files, ar.files); d != "" {
		medium++
		ev = append(ev, "same directory "+d)
	}
	// medium: shares a directory with a failure mode that violates the contract
	for _, fm := range ar.violatedBy {
		if d := firstSharedDir(im.files, fmFiles[fm]); d != "" {
			medium++
			ev = append(ev, "shares directory "+d+" with violating failure mode "+fm)
			break
		}
	}
	// weak: meaningful name-token overlap
	if t := nameOverlap(im, ar); t != "" {
		weak++
		ev = append(ev, "name token overlap: "+t)
	}

	// Directionality: a read-only surface does not realize a write-only
	// guarantee unless a strong signal ties them.
	if strong == 0 && im.rw == "read" && ar.rw == "write" {
		return realizationCandidate{}, false
	}

	// No candidate from name similarity alone (or nothing).
	if strong == 0 && medium == 0 {
		return realizationCandidate{}, false
	}

	conf := "low"
	switch {
	case strong >= 1 && (medium >= 1 || weak >= 1):
		conf = "high"
	case strong >= 1, medium >= 1 && weak >= 1:
		conf = "medium"
	default:
		conf = "low" // medium signal(s) alone — path/dir overlap → low
	}

	return realizationCandidate{
		Implementation: im.id,
		Realizes:       ar.id,
		Source:         "generated_evidence_scoring",
		Confidence:     conf,
		Evidence:       ev,
	}, true
}

// ── N-Triples parsing ────────────────────────────────────────────────────────

func parseContractsFromNT(ntBytes []byte) (map[string]*implC, map[string]*archC, map[string][]string, map[string]bool) {
	type cnode struct {
		isContract, isFM          bool
		kind, name, rw, repo      string
		files, covers, violatedBy []string
		hasInv, hasTest           bool
		protects                  []string
	}
	nodes := map[string]*cnode{}
	get := func(id string) *cnode {
		n := nodes[id]
		if n == nil {
			n = &cnode{}
			nodes[id] = n
		}
		return n
	}
	authoritative := map[string]bool{}

	implKinds := map[string]bool{"http": true, "grpc": true, "grpc_web": true, "rest": true}

	sc := bufio.NewScanner(bytes.NewReader(ntBytes))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		s, p, o, ok := splitTriple(sc.Text())
		if !ok {
			continue
		}
		sid, sclass := iriID(s)
		switch p {
		case rdf.PropType:
			if o == "<"+rdf.ClassContract+">" {
				get(sid).isContract = true
			} else if o == "<"+rdf.ClassFailureMode+">" {
				get(sid).isFM = true
			}
		case rdf.PropKind:
			get(sid).kind = literal(o)
		case rdf.PropReadOrWrite:
			get(sid).rw = literal(o)
		case rdf.PropRepo:
			get(sid).repo = literal(o)
		case rdf.PropLabel:
			get(sid).name = literal(o)
		case rdf.PropAnchoredIn:
			if f, isFile := sourceFilePath(o); isFile {
				get(sid).files = append(get(sid).files, f)
			}
		case rdf.PropCoversPath, rdf.PropDetectAppliesToPath:
			get(sid).covers = append(get(sid).covers, literal(o))
		case rdf.PropConstrainedByInvariant:
			get(sid).hasInv = true
		case rdf.PropRequiresTest:
			get(sid).hasTest = true
		case rdf.PropViolatedBy:
			if oid, _ := iriID(o); oid != "" {
				get(sid).violatedBy = append(get(sid).violatedBy, oid)
			}
		case rdf.PropProtects:
			if f, isFile := sourceFilePath(o); isFile {
				get(sid).protects = append(get(sid).protects, f)
			}
		case rdf.PropRealizesContract:
			if oid, _ := iriID(o); oid != "" {
				authoritative[sid+"|"+oid] = true
			}
		}
		_ = sclass
	}

	impls := map[string]*implC{}
	archs := map[string]*archC{}
	fmFiles := map[string][]string{}
	for id, n := range nodes {
		if n.isFM {
			fmFiles[id] = n.protects
		}
		if !n.isContract {
			continue
		}
		if implKinds[n.kind] {
			impls[id] = &implC{id: id, kind: n.kind, name: n.name, rw: n.rw, domain: n.repo, files: n.files}
		} else {
			archs[id] = &archC{id: id, name: n.name, rw: n.rw, domain: n.repo, files: n.files, covers: n.covers, violatedBy: n.violatedBy}
		}
	}
	return impls, archs, fmFiles, authoritative
}

func renderCandidates(cands []realizationCandidate) ([]byte, error) {
	type file struct {
		ContractRealizations struct {
			Realizations []realizationCandidate `yaml:"realizations"`
			Candidates   []realizationCandidate `yaml:"candidates"`
		} `yaml:"contract_realizations"`
	}
	var f file
	f.ContractRealizations.Realizations = []realizationCandidate{}
	f.ContractRealizations.Candidates = cands
	var buf bytes.Buffer
	buf.WriteString("# GENERATED by `sensei suggest-realizations` — DO NOT EDIT.\n")
	buf.WriteString("# Conservative candidateRealizesContract proposals (impl → architectural).\n")
	buf.WriteString("# Candidates only — never authoritative. Promote by moving an entry into the\n")
	buf.WriteString("# authored contract_realizations.yaml `realizations:` list after review.\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	enc.Close()
	return buf.Bytes(), nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

func splitTriple(line string) (s, p, o string, ok bool) {
	line = strings.TrimSpace(line)
	if !strings.HasSuffix(line, " .") {
		return "", "", "", false
	}
	core := strings.TrimSuffix(line, " .")
	parts := strings.SplitN(core, " ", 3)
	if len(parts) < 3 {
		return "", "", "", false
	}
	// predicate comes as <iri>; strip the brackets so callers compare to rdf.Prop*.
	return parts[0], strings.TrimSuffix(strings.TrimPrefix(parts[1], "<"), ">"), parts[2], true
}

// iriID returns the short id and class segment of an <…#class/id> IRI.
func iriID(tok string) (id, class string) {
	t := strings.TrimSuffix(strings.TrimPrefix(tok, "<"), ">")
	h := strings.LastIndex(t, "#")
	if h < 0 {
		return "", ""
	}
	rest := t[h+1:]
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return "", rest
	}
	return rest[slash+1:], rest[:slash]
}

func sourceFilePath(tok string) (string, bool) {
	id, class := iriID(tok)
	if class != "sourceFile" || id == "" {
		return "", false
	}
	id = strings.ReplaceAll(id, "%2F", "/")
	id = strings.ReplaceAll(id, "%2f", "/")
	return id, true
}

func literal(o string) string {
	o = strings.TrimSpace(o)
	if strings.HasPrefix(o, "\"") {
		if i := strings.LastIndexByte(o, '"'); i > 0 {
			return o[1:i]
		}
	}
	return strings.Trim(o, "<>")
}

func firstShared(a, b []string) string {
	set := map[string]bool{}
	for _, x := range a {
		set[x] = true
	}
	sort.Strings(b)
	for _, y := range b {
		if set[y] {
			return y
		}
	}
	return ""
}

func firstSharedDir(a, b []string) string {
	set := map[string]bool{}
	for _, x := range a {
		set[path.Dir(x)] = true
	}
	dirs := make([]string, 0, len(b))
	for _, y := range b {
		dirs = append(dirs, path.Dir(y))
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		if d != "." && d != "/" && set[d] {
			return d
		}
	}
	return ""
}

func globMatch(glob, p string) bool {
	g := strings.TrimSuffix(strings.TrimSuffix(glob, "**"), "*")
	g = strings.TrimSuffix(g, "/")
	if g == "" {
		return false
	}
	return p == g || strings.HasPrefix(p, g+"/")
}

var nameStop = map[string]bool{
	"http": true, "grpc": true, "contract": true, "api": true, "the": true,
	"and": true, "must": true, "service": true, "handler": true, "endpoint": true,
}

func tokens(s string) map[string]bool {
	out := map[string]bool{}
	for _, t := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		if len(t) >= 4 && !nameStop[t] {
			out[t] = true
		}
	}
	return out
}

func nameOverlap(im *implC, ar *archC) string {
	a := tokens(im.id + " " + im.name)
	b := tokens(ar.id + " " + ar.name)
	var shared []string
	for t := range a {
		if b[t] {
			shared = append(shared, t)
		}
	}
	sort.Strings(shared)
	return strings.Join(shared, ",")
}
