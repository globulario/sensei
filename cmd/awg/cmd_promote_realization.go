// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	yaml "gopkg.in/yaml.v3"
)

// awg promote-realization — the review turnstile.
//
// It moves ONE explicitly-selected, already-existing candidateRealizesContract
// entry into authoritative realizations. It does not score, does not infer, and
// does not look at name/path overlap — that all happened at suggest time. This
// is a human review action: pick a candidate by impl (+arch), and promote it.
//
// After promotion + rebuild the pair emits the authoritative
// realizesContract / realizedByContract edges; the candidate edge is gone.

// crFile is the authored/generated contract_realizations document.
type crFile struct {
	ContractRealizations struct {
		Realizations []realizationCandidate `yaml:"realizations"`
		Candidates   []realizationCandidate `yaml:"candidates"`
	} `yaml:"contract_realizations"`
}

func runPromoteRealization(args []string) int {
	fs := flag.NewFlagSet("awg promote-realization", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	impl := fs.String("impl", "", "implementation contract id to promote (required)")
	arch := fs.String("arch", "", "architectural contract id (required only when --impl has more than one candidate)")
	why := fs.String("why", "", "REQUIRED obligation note: why this implementation is OBLIGATED to honor the contract (proof, not plausibility)")
	svcRepoFlag := fs.String("services-repo", "", "path to services repo (auto-detect)")
	agRepoFlag := fs.String("ag-repo", "", "path to awareness-graph repo (auto-detect)")
	authoredFlag := fs.String("authored", "", "authored contract_realizations.yaml (default: in ag-repo)")
	candidatesFlag := fs.String("candidates", "", "generated candidate file (default: in ag-repo)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg promote-realization --impl <id> [--arch <id>]

Promote ONE reviewed candidateRealizesContract entry into authoritative
realizations. Review action only — never scores, never infers, never bulk-promotes.
Run 'awg rebuild' afterward to emit realizesContract / realizedByContract.
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *impl == "" {
		fmt.Fprintln(os.Stderr, "awg promote-realization: --impl is required")
		return 2
	}
	if *why == "" {
		fmt.Fprintln(os.Stderr, "awg promote-realization: --why is required — promotion needs an obligation note (why the implementation MUST honor the contract: code enforcement / test / failure mode / authored rule / human confirmation). Plausibility is not proof.")
		return 2
	}

	agRepo := *agRepoFlag
	if agRepo == "" {
		svcRepo, _ := resolveServicesRepo(*svcRepoFlag)
		agRepo, _ = resolveAGRepo("", svcRepo)
	}
	authoredPath := *authoredFlag
	candidatesPath := *candidatesFlag
	if authoredPath == "" {
		authoredPath = filepath.Join(agRepo, "docs", "awareness", "architecture", "contract_realizations.yaml")
	}
	if candidatesPath == "" {
		candidatesPath = filepath.Join(agRepo, "docs", "awareness", "architecture", "contract_realization_candidates.yaml")
	}

	authoredRaw, err := os.ReadFile(authoredPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg promote-realization: read authored: %v\n", err)
		return 1
	}
	var authored crFile
	if err := yaml.Unmarshal(authoredRaw, &authored); err != nil {
		fmt.Fprintf(os.Stderr, "awg promote-realization: parse authored: %v\n", err)
		return 1
	}
	var generated crFile
	genRaw, gerr := os.ReadFile(candidatesPath)
	if gerr == nil {
		_ = yaml.Unmarshal(genRaw, &generated)
	}

	promoted, fromGenerated, err := promoteCandidate(&authored, &generated, *impl, *arch, *why)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg promote-realization: %v\n", err)
		return 1
	}

	if err := os.WriteFile(authoredPath, renderCRFile(&authored, authoredHeader), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "awg promote-realization: write authored: %v\n", err)
		return 1
	}
	if fromGenerated {
		// Use the generator's own renderer so the file stays byte-identical to a
		// fresh `awg suggest-realizations` run (its --check freshness gate).
		genOut, rerr := renderCandidates(generated.ContractRealizations.Candidates)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "awg promote-realization: render candidates: %v\n", rerr)
			return 1
		}
		if err := os.WriteFile(candidatesPath, genOut, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "awg promote-realization: write candidates: %v\n", err)
			return 1
		}
	}

	fmt.Fprintf(os.Stderr, "promote-realization: promoted %s --realizesContract--> %s (was confidence=%s)\n",
		promoted.Implementation, promoted.Realizes, promoted.Confidence)
	fmt.Fprintln(os.Stderr, "  run `awg rebuild` to emit realizesContract / realizedByContract.")
	return 0
}

// promoteCandidate is the pure turnstile: find the single matching candidate,
// refuse on the guardrail violations, move it into authored realizations, and
// remove it from its source candidate list. Returns the promoted entry and
// whether it came from the generated file.
func promoteCandidate(authored, generated *crFile, impl, arch, why string) (realizationCandidate, bool, error) {
	if impl == "" {
		return realizationCandidate{}, false, fmt.Errorf("--impl is required")
	}
	type hit struct {
		entry         realizationCandidate
		idx           int
		fromGenerated bool
	}
	var hits []hit
	collect := func(list []realizationCandidate, fromGen bool) {
		for i, c := range list {
			if c.Implementation == impl && (arch == "" || c.Realizes == arch) {
				hits = append(hits, hit{entry: c, idx: i, fromGenerated: fromGen})
			}
		}
	}
	collect(authored.ContractRealizations.Candidates, false)
	collect(generated.ContractRealizations.Candidates, true)

	switch {
	case len(hits) == 0:
		if arch != "" {
			return realizationCandidate{}, false, fmt.Errorf("no candidate %s --> %s to promote", impl, arch)
		}
		return realizationCandidate{}, false, fmt.Errorf("no candidate for impl %s to promote", impl)
	case len(hits) > 1:
		var pairs []string
		for _, h := range hits {
			pairs = append(pairs, h.entry.Realizes)
		}
		sort.Strings(pairs)
		return realizationCandidate{}, false, fmt.Errorf("ambiguous: %d candidates for %s (%v); specify --arch", len(hits), impl, pairs)
	}
	h := hits[0]

	// Refuse if already authoritative.
	for _, r := range authored.ContractRealizations.Realizations {
		if r.Implementation == h.entry.Implementation && r.Realizes == h.entry.Realizes {
			return realizationCandidate{}, false, fmt.Errorf("%s --> %s is already authoritative (realizesContract)", impl, h.entry.Realizes)
		}
	}

	// The obligation note (why) is the proof of authority — it replaces the
	// scoring evidence (which was only the reason to SUSPECT, never proof).
	evidence := []string{why}
	rz := realizationCandidate{
		Implementation: h.entry.Implementation,
		Realizes:       h.entry.Realizes,
		Source:         "promoted_candidate",
		Confidence:     h.entry.Confidence,
		Evidence:       evidence,
	}
	authored.ContractRealizations.Realizations = append(authored.ContractRealizations.Realizations, rz)
	sortCandidates(authored.ContractRealizations.Realizations)

	// Remove from whichever candidate list it came from.
	if h.fromGenerated {
		generated.ContractRealizations.Candidates = removeAt(generated.ContractRealizations.Candidates, h.idx)
	} else {
		authored.ContractRealizations.Candidates = removeAt(authored.ContractRealizations.Candidates, h.idx)
	}
	return rz, h.fromGenerated, nil
}

func removeAt(s []realizationCandidate, i int) []realizationCandidate {
	out := make([]realizationCandidate, 0, len(s)-1)
	out = append(out, s[:i]...)
	out = append(out, s[i+1:]...)
	return out
}

func sortCandidates(s []realizationCandidate) {
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].Implementation != s[j].Implementation {
			return s[i].Implementation < s[j].Implementation
		}
		return s[i].Realizes < s[j].Realizes
	})
}

const authoredHeader = `# Contract-spine Phase 2 — authoritative impl→architectural realizations.
#
#   realizations: authoritative (hand-authored or promoted via
#                 ` + "`awg promote-realization`" + `). Emit realizesContract + realizedByContract.
#   candidates:   NON-authoritative, awaiting promotion. Emit candidateRealizesContract ONLY.
#
# Generator-discovered candidates live in contract_realization_candidates.yaml.
# This file keeps promoted realizations plus any hand-authored links the
# generator cannot infer.
`

func renderCRFile(f *crFile, header string) []byte {
	if f.ContractRealizations.Realizations == nil {
		f.ContractRealizations.Realizations = []realizationCandidate{}
	}
	if f.ContractRealizations.Candidates == nil {
		f.ContractRealizations.Candidates = []realizationCandidate{}
	}
	var buf bytes.Buffer
	buf.WriteString(header)
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	_ = enc.Encode(f)
	enc.Close()
	return buf.Bytes()
}
