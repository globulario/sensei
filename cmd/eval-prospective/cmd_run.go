// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/globulario/sensei/golang/architecture/prospective"
	"github.com/globulario/sensei/golang/architecture/prospectivelabel"
	"github.com/globulario/sensei/golang/architecture/prospectivescore"
)

// runRun replays the frozen production surface over the pinned changes.
//
// It refuses to execute until a frozen answer key exists and binds to this
// exact sample. That refusal is the protocol's fifth arrow made mechanical:
// retrieval output must not exist, or must not be visible, when applicability
// is decided. There is deliberately NO flag that bypasses it — a skip flag is
// how the one ordering constraint the whole experiment rests on gets undone by
// somebody in a hurry.
// runFlags is defined separately so a test can assert what the flag set does
// and does not contain. The absence of a bypass flag is a property of this
// command, and a property nothing checks is a property that stops holding.
type runFlags struct {
	set, labels, repo, sensei, addr, domain, graphDigest, executedAt, out *string
}

func defineRunFlags(fs *flag.FlagSet) runFlags {
	return runFlags{
		set:         fs.String("reference-set", "docs/evaluation/prospective-v1-reference-set", "frozen reference set to replay"),
		labels:      fs.String("labels", "", "frozen applicability labels (required: retrieval may not run before the answer key is frozen)"),
		repo:        fs.String("repo", ".", "checkout of the pinned world"),
		sensei:      fs.String("sensei", "sensei", "production Sensei CLI to replay"),
		addr:        fs.String("addr", "localhost:10120", "Sensei gRPC address"),
		domain:      fs.String("domain", "", "repository-context domain (required: the frozen execution identity fixes it)"),
		graphDigest: fs.String("graph-digest", "", "graph digest this run must be served by (required)"),
		executedAt:  fs.String("executed-at", "", "RFC3339 timestamp; a self-stamped artifact is not reproducible (required)"),
		out:         fs.String("out", "", "path to write the run artifact to (required)"),
	}
}

func runRun(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	f := defineRunFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	set, labelsPath, repo := f.set, f.labels, f.repo
	senseiBin, addr, domain := f.sensei, f.addr, f.domain
	graphDigest, executedAt, out := f.graphDigest, f.executedAt, f.out
	for name, v := range map[string]string{
		"labels": *labelsPath, "domain": *domain, "graph-digest": *graphDigest,
		"executed-at": *executedAt, "out": *out,
	} {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("--%s is required", name)
		}
	}

	rs, err := LoadReferenceSet(*set)
	if err != nil {
		return err
	}
	proto, err := protocolByID(rs.Manifest.ProtocolID)
	if err != nil {
		return err
	}
	if err := proto.verify(*repo); err != nil {
		return err
	}
	if proto.DigestSHA256 != rs.Manifest.ProtocolDigestSHA256 {
		return fmt.Errorf("this checkout's protocol digest %s is not the one the sample was frozen under (%s)", proto.DigestSHA256, rs.Manifest.ProtocolDigestSHA256)
	}

	labels, err := prospectivelabel.LoadLabelSet(*labelsPath)
	if err != nil {
		return fmt.Errorf("frozen labels: %w", err)
	}
	if err := labels.VerifyBinding(rs.Manifest.DigestSHA256, rs.Manifest.BlindCorpusDigestSHA256); err != nil {
		return err
	}

	// World drift is refused, not reported. Results may not carry across a
	// moved world; that requires a new observation.
	head, err := ResolveRevision(ctx, *repo, "HEAD")
	if err != nil {
		return err
	}
	if err := prospective.VerifyRevision(rs.Manifest.World, head); err != nil {
		return err
	}

	if err := requireAuthoritativeGraph(ctx, *senseiBin, *addr, *domain, *graphDigest); err != nil {
		return err
	}

	idx := NewCorpusIndex(rs.EligibleItemIDs())
	retriever := Retriever{Bin: *senseiBin, Addr: *addr, Domain: *domain, Repo: *repo}
	run := prospectivescore.Run{
		SchemaVersion:              prospectivescore.RunSchemaVersion,
		ProtocolID:                 rs.Manifest.ProtocolID,
		SampleManifestDigestSHA256: rs.Manifest.DigestSHA256,
		BlindCorpusDigestSHA256:    rs.Manifest.BlindCorpusDigestSHA256,
		LabelsDigestSHA256:         labels.DigestSHA256,
		WorldRevision:              rs.Manifest.World.Revision,
		GraphDigestSHA256:          *graphDigest,
		RetrievalSurface:           rs.Manifest.RetrievalSurface,
		ExecutedAt:                 *executedAt,
		SenseiInvocation: fmt.Sprintf("%s preflight --file <each changed path> --task <%s> --domain %s --repo <pinned checkout> --addr %s --json",
			*senseiBin, TaskTextRuleID, *domain, *addr),
	}

	for i, item := range rs.Manifest.Items {
		pkg, ok := rs.Packages[item.ItemKey]
		if !ok {
			return fmt.Errorf("no package for sampled change %s", item.ItemKey)
		}
		fmt.Fprintf(os.Stderr, "[%d/%d] %s (%s)\n", i+1, len(rs.Manifest.Items), item.ItemKey, item.Stratum)
		cr := retriever.Invoke(ctx, pkg, idx)
		// The stratum comes from the frozen manifest, never from anything
		// observed during the run.
		cr.Stratum = item.Stratum
		run.Changes = append(run.Changes, cr)
		fmt.Fprintf(os.Stderr, "      status=%s surfaced=%d outside-corpus=%d\n",
			cr.RetrievalStatus, len(cr.Surfaced), len(cr.SurfacedOutsideCorpus))
	}

	sealed, err := run.Seal()
	if err != nil {
		return err
	}
	if err := sealed.Validate(); err != nil {
		return err
	}
	if err := writeSealedJSON(*out, sealed); err != nil {
		return err
	}
	fmt.Printf("run written: %s\n  digest: %s\n  changes: %d\n  labels:  %s\n",
		*out, sealed.DigestSHA256, len(sealed.Changes), sealed.LabelsDigestSHA256)
	fmt.Println("\nNext: eval-prospective score --reference-set ... --labels ... --run ...")
	return nil
}

// requireAuthoritativeGraph refuses to measure against a graph the server
// cannot certify.
//
// The frozen execution identity names both the verdict and the freshness state
// this run requires. A degraded or stale graph's answers are a stale graph's
// opinions delivered with the same confidence, and a recall figure computed
// from them would describe a world nobody can return to.
func requireAuthoritativeGraph(ctx context.Context, bin, addr, domain, expectedDigest string) error {
	args := []string{"metadata", "--addr", addr, "--domain", domain, "--json"}
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("graph authority could not be read (%s): %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
	}
	var meta struct {
		Authority struct {
			Verdict              string `json:"verdict"`
			GraphFreshnessState  string `json:"graph_freshness_state"`
			GraphFreshnessDetail string `json:"graph_freshness_detail"`
			LiveDigest           string `json:"live_store_graph_digest_sha256"`
		} `json:"authority"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &meta); err != nil {
		return fmt.Errorf("graph authority: %w", err)
	}
	if meta.Authority.Verdict != "AUTHORITY_VERDICT_AUTHORITATIVE" {
		return fmt.Errorf("graph authority verdict is %s, not AUTHORITY_VERDICT_AUTHORITATIVE: %s",
			meta.Authority.Verdict, meta.Authority.GraphFreshnessDetail)
	}
	if meta.Authority.GraphFreshnessState != "GRAPH_FRESHNESS_STATE_CURRENT" {
		return fmt.Errorf("graph freshness is %s, not GRAPH_FRESHNESS_STATE_CURRENT: %s",
			meta.Authority.GraphFreshnessState, meta.Authority.GraphFreshnessDetail)
	}
	if meta.Authority.LiveDigest != expectedDigest {
		return fmt.Errorf("this server is serving graph %s, but the run is pinned to %s: a different graph is a different experiment",
			meta.Authority.LiveDigest, expectedDigest)
	}
	fmt.Fprintf(os.Stderr, "graph authority: authoritative, current, digest %s\n  detail: %s\n",
		meta.Authority.LiveDigest, meta.Authority.GraphFreshnessDetail)
	return nil
}
