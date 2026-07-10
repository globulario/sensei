// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store/oxigraph"
)

type seedStatusLane struct {
	State   string `json:"state"`
	Detail  string `json:"detail,omitempty"`
	Current bool   `json:"current"`
}

type seedStatusResult struct {
	SeedPath              string         `json:"seed_path"`
	TransactionPath       string         `json:"transaction_path,omitempty"`
	QueryURL              string         `json:"query_url"`
	MarkerIRI             string         `json:"marker_iri"`
	DigestSHA256          string         `json:"digest_sha256"`
	TripleCount           int64          `json:"triple_count"`
	GeneratedDigestSHA256 string         `json:"generated_digest_sha256,omitempty"`
	GeneratedTripleCount  int64          `json:"generated_triple_count,omitempty"`
	LiveDigestSHA256      string         `json:"live_digest_sha256,omitempty"`
	LiveTripleCount       int64          `json:"live_triple_count,omitempty"`
	GeneratedVsCommitted  seedStatusLane `json:"generated_vs_committed"`
	TransactionStamp      seedStatusLane `json:"transaction_stamp"`
	LiveStore             seedStatusLane `json:"live_store"`
	OverallState          string         `json:"overall_state"`
	OverallDetail         string         `json:"overall_detail,omitempty"`
	RequireCurrent        bool           `json:"require_current"`
}

func runSeedStatus(args []string) int {
	fs := flag.NewFlagSet("awg seed-status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	seedPathFlag := fs.String("seed", "", "path to awareness.nt (default: auto-detect embedded seed)")
	oxigraphURL := fs.String("oxigraph-url", "http://localhost:7878/query", "Oxigraph query or store endpoint")
	svcRepoFlag := fs.String("services-repo", "", "path to services repo (auto-detect)")
	agRepoFlag := fs.String("ag-repo", "", "path to awareness-graph repo (auto-detect)")
	requireCurrent := fs.Bool("require-current", false, "exit 1 when the live store does not contain this seed marker")
	asJSON := fs.Bool("json", false, "output as JSON")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg seed-status [flags]

Checks whether a live Oxigraph store contains the exact seed marker embedded in
an awareness.nt file, and when repo context is available also compares the
committed seed + transaction stamp to a freshly generated graph artifact.

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	seedPath, err := resolveSeedPath(*seedPathFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg seed-status: %v\n", err)
		return 1
	}
	seedBytes, err := os.ReadFile(seedPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg seed-status: read seed: %v\n", err)
		return 1
	}
	marker, ok := seedmeta.ParseMarker(seedBytes)
	if !ok {
		fmt.Fprintf(os.Stderr, "awg seed-status: %s does not carry a seed marker; rebuild it with current AWG\n", seedPath)
		return 1
	}
	svcRepo, _ := resolveServicesRepo(*svcRepoFlag)
	agRepo, _ := resolveAGRepo(*agRepoFlag, svcRepo)

	queryURL, err := normalizeOxigraphQueryURL(*oxigraphURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg seed-status: invalid --oxigraph-url: %v\n", err)
		return 1
	}
	res := seedStatusResult{
		SeedPath:       seedPath,
		QueryURL:       queryURL,
		MarkerIRI:      marker.IRI,
		DigestSHA256:   marker.Digest,
		TripleCount:    marker.TripleCount,
		LiveStore:      seedStatusLane{State: "down", Detail: "live store unreachable"},
		RequireCurrent: *requireCurrent,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	store, err := oxigraph.New(queryURL)
	if err != nil {
		res.LiveStore.Detail = err.Error()
		res.OverallState, res.OverallDetail = classifySeedStatusOverall(res)
		return printSeedStatusResult(res, *asJSON)
	}
	defer store.Close()

	verification := seedmeta.VerifyLiveStore(ctx, store, marker)
	res.LiveDigestSHA256 = verification.Live.Digest
	res.LiveTripleCount = verification.LiveTripleCount
	res.LiveStore = seedStatusLaneFromFreshness(verification)
	populateRepoStatus(&res, agRepo, svcRepo, seedBytes)
	res.OverallState, res.OverallDetail = classifySeedStatusOverall(res)
	return printSeedStatusResult(res, *asJSON)
}

func printSeedStatusResult(res seedStatusResult, asJSON bool) int {
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(res)
		if res.RequireCurrent && res.OverallState != "current" {
			return 1
		}
		return 0
	}
	fmt.Printf("Seed file:           %s\n", res.SeedPath)
	fmt.Printf("Oxigraph query URL:  %s\n", res.QueryURL)
	fmt.Printf("Seed digest:         %s\n", res.DigestSHA256)
	fmt.Printf("Seed triple count:   %d\n", res.TripleCount)
	fmt.Printf("Marker IRI:          %s\n", res.MarkerIRI)
	fmt.Printf("Live digest:         %s\n", strOrDash(res.LiveDigestSHA256))
	fmt.Printf("Live triple count:   %d\n", res.LiveTripleCount)
	fmt.Printf("Generated vs commit: %s\n", res.GeneratedVsCommitted.State)
	if res.GeneratedVsCommitted.Detail != "" {
		fmt.Printf("  detail:            %s\n", res.GeneratedVsCommitted.Detail)
	}
	fmt.Printf("Transaction stamp:   %s\n", res.TransactionStamp.State)
	if res.TransactionStamp.Detail != "" {
		fmt.Printf("  detail:            %s\n", res.TransactionStamp.Detail)
	}
	fmt.Printf("Live store:          %s\n", res.LiveStore.State)
	if res.LiveStore.Detail != "" {
		fmt.Printf("  detail:            %s\n", res.LiveStore.Detail)
	}
	fmt.Printf("Overall state:       %s\n", res.OverallState)
	if res.OverallDetail != "" {
		fmt.Printf("Overall detail:      %s\n", res.OverallDetail)
	}
	if res.OverallState == "current" {
		fmt.Println("Status:              committed seed, transaction stamp, and live store are aligned")
		return 0
	}
	fmt.Println("Status:              authority is not fully aligned across generated, committed, and live graph state")
	fmt.Println("Next step:           run `awg rebuild` to regenerate, verify, and promote one certified graph state")
	if res.RequireCurrent {
		return 1
	}
	return 0
}

func populateRepoStatus(res *seedStatusResult, agRepo, svcRepo string, committedSeed []byte) {
	res.GeneratedVsCommitted = seedStatusLane{State: "unknown", Detail: "repo context unavailable"}
	res.TransactionStamp = seedStatusLane{State: "unknown", Detail: "repo context unavailable"}
	if strings.TrimSpace(agRepo) == "" {
		return
	}
	if err := ensureCrossRepoRebuildPrereqs(agRepo, svcRepo); err != nil {
		detail := err.Error()
		res.GeneratedVsCommitted = seedStatusLane{State: "blocked", Detail: detail}
		res.TransactionStamp = seedStatusLane{State: "blocked", Detail: detail}
		return
	}
	res.TransactionPath = defaultTransactionPath(agRepo)
	inputDirs, intentDir, err := collectInputDirs(svcRepo, agRepo)
	if err != nil || len(inputDirs) == 0 {
		if err != nil {
			res.GeneratedVsCommitted.Detail = err.Error()
			res.TransactionStamp.Detail = err.Error()
		}
		return
	}
	generated, _, _, err := generateNT(inputDirs, intentDir, svcRepo, agRepo)
	if err != nil {
		res.GeneratedVsCommitted.Detail = err.Error()
		res.TransactionStamp.Detail = err.Error()
		return
	}
	if generatedMarker, ok := seedmeta.ParseMarker(generated); ok {
		res.GeneratedDigestSHA256 = generatedMarker.Digest
		res.GeneratedTripleCount = generatedMarker.TripleCount
	}
	agOnly := generateAgOnlyNT(agRepo)
	seedFreshness := evaluateSeedFreshness(committedSeed, generated, agOnly)
	res.GeneratedVsCommitted = seedStatusLaneFromAudit(seedFreshness)

	txCurrent, err := buildTransactionTSV(agRepo, svcRepo, generated)
	if err != nil {
		res.TransactionStamp = seedStatusLane{State: "unknown", Detail: err.Error()}
		return
	}
	committedTx, err := os.ReadFile(res.TransactionPath)
	if err != nil {
		if os.IsNotExist(err) {
			res.TransactionStamp = seedStatusLane{State: "stale", Detail: "committed transaction stamp missing"}
			return
		}
		res.TransactionStamp = seedStatusLane{State: "unknown", Detail: err.Error()}
		return
	}
	res.TransactionStamp = seedStatusLaneFromAudit(evaluateBuildTransactionFreshness(committedTx, txCurrent))
}

func seedStatusLaneFromAudit(res auditResult) seedStatusLane {
	lane := seedStatusLane{
		Current: res.level == auditPASS,
		Detail:  strings.TrimSpace(res.summary),
	}
	if lane.Current {
		lane.State = "current"
		return lane
	}
	lane.State = "stale"
	return lane
}

func seedStatusLaneFromFreshness(verification seedmeta.Verification) seedStatusLane {
	lane := seedStatusLane{Detail: verification.Detail}
	switch verification.State {
	case seedmeta.FreshnessCurrent:
		lane.State = "current"
		lane.Current = true
	case seedmeta.FreshnessStale:
		lane.State = "stale"
	case seedmeta.FreshnessUnknown:
		lane.State = "unknown"
	case seedmeta.FreshnessEmpty:
		lane.State = "empty"
	case seedmeta.FreshnessCheckError:
		if looksLikeStoreDown(verification.Detail) {
			lane.State = "down"
		} else {
			lane.State = "degraded"
		}
	default:
		lane.State = "unknown"
	}
	return lane
}

func looksLikeStoreDown(detail string) bool {
	d := strings.ToLower(strings.TrimSpace(detail))
	switch {
	case strings.Contains(d, "connection refused"),
		strings.Contains(d, "connect: cannot assign requested address"),
		strings.Contains(d, "no such host"),
		strings.Contains(d, "dial tcp"),
		strings.Contains(d, "timeout"),
		strings.Contains(d, "deadline exceeded"),
		strings.Contains(d, "eof"):
		return true
	default:
		return false
	}
}

func classifySeedStatusOverall(res seedStatusResult) (string, string) {
	if res.GeneratedVsCommitted.Current && res.TransactionStamp.Current && res.LiveStore.Current {
		return "current", "generated artifact, committed seed, transaction stamp, and live store all match"
	}
	if res.GeneratedVsCommitted.State == "blocked" || res.TransactionStamp.State == "blocked" {
		if res.GeneratedVsCommitted.Detail != "" {
			return "blocked", res.GeneratedVsCommitted.Detail
		}
		return "blocked", "cross-repo rebuild inputs are unavailable"
	}
	if res.LiveStore.State == "down" {
		return "down", res.LiveStore.Detail
	}
	if res.GeneratedVsCommitted.State == "unknown" || res.TransactionStamp.State == "unknown" || res.LiveStore.State == "unknown" {
		return "unknown", "could not prove all three authority lanes"
	}
	if res.LiveStore.State == "degraded" {
		return "degraded", "live store freshness check errored"
	}
	if res.LiveStore.State == "empty" {
		return "stale", "live store is empty"
	}
	currentCount := 0
	for _, lane := range []seedStatusLane{res.GeneratedVsCommitted, res.TransactionStamp, res.LiveStore} {
		if lane.Current {
			currentCount++
		}
	}
	if currentCount > 0 {
		return "split", "only part of the authority chain is current"
	}
	return "stale", "generated artifact, committed seed, transaction stamp, and live store are not aligned"
}

func resolveSeedPath(explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return filepath.Abs(explicit)
	}
	agRepo, _ := resolveAGRepo("", "")
	if agRepo == "" {
		return "", fmt.Errorf("cannot auto-detect awareness.nt; pass --seed")
	}
	return filepath.Join(agRepo, "golang", "server", "embeddata", "awareness.nt"), nil
}

func normalizeOxigraphQueryURL(raw string) (string, error) {
	storeURL, err := normalizeOxigraphURL(raw)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(storeURL)
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimSuffix(u.Path, "/store") + "/query"
	u.RawQuery = ""
	return u.String(), nil
}
