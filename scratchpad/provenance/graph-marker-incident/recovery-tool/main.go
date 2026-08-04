// Throwaway, read-only recovery tool. Not part of the module's real command
// set — lives under scratchpad/, never committed. Queries the live Oxigraph
// store exactly the way sensei serve's own runtime-marker sync does (same
// SeedMarkers/CountTriples calls from golang/store/oxigraph), writes a
// candidate marker file to a temp directory, and verifies it against the
// live store using the server's own seedmeta.VerifyLiveStore. It never
// writes to the real .sensei/ marker path.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/globulario/sensei/golang/seedmeta"
	"github.com/globulario/sensei/golang/store/oxigraph"
)

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "ERROR:", err)
	os.Exit(1)
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	queryURL := "http://127.0.0.1:7878/query"
	client, err := oxigraph.New(queryURL)
	if err != nil {
		fatal(fmt.Errorf("connect: %w", err))
	}
	defer client.Close()

	markers, err := client.SeedMarkers(ctx)
	if err != nil {
		fatal(fmt.Errorf("SeedMarkers: %w", err))
	}
	fmt.Printf("live seed markers found: %d\n", len(markers))
	if len(markers) != 1 {
		fatal(fmt.Errorf("expected exactly 1 live seed marker, got %d", len(markers)))
	}
	marker := markers[0]

	liveCount, err := client.CountTriples(ctx)
	if err != nil {
		fatal(fmt.Errorf("CountTriples: %w", err))
	}
	fmt.Printf("marker: digest=%s iri=%s triple_count=%d\n", marker.Digest, marker.IRI, marker.TripleCount)
	fmt.Printf("live store CountTriples: %d\n", liveCount)
	if marker.TripleCount != liveCount {
		fatal(fmt.Errorf("marker triple count %d != live store count %d", marker.TripleCount, liveCount))
	}

	candidateDir := "scratchpad/provenance/graph-marker-incident/candidate"
	if err := os.MkdirAll(candidateDir, 0o755); err != nil {
		fatal(err)
	}
	candidatePath := candidateDir + "/graph-authority.json"
	if err := seedmeta.WriteMarkerFile(candidatePath, marker); err != nil {
		fatal(fmt.Errorf("write candidate marker: %w", err))
	}
	fmt.Printf("candidate marker written: %s\n", candidatePath)

	// Verify the candidate the exact same way the running server verifies
	// freshness: VerifyLiveStore compares an "expected" marker (what we
	// just derived) against what it independently re-discovers live.
	ver := seedmeta.VerifyLiveStore(ctx, client, marker)
	verJSON, _ := json.MarshalIndent(struct {
		State           string
		ExpectedDigest  string
		LiveDigest      string
		LiveTripleCount int64
		MarkerPresent   bool
		SeedBuildCount  int64
		Detail          string
	}{
		State:           ver.State.String(),
		ExpectedDigest:  ver.Expected.Digest,
		LiveDigest:      ver.Live.Digest,
		LiveTripleCount: ver.LiveTripleCount,
		MarkerPresent:   ver.MarkerPresent,
		SeedBuildCount:  ver.SeedBuildCount,
		Detail:          ver.Detail,
	}, "", "  ")
	fmt.Println(string(verJSON))

	if ver.State != seedmeta.FreshnessCurrent {
		fatal(fmt.Errorf("candidate does not verify as current: %s", ver.Detail))
	}
	fmt.Println("VERIFIED: candidate marker is current against the live store")

	// Re-read the candidate file back and re-verify from disk, so the
	// verification exercises the exact artifact that would be installed.
	fromDisk, err := seedmeta.ReadMarkerFile(candidatePath)
	if err != nil {
		fatal(fmt.Errorf("re-read candidate marker: %w", err))
	}
	if fromDisk.Digest != marker.Digest || fromDisk.IRI != marker.IRI || fromDisk.TripleCount != marker.TripleCount {
		fatal(fmt.Errorf("candidate file on disk does not match derived marker"))
	}
	ver2 := seedmeta.VerifyLiveStore(ctx, client, fromDisk)
	if ver2.State != seedmeta.FreshnessCurrent {
		fatal(fmt.Errorf("on-disk candidate does not verify as current: %s", ver2.Detail))
	}
	fmt.Println("VERIFIED (from disk): on-disk candidate marker is current against the live store")
}
