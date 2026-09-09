// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"net"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/globulario/sensei/golang/gitobject"
	"strings"
	"testing"
	"time"

	awarenesspb "github.com/globulario/sensei/golang/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// A PUBLISHED COMMIT IDENTITY IS A FULL-WIDTH GIT OBJECT ID.
//
// service-build stamped BUILD_COMMIT and SRC_COMMIT with `git rev-parse
// --short=12`, so every consumer of Metadata received a 12-character
// abbreviation. An abbreviation is a different KIND of fact from an object id:
// it names a commit only probabilistically, and it cannot be compared for
// equality against one written out in full.
//
// Downstream, sensei-code binds an architecture turn to {task, objective
// digest, base, graph build commit} and requires a full object id of the last.
// The abbreviated stamp could never satisfy it, and the failure presented as
// "this graph has no identity" rather than as "this identity is abbreviated" --
// which is the expensive kind of wrong, because it points at the wrong repo.
//
// This test is deliberately NOT a grep for `rev-parse HEAD` in the Makefile.
// A recipe that looks right and a binary that publishes the right value are
// two different claims, and only the second one is what a consumer reads. So
// it runs the real target, starts the real binary, and asks the real Metadata
// RPC.

// abbreviatedObjectID is what the defect looked like: hex, lowercase, and far
// too short to be an identity.
//
// WIDTH IS NOT PINNED TO 40 HERE. These tests asserted `^[0-9a-f]{40}$`, which
// rejects the exact value the Makefile publishes in a repository initialized
// with `git init --object-format=sha256` -- a documented, supported format
// whose `git rev-parse HEAD` returns 64 hex. It also contradicted
// namesRepository, which already accepted both widths, so one value was valid
// evidence in one function and malformed in the next. Both now ask
// gitobject.IsObjectID, which is the single place that answers this.
var abbreviatedObjectID = regexp.MustCompile(`^[0-9a-f]{4,39}$`)

func repoRootForStampTest(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func gitHead(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD in %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out))
}

func freeLoopbackPort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// buildStampedServer runs the REAL service-build target and returns the binary
// it produced. Running the target rather than reproducing its ldflags is the
// point: a test that computed the stamp itself would pass no matter what the
// recipe did.
func buildStampedServer(t *testing.T, root string) string {
	t.Helper()
	if testing.Short() {
		t.Skip("service-build is a real link; skipped under -short")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skipf("make is not available: %v", err)
	}
	cmd := exec.Command("make", "service-build")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make service-build: %v\n%s", err, out)
	}
	return filepath.Join(root, "bin", "awareness-graph")
}

// metadataFrom starts the built binary and asks it what it is.
//
// Pointed at an unreachable backend with -allow-stale-seed, so the answer comes
// from the binary's own stamp and depends on no store, no seed and no network.
func metadataFrom(t *testing.T, binary string) *awarenesspb.MetadataResponse {
	t.Helper()
	addr := freeLoopbackPort(t)

	srv := exec.Command(binary,
		"-addr", addr,
		"-oxigraph-url", "http://127.0.0.1:1/query",
		"-allow-stale-seed",
	)
	if err := srv.Start(); err != nil {
		t.Fatalf("start the stamped server: %v", err)
	}
	t.Cleanup(func() {
		_ = srv.Process.Kill()
		_, _ = srv.Process.Wait()
	})

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial the stamped server: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	client := awarenesspb.NewAwarenessGraphClient(conn)
	deadline := time.Now().Add(30 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		resp, err := client.Metadata(ctx, &awarenesspb.MetadataRequest{})
		cancel()
		if err == nil {
			return resp
		}
		if time.Now().After(deadline) {
			t.Fatalf("the stamped server never answered Metadata: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestTheStampedBinaryPublishesACanonicalGraphBuildCommit(t *testing.T) {
	root := repoRootForStampTest(t)
	resp := metadataFrom(t, buildStampedServer(t, root))

	got := resp.GetGraphBuildCommit()
	if got == "" {
		t.Fatal("the stamped binary publishes no graph_build_commit")
	}
	if abbreviatedObjectID.MatchString(got) {
		t.Fatalf("graph_build_commit %q is an abbreviated object id (%d chars); "+
			"a published commit identity is the full-width form, "+
			"and an abbreviation cannot be compared for equality with one", got, len(got))
	}
	if !gitobject.IsObjectID(got) {
		t.Fatalf("graph_build_commit %q is not a full-width git object id (%d chars)", got, len(got))
	}

	// And it is THIS commit, not merely a well-shaped one.
	if want := gitHead(t, root); got != want {
		t.Fatalf("graph_build_commit names %s, but the binary was built from %s", got, want)
	}
}

// The same rule for the services commit, WITHOUT inventing a value for it.
//
// Empty is the accepted state since #344 -- a Sensei built with no services
// checkout has no true value for that field, and the serving binary's stamp is
// about the serving binary. What is not accepted is an abbreviation: if the
// field is published at all, it is published as an object id.
func TestAPublishedSourceRepoCommitIsCanonicalOrAbsent(t *testing.T) {
	root := repoRootForStampTest(t)
	resp := metadataFrom(t, buildStampedServer(t, root))

	got := strings.TrimSpace(resp.GetSourceRepoCommit())
	if got == "" {
		t.Log("source_repo_commit is empty; no services checkout was resolvable at build time, " +
			"which is honest rather than abbreviated")
		return
	}
	if abbreviatedObjectID.MatchString(got) {
		t.Fatalf("source_repo_commit %q is an abbreviated object id (%d chars)", got, len(got))
	}
	if !gitobject.IsObjectID(got) {
		t.Fatalf("source_repo_commit %q is not a full-width git object id (%d chars)", got, len(got))
	}
}

// The predicate the abbreviation defeated, stated here so the two halves of the
// contract live next to each other: a consumer requiring a canonical object id
// must reject the abbreviated form outright rather than accepting a prefix.
func TestAnAbbreviatedCommitIsNotACanonicalIdentity(t *testing.T) {
	full := "9004725eb6c0a77830fea592adb0839ce0d86ec1"
	short := full[:12]

	if !gitobject.IsObjectID(full) {
		t.Fatal("a 40-character object id is not being recognized as canonical")
	}
	if gitobject.IsObjectID(short) {
		t.Fatal("a 12-character prefix is being accepted as a canonical identity")
	}
	if !abbreviatedObjectID.MatchString(short) {
		t.Fatal("a 12-character prefix is not being recognized as abbreviated")
	}
	if full[:12] != short {
		t.Fatal("the prefix relationship this defect rested on no longer holds")
	}
}
