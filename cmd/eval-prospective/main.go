// SPDX-License-Identifier: AGPL-3.0-only

// Command eval-prospective freezes the prospective authoring-recall sample of
// docs/evaluation/prospective-recall-protocol-v1.md.
//
// It implements docs/design/prospective-recall-harness-259.md.
//
// `freeze` is Slice 1: the steps from "protocol merged" up to and including
// "blind adjudication package emitted". It stops there, and it holds no
// vocabulary for an applicability label — a tool able to express an answer is
// a tool able to leak one.
//
// `run`, `score` and `report` are Slice 2. They were written before any label
// existed and can only execute after the labels are frozen, which is the order
// the protocol requires rather than the order that happened to be convenient:
// a grader authored after seeing scores is not a grader. `run` refuses to
// start until a frozen answer key binds to this exact sample, and no flag
// bypasses that.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/globulario/sensei/golang/architecture/prospective"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	ctx := context.Background()
	var err error
	switch os.Args[1] {
	case "freeze":
		err = runFreeze(ctx, os.Args[2:])
	case "protocol":
		err = runProtocol(os.Args[2:])
	case "run":
		err = runRun(ctx, os.Args[2:])
	case "score":
		err = runScore(os.Args[2:])
	case "report":
		err = runReport(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "eval-prospective: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval-prospective:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `eval-prospective — freeze, replay and score the prospective authoring-recall sample (#259)

Usage:
  eval-prospective freeze    [flags]   enumerate, classify, sample, emit blind packages
  eval-prospective protocol            show the registered protocols and verify their digests
  eval-prospective run       [flags]   replay the frozen retrieval surface over the pinned changes
  eval-prospective score     [flags]   compare the run against the frozen applicability labels
  eval-prospective report    [flags]   render the protocol section 12 report from a score

freeze is Slice 1 and stops at the blind adjudication package. run, score and
report are Slice 2: written before the labels existed, and refusing to execute
until a frozen answer key binds to this exact sample. There is no flag that
bypasses that refusal.
`)
}

func runProtocol(args []string) error {
	fs := flag.NewFlagSet("protocol", flag.ExitOnError)
	repo := fs.String("repo", ".", "repository checkout to verify the protocol document in")
	if err := fs.Parse(args); err != nil {
		return err
	}
	for _, p := range registeredProtocols {
		status := "ok"
		if err := p.verify(*repo); err != nil {
			status = "REFUSED: " + err.Error()
		}
		fmt.Printf("%s\n  path:     %s\n  digest:   %s\n  world:    %s (%s)\n  status:   %s\n",
			p.ID, p.Path, p.DigestSHA256, p.World, p.Domain, status)
	}
	return nil
}

func runFreeze(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("freeze", flag.ExitOnError)
	repo := fs.String("repo", ".", "checkout of the world being sampled")
	protocolID := fs.String("protocol", defaultProtocol.ID, "protocol identity to run under")
	revision := fs.String("revision", "", "pinned world revision (required: the sample is bound to it)")
	seed := fs.String("seed", "", "selection seed, committed before labels exist (required)")
	generatedAt := fs.String("generated-at", "", "RFC3339 timestamp; a self-stamped artifact is not reproducible (required)")
	out := fs.String("out", "", "directory to write the reference set into (required)")
	senseiBin := fs.String("sensei", "sensei", "production Sensei CLI to consult")
	addr := fs.String("addr", "localhost:10120", "Sensei gRPC address")
	graphDigest := fs.String("graph-digest", "", "live store digest the classification evidence was read from (required)")
	target := fs.Int("target", prospective.DefaultTargetPerStratum, "per-stratum sampling target")
	overlap := fs.Float64("overlap", overlapFraction, "second-adjudicator overlap fraction")
	corpusLimit := fs.Int("corpus-page", 100, "rows per page when walking a class; the walk continues until the server reports no more")
	if err := fs.Parse(args); err != nil {
		return err
	}
	for name, v := range map[string]string{"revision": *revision, "seed": *seed, "generated-at": *generatedAt, "out": *out, "graph-digest": *graphDigest} {
		if v == "" {
			return fmt.Errorf("--%s is required", name)
		}
	}
	proto, err := protocolByID(*protocolID)
	if err != nil {
		return err
	}
	if err := proto.verify(*repo); err != nil {
		return err
	}

	// Fail closed on drift before anything is enumerated. A sample built from
	// a drifted checkout would describe a world nobody can return to.
	head, err := ResolveRevision(ctx, *repo, "HEAD")
	if err != nil {
		return err
	}
	pinned, err := ResolveRevision(ctx, *repo, *revision)
	if err != nil {
		return err
	}
	tree, err := TreeDigest(ctx, *repo, pinned)
	if err != nil {
		return err
	}
	wb := prospective.Bind(proto.World, proto.Domain, pinned, tree)
	if err := prospective.VerifyRevision(wb, head); err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "enumerating the complete candidate population at", pinned)
	changes, exclusions, err := EnumerateChanges(ctx, *repo, pinned)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "  %d candidate changes, %d excluded\n", len(changes), len(exclusions))

	graph := Graph{Bin: *senseiBin, Addr: *addr, Domain: proto.Domain}
	fmt.Fprintln(os.Stderr, "freezing the eligible corpus")
	corpus, err := graph.BuildCorpus(ctx, *graphDigest, *corpusLimit)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "  %d adjudicable eligible items, %d excluded\n", corpus.Adjudicable(), len(corpus.Excluded))
	for _, a := range corpus.Accounting {
		fmt.Fprintf(os.Stderr, "    %-18s graph=%-4d enumerated=%-4d materialized=%-4d excluded=%-3d beyond-row-cap=%d\n",
			a.Class, a.GraphTotal, a.Enumerated, a.Materialized, a.Excluded, a.NotEnumerable)
	}

	// The corpus comes first because the anchor index is derived from it. Two
	// independent reads of the graph could disagree; one read cannot.
	idx, err := prospective.AnchorIndexFromCorpus(corpus)
	if err != nil {
		return err
	}
	paths := distinctExistingPaths(changes)
	if err := prospective.VerifyAnchorsReachThePopulation(idx, paths); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "  %d anchored paths, against %d distinct existing paths in the population\n", len(idx.AnchoredPaths), len(paths))

	inv, err := prospective.BuildInventory(wb, idx, changes, exclusions)
	if err != nil {
		return err
	}

	manifest, blindCorpus, packages, err := prospective.Build(inv, corpus, prospective.Options{
		ProtocolID:           proto.ID,
		ProtocolDigestSHA256: proto.DigestSHA256,
		Seed:                 *seed,
		GeneratedAt:          *generatedAt,
		TargetPerStratum:     *target,
		OverlapFraction:      *overlap,
		RetrievalSurface:     chosenRetrievalSurface,
	}, ContentLookup(ctx, *repo, changes))
	if err != nil {
		return err
	}

	return writeReferenceSet(*out, inv, corpus, blindCorpus, idx, manifest, packages)
}

// chosenRetrievalSurface resolves design section 3.1.
//
// The protocol's question is what could GOVERN a file being created here, so
// the surface has to accept the proposed change rather than only a path that
// already exists in the graph. `sensei preflight` takes both --file and --task
// and returns risk class, patterns and required actions without needing the
// file to be anchored, which is the closest production comes to the question.
// `briefing --task` is recorded as the secondary because it accepts the task
// text alone.
//
// It is frozen here, before any score exists, so the instrument cannot be
// chosen after somebody sees which surface scores better.
// overlapFraction is section 10's 20%.
const overlapFraction = 0.2

var chosenRetrievalSurface = prospective.RetrievalSurface{
	ID:         "sensei.preflight.file_and_task.v1",
	Invocation: "sensei preflight --file <changed path> --task <change description> --domain <domain> --json  (secondary: sensei briefing --task <change description> --json)",
	Rationale: "The protocol's question is not BY_FILE: it asks what could govern a file being created here, with these imports, in this package. " +
		"preflight accepts the proposed change's path and task text and answers without requiring the path to be anchored, so it can answer for a file that does not yet exist. " +
		"Where production returns no prospective result for such a path, Slice 2 records no_prospective_channel rather than falling back to BY_FILE.",
	NoChannelStatus: prospective.StatusNoProspectiveChannel,
}

func distinctExistingPaths(changes []prospective.Change) []string {
	seen := map[string]bool{}
	for _, c := range changes {
		for _, p := range c.Paths {
			if p.ExistedBefore {
				seen[p.Path] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func writeReferenceSet(dir string, inv prospective.Inventory, corpus prospective.Corpus, blind prospective.BlindCorpus, idx prospective.AnchorIndex, m prospective.Manifest, pkgs []prospective.BlindPackage) error {
	if err := os.MkdirAll(filepath.Join(dir, "packages"), 0o755); err != nil {
		return err
	}
	// overlap-subset.json is written as its own artifact even though the
	// manifest already carries the keys. Section 10 requires the subset to be
	// fixed before any label is compared, and a standalone file with its own
	// digest is what makes that checkable without re-reading a manifest whose
	// other fields could have moved.
	overlap := struct {
		SchemaVersion              string   `json:"schema_version"`
		SampleManifestDigestSHA256 string   `json:"sample_manifest_digest_sha256"`
		Fraction                   float64  `json:"fraction"`
		ItemKeys                   []string `json:"item_keys"`
		SecondAdjudicatorStatus    string   `json:"second_adjudicator_status"`
	}{
		SchemaVersion:              "sensei.prospective_overlap_subset.v1",
		SampleManifestDigestSHA256: m.DigestSHA256,
		Fraction:                   overlapFraction,
		ItemKeys:                   m.OverlapItemKeys,
		// Typed absence, per section 10: an unavailable second adjudicator is
		// recorded, never substituted. No AI stands in for one.
		SecondAdjudicatorStatus: "second_adjudicator_unavailable",
	}

	files := map[string]any{
		"sample-manifest.json":     m,
		"inventory.json":           inv,
		"corpus.json":              corpus,
		prospective.BlindCorpusRef: blind,
		"anchor-index.json":        idx,
		"overlap-subset.json":      overlap,
	}
	digests := map[string]string{}
	for name, payload := range files {
		d, err := writeJSON(filepath.Join(dir, name), payload)
		if err != nil {
			return err
		}
		digests[name] = d
	}
	for _, p := range pkgs {
		name := filepath.Join("packages", packageFileName(p.ItemKey))
		d, err := writeJSON(filepath.Join(dir, name), p)
		if err != nil {
			return err
		}
		digests[name] = d
	}
	names := make([]string, 0, len(digests))
	for n := range digests {
		names = append(names, n)
	}
	sort.Strings(names)
	var body []byte
	for _, n := range names {
		body = append(body, []byte(digests[n]+"  "+n+"\n")...)
	}
	if err := os.WriteFile(filepath.Join(dir, "DIGESTS.txt"), body, 0o644); err != nil {
		return err
	}
	fmt.Printf("frozen: %s\n  manifest digest:     %s\n  blind corpus digest: %s\n  items: %d  packages: %d\n",
		dir, m.DigestSHA256, blind.DigestSHA256, len(m.Items), len(pkgs))
	for _, s := range m.Strata {
		fmt.Printf("  %-24s population=%-5d selected=%-3d %s\n", s.Stratum, s.Population, s.Selected, s.Status)
	}
	for _, e := range m.Exclusions {
		fmt.Printf("  excluded %-32s %d\n", e.Reason, e.Count)
	}
	fmt.Printf("\nNext: a human adjudicates blind against %s. Nothing here may decide applicability.\n", prospective.BlindCorpusRef)
	return nil
}

// packageFileName turns an item key into a portable filename.
//
// The key is scheme-prefixed as "pr1:<hash>", and a colon is not a legal
// filename character on Windows -- git checkout refuses the whole tree, so a
// reference set named this way is simply unavailable to half the people meant
// to read it. Only the filename is rewritten; the item key inside the package
// keeps its colon, because it is the identity a label attaches to and renaming
// it would orphan every reference to it.
func packageFileName(itemKey string) string {
	return strings.ReplaceAll(itemKey, ":", "-") + ".json"
}

// writeJSON writes one artifact and returns the SHA-256 of the BYTES it wrote.
//
// The bytes, not the canonical form. Each artifact already carries its own
// canonical digest inside it, computed over compact JSON; the files on disk are
// indented for a human reader. A ledger recording the canonical digests would
// therefore fail `sha256sum -c` against its own files, and a reader checking
// the reference set would read that as corruption rather than as two different
// digests serving two different purposes.
func writeJSON(path string, payload any) (string, error) {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	body = append(body, '\n')
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}
