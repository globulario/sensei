// SPDX-License-Identifier: AGPL-3.0-only

// Command eval-prospective freezes the prospective authoring-recall sample of
// docs/evaluation/prospective-recall-protocol-v1.md.
//
// It implements Slice 1 of docs/design/prospective-recall-harness-259.md: the
// steps from "protocol merged" up to and including "blind adjudication package
// emitted". It stops there. It does not run retrieval, it does not score, and
// it holds no vocabulary for an applicability label — the freeze order puts
// human labels between this binary and any number, and a tool able to express
// an answer is a tool able to leak one.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

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
	fmt.Fprint(os.Stderr, `eval-prospective — freeze the prospective authoring-recall sample (#259, Slice 1)

Usage:
  eval-prospective freeze    [flags]   enumerate, classify, sample, emit blind packages
  eval-prospective protocol            show the registered protocols and verify their digests

This binary stops at the blind adjudication package. Retrieval execution and
scoring belong to Slice 2, after the human applicability labels are frozen.
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
	overlap := fs.Float64("overlap", 0.2, "second-adjudicator overlap fraction")
	corpusLimit := fs.Int("corpus-limit", 100, "rows requested per class; production caps this server-side, and the shortfall is reconciled against the graph totals")
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

	manifest, packages, err := prospective.Build(inv, corpus, prospective.Options{
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

	return writeReferenceSet(*out, inv, corpus, idx, manifest, packages)
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

func writeReferenceSet(dir string, inv prospective.Inventory, corpus prospective.Corpus, idx prospective.AnchorIndex, m prospective.Manifest, pkgs []prospective.BlindPackage) error {
	if err := os.MkdirAll(filepath.Join(dir, "packages"), 0o755); err != nil {
		return err
	}
	files := map[string]any{
		"sample-manifest.json": m,
		"inventory.json":       inv,
		"corpus.json":          corpus,
		"anchor-index.json":    idx,
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
		name := filepath.Join("packages", p.ItemKey+".json")
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
	fmt.Printf("frozen: %s\n  manifest digest: %s\n  items: %d  packages: %d\n", dir, m.DigestSHA256, len(m.Items), len(pkgs))
	for _, s := range m.Strata {
		fmt.Printf("  %-24s population=%-5d selected=%-3d %s\n", s.Stratum, s.Population, s.Selected, s.Status)
	}
	for _, e := range m.Exclusions {
		fmt.Printf("  excluded %-32s %d\n", e.Reason, e.Count)
	}
	fmt.Printf("\nNext: a human adjudicates docs blind. Nothing here may decide applicability.\n")
	return nil
}

func writeJSON(path string, payload any) (string, error) {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		return "", err
	}
	d, err := prospective.DigestOf(payload)
	if err != nil {
		return "", err
	}
	return d, nil
}
