// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/globulario/awareness-graph/golang/governancepack"
	"github.com/globulario/awareness-graph/golang/seedmeta"
)

func runGovernance(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, `Usage: awg governance <subcommand> [flags]

Subcommands:
  init                        Create local governance directory and trust-store scaffold
  fetch                       Fetch a signed pack into local governance storage
  publish                     Build/sign/release vendor governance-pack artifacts
  trust add --file <path>     Install or merge trusted publishers
  trust list                  List trusted publishers and keys
  trust show <publisher-id>   Show one trusted publisher entry
  trust rotate-check          Show trust rotation warnings
  verify-pack <path-or-dir>   Verify a governance pack locally
  activate <path-or-dir>      Verify, stage, build, load, and activate a governance pack
  status                      Show active governance-pack and local graph state
`)
		return 2
	}
	switch args[0] {
	case "init":
		return runGovernanceInit(args[1:])
	case "fetch":
		return runGovernanceFetch(args[1:])
	case "publish":
		return runGovernancePublish(args[1:])
	case "trust":
		return runGovernanceTrust(args[1:])
	case "verify-pack":
		return runGovernanceVerifyPack(args[1:])
	case "activate":
		return runGovernanceActivate(args[1:])
	case "status":
		return runGovernanceStatus(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "awg governance: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runGovernanceFetch(args []string) int {
	fs := flag.NewFlagSet("awg governance fetch", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootFlag := fs.String("project-root", "", "project root (auto-detect)")
	trustedKeysFlag := fs.String("trusted-keys", "", "path to trusted publisher key set (default: <project>/.awg/governance/trusted-publishers.json)")
	source := fs.String("source", "", "publication root dir or base URL")
	packID := fs.String("pack-id", "", "governance pack id")
	packVersion := fs.String("pack-version", "", "governance pack version")
	channel := fs.String("channel", "", "publication channel pointer such as stable")
	activate := fs.Bool("activate", false, "activate the fetched pack after local verification")
	storeURL := fs.String("store-url", "http://localhost:7878/store?default", "Oxigraph Graph Store endpoint when --activate is set")
	graphMarkerFile := fs.String("graph-marker-file", "", "write verified live graph identity to this file after a successful activation")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	root, err := resolveProjectRoot(*rootFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance fetch: resolve project root: %v\n", err)
		return 1
	}
	trustedKeysPath := strings.TrimSpace(*trustedKeysFlag)
	if trustedKeysPath == "" {
		trustedKeysPath = governancepack.TrustedKeysPath(root)
	}
	verified, fetched, err := fetchGovernancePack(*source, trustedKeysPath, root, *packID, *packVersion, *channel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance fetch: %v\n", err)
		return 1
	}
	fmt.Printf("Fetched pack:       %s %s\n", verified.Manifest.PackID, verified.Manifest.PackVersion)
	fmt.Printf("Publisher:          %s\n", verified.Manifest.Publisher.ID)
	fmt.Printf("Payload digest:     %s\n", verified.PayloadMarker.Digest)
	fmt.Printf("Fetched dir:        %s\n", fetched.Dir)
	if !*activate {
		fmt.Println("Fetch:              ok")
		return 0
	}
	return runGovernanceActivate([]string{
		"-project-root", root,
		"-trusted-keys", trustedKeysPath,
		"-store-url", *storeURL,
		"-graph-marker-file", *graphMarkerFile,
		fetched.Dir,
	})
}

func runGovernancePublish(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, `Usage: awg governance publish <subcommand> [flags]

Subcommands:
  gen-key  Generate a local publisher signing key for governance-pack publication
  trust-root  Derive a trusted-publishers.json bootstrap from a signing key
  build    Build an unsigned governance pack from canonical local inputs
  sign     Sign the governance-pack manifest with a local publisher key
  release  Verify and publish a signed pack into an immutable local publication root
`)
		return 2
	}
	switch args[0] {
	case "gen-key":
		return runGovernancePublishGenKey(args[1:])
	case "trust-root":
		return runGovernancePublishTrustRoot(args[1:])
	case "build":
		return runGovernancePublishBuild(args[1:])
	case "sign":
		return runGovernancePublishSign(args[1:])
	case "release":
		return runGovernancePublishRelease(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "awg governance publish: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runGovernancePublishGenKey(args []string) int {
	fs := flag.NewFlagSet("awg governance publish gen-key", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	out := fs.String("out", "", "output path for awg.signing-key.v1 JSON")
	publisherID := fs.String("publisher-id", "", "publisher id")
	keyID := fs.String("key-id", "", "publisher signing key id")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*out) == "" || strings.TrimSpace(*publisherID) == "" || strings.TrimSpace(*keyID) == "" {
		fmt.Fprintln(os.Stderr, "Usage: awg governance publish gen-key --out <path> --publisher-id <id> --key-id <id>")
		return 2
	}
	key, err := generateGovernanceSigningKey(*out, *publisherID, *keyID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance publish gen-key: %v\n", err)
		return 1
	}
	fmt.Printf("Signing key:        %s\n", *out)
	fmt.Printf("Publisher:          %s\n", key.PublisherID)
	fmt.Printf("Key id:             %s\n", key.KeyID)
	fmt.Println("Generate:           ok")
	return 0
}

func runGovernancePublishTrustRoot(args []string) int {
	fs := flag.NewFlagSet("awg governance publish trust-root", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	signingKey := fs.String("signing-key", "", "path to awg.signing-key.v1 JSON")
	out := fs.String("out", "", "output path for trusted-publishers.json")
	displayName := fs.String("display-name", "", "optional publisher display name")
	status := fs.String("status", "active", "trusted key status: active|deprecated|revoked|future")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*signingKey) == "" || strings.TrimSpace(*out) == "" {
		fmt.Fprintln(os.Stderr, "Usage: awg governance publish trust-root --signing-key <path> --out <path> [--display-name <name>] [--status active]")
		return 2
	}
	store, err := buildGovernanceTrustRoot(*signingKey, *out, *displayName, *status)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance publish trust-root: %v\n", err)
		return 1
	}
	fmt.Printf("Trust root:         %s\n", *out)
	fmt.Printf("Trusted publishers: %d\n", len(store.Publishers))
	for _, p := range store.Publishers {
		fmt.Printf("%s  %s (%d key(s))\n", p.PublisherID, displayTrustPublisherName(p.DisplayName), len(p.Keys))
	}
	fmt.Println("Generate:           ok")
	return 0
}

func runGovernancePublishBuild(args []string) int {
	fs := flag.NewFlagSet("awg governance publish build", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var inputDirs stringSlice
	fs.Var(&inputDirs, "input", "canonical governance YAML directory (repeatable)")
	inputNT := fs.String("input-nt", "", "prebuilt canonical governance N-Triples input")
	outDir := fs.String("out-dir", "", "output directory for governance-pack artifacts")
	packID := fs.String("pack-id", "", "governance pack id")
	packVersion := fs.String("pack-version", "", "governance pack version")
	publisherID := fs.String("publisher-id", "", "publisher id")
	publisherName := fs.String("publisher-name", "", "publisher display name")
	issuedAt := fs.String("issued-at", time.Now().UTC().Format(time.RFC3339), "manifest issued_at timestamp")
	minAWGVersion := fs.String("min-awg-version", Version, "minimum compatible AWG version")
	maxAWGVersion := fs.String("max-awg-version", "", "maximum compatible AWG version")
	keyID := fs.String("key-id", "", "publisher signing key id recorded in the manifest")
	corpusDigest := fs.String("corpus-digest", "", "optional corpus digest for provenance")
	promotionBatchID := fs.String("promotion-batch-id", "", "optional promotion batch id")
	strict := fs.Bool("strict", false, "fail on unrecognized YAML schemas when building from --input")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	payloadInput, err := readPackBuildInput(*inputNT, inputDirs, *strict)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance publish build: %v\n", err)
		return 1
	}
	marker, err := buildGovernancePackFromBytes(payloadInput, governancePublishBuildOptions{
		PackID:             *packID,
		PackVersion:        *packVersion,
		PublisherID:        *publisherID,
		PublisherName:      *publisherName,
		IssuedAt:           *issuedAt,
		MinAWGVersion:      *minAWGVersion,
		MaxAWGVersion:      *maxAWGVersion,
		KeyID:              *keyID,
		CorpusDigestSHA256: *corpusDigest,
		PromotionBatchID:   *promotionBatchID,
		OutputDir:          *outDir,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance publish build: %v\n", err)
		return 1
	}
	fmt.Printf("Output dir:         %s\n", *outDir)
	fmt.Printf("Pack id:            %s\n", *packID)
	fmt.Printf("Pack version:       %s\n", *packVersion)
	fmt.Printf("Payload digest:     %s\n", marker.Digest)
	fmt.Printf("Payload triples:    %d\n", marker.TripleCount)
	fmt.Printf("Payload marker:     %s\n", marker.IRI)
	fmt.Println("Build:              ok")
	return 0
}

func runGovernancePublishSign(args []string) int {
	fs := flag.NewFlagSet("awg governance publish sign", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	signingKey := fs.String("signing-key", "", "path to awg.signing-key.v1 JSON file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || strings.TrimSpace(*signingKey) == "" {
		fmt.Fprintln(os.Stderr, "Usage: awg governance publish sign --signing-key <path> <pack-dir-or-manifest>")
		return 2
	}
	manifest, sig, err := signGovernancePack(fs.Arg(0), *signingKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance publish sign: %v\n", err)
		return 1
	}
	fmt.Printf("Pack id:            %s\n", manifest.PackID)
	fmt.Printf("Pack version:       %s\n", manifest.PackVersion)
	fmt.Printf("Publisher:          %s\n", manifest.Publisher.ID)
	fmt.Printf("Key id:             %s\n", manifest.Signature.KeyID)
	fmt.Printf("Signature bytes:    %d\n", len(sig))
	fmt.Println("Sign:               ok")
	return 0
}

func runGovernancePublishRelease(args []string) int {
	fs := flag.NewFlagSet("awg governance publish release", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	trustedKeys := fs.String("trusted-keys", "", "path to trusted-publishers.json used to verify published packs")
	signingKey := fs.String("signing-key", "", "path to awg.signing-key.v1 JSON file used to sign the publication index")
	publicationRoot := fs.String("publication-root", "", "output root for immutable published governance artifacts")
	var channels stringSlice
	fs.Var(&channels, "channel", "channel pointer to update for this pack release (repeatable)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || strings.TrimSpace(*trustedKeys) == "" || strings.TrimSpace(*publicationRoot) == "" || strings.TrimSpace(*signingKey) == "" {
		fmt.Fprintln(os.Stderr, "Usage: awg governance publish release --trusted-keys <path> --signing-key <path> --publication-root <dir> [--channel stable] <pack-dir-or-manifest>")
		return 2
	}
	verified, indexPath, err := releaseGovernancePack(fs.Arg(0), *trustedKeys, *publicationRoot, *signingKey, channels)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance publish release: %v\n", err)
		return 1
	}
	fmt.Printf("Publication root:   %s\n", *publicationRoot)
	fmt.Printf("Pack id:            %s\n", verified.Manifest.PackID)
	fmt.Printf("Pack version:       %s\n", verified.Manifest.PackVersion)
	fmt.Printf("Payload digest:     %s\n", verified.PayloadMarker.Digest)
	fmt.Printf("Index:              %s\n", indexPath)
	fmt.Printf("Trust root:         %s\n", filepath.Join(*publicationRoot, "trusted-publishers.json"))
	if len(channels) > 0 {
		fmt.Printf("Channels:           %s\n", strings.Join(channels, ", "))
	}
	fmt.Println("Release:            ok")
	return 0
}

func runGovernanceInit(args []string) int {
	fs := flag.NewFlagSet("awg governance init", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootFlag := fs.String("project-root", "", "project root (auto-detect)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	root, err := resolveProjectRoot(*rootFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance init: resolve project root: %v\n", err)
		return 1
	}
	dir := governancepack.GovernanceDirPath(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "awg governance init: %v\n", err)
		return 1
	}
	trustPath := governancepack.TrustedKeysPath(root)
	if _, err := os.Stat(trustPath); os.IsNotExist(err) {
		store := governancepack.TrustStore{SchemaVersion: governancepack.TrustStoreSchemaV1, Publishers: []governancepack.TrustedPublisher{}}
		if err := governancepack.WriteTrustStore(trustPath, store); err != nil {
			fmt.Fprintf(os.Stderr, "awg governance init: write trust store: %v\n", err)
			return 1
		}
	}
	fmt.Printf("Governance dir:      %s\n", dir)
	fmt.Printf("Trust store:         %s\n", trustPath)
	fmt.Println("Initialization:      ok")
	return 0
}

func runGovernanceTrust(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, `Usage: awg governance trust <subcommand> [flags]

Subcommands:
  add --file <path>
  fetch --source <path-or-url>
  list
  show <publisher-id>
  rotate-check
`)
		return 2
	}
	switch args[0] {
	case "add":
		return runGovernanceTrustAdd(args[1:])
	case "fetch":
		return runGovernanceTrustFetch(args[1:])
	case "list":
		return runGovernanceTrustList(args[1:])
	case "show":
		return runGovernanceTrustShow(args[1:])
	case "rotate-check":
		return runGovernanceTrustRotateCheck(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "awg governance trust: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runGovernanceTrustFetch(args []string) int {
	fs := flag.NewFlagSet("awg governance trust fetch", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootFlag := fs.String("project-root", "", "project root (auto-detect)")
	sourceFlag := fs.String("source", "", "directory, file, or base URL for trusted-publishers.json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*sourceFlag) == "" {
		fmt.Fprintln(os.Stderr, "Usage: awg governance trust fetch --source <path-or-url>")
		return 2
	}
	root, err := resolveProjectRoot(*rootFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance trust fetch: resolve project root: %v\n", err)
		return 1
	}
	store, stagePath, err := fetchTrustStoreCandidate(*sourceFlag, root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance trust fetch: %v\n", err)
		return 1
	}
	fmt.Printf("Fetched trust root:  %s\n", stagePath)
	fmt.Printf("Publishers:          %d\n", len(store.Publishers))
	for _, p := range store.Publishers {
		fmt.Printf("%s  %s (%d key(s))\n", p.PublisherID, displayTrustPublisherName(p.DisplayName), len(p.Keys))
	}
	fmt.Println("Fetch:               ok")
	fmt.Printf("Next step:           awg governance trust add --file %s\n", stagePath)
	return 0
}

func runGovernanceTrustAdd(args []string) int {
	fs := flag.NewFlagSet("awg governance trust add", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootFlag := fs.String("project-root", "", "project root (auto-detect)")
	fileFlag := fs.String("file", "", "path to trusted-publishers.json")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*fileFlag) == "" {
		fmt.Fprintln(os.Stderr, "Usage: awg governance trust add --file <path>")
		return 2
	}
	root, err := resolveProjectRoot(*rootFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance trust add: resolve project root: %v\n", err)
		return 1
	}
	incoming, err := governancepack.LoadTrustStore(*fileFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance trust add: %v\n", err)
		return 1
	}
	target := governancepack.TrustedKeysPath(root)
	existing := governancepack.TrustStore{SchemaVersion: governancepack.TrustStoreSchemaV1, Publishers: []governancepack.TrustedPublisher{}}
	if _, err := os.Stat(target); err == nil {
		existing, err = governancepack.LoadTrustStore(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "awg governance trust add: load existing trust store: %v\n", err)
			return 1
		}
	}
	merged, err := governancepack.MergeTrustStore(existing, incoming)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance trust add: %v\n", err)
		return 1
	}
	if err := governancepack.WriteTrustStore(target, merged); err != nil {
		fmt.Fprintf(os.Stderr, "awg governance trust add: %v\n", err)
		return 1
	}
	fmt.Printf("Trust store:         %s\n", target)
	fmt.Printf("Trusted publishers:  %d\n", len(merged.Publishers))
	return 0
}

func runGovernanceTrustList(args []string) int {
	fs := flag.NewFlagSet("awg governance trust list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootFlag := fs.String("project-root", "", "project root (auto-detect)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	root, err := resolveProjectRoot(*rootFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance trust list: resolve project root: %v\n", err)
		return 1
	}
	store, err := governancepack.LoadTrustStore(governancepack.TrustedKeysPath(root))
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance trust list: %v\n", err)
		return 1
	}
	fmt.Printf("Trust store:         %s\n", governancepack.TrustedKeysPath(root))
	for _, p := range store.Publishers {
		fmt.Printf("%s  %s\n", p.PublisherID, displayTrustPublisherName(p.DisplayName))
		for _, k := range p.Keys {
			fmt.Printf("  - %s %s status=%s", k.KeyID, k.Algorithm, governancepack.NormalizedKeyStatusForDisplay(k.Status))
			if k.ValidFrom != "" {
				fmt.Printf(" valid_from=%s", k.ValidFrom)
			}
			if k.ValidUntil != "" {
				fmt.Printf(" valid_until=%s", k.ValidUntil)
			}
			fmt.Println()
		}
	}
	return 0
}

func displayTrustPublisherName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "(no display name)"
	}
	return name
}

func runGovernanceTrustShow(args []string) int {
	fs := flag.NewFlagSet("awg governance trust show", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootFlag := fs.String("project-root", "", "project root (auto-detect)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Usage: awg governance trust show <publisher-id>")
		return 2
	}
	root, err := resolveProjectRoot(*rootFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance trust show: resolve project root: %v\n", err)
		return 1
	}
	store, err := governancepack.LoadTrustStore(governancepack.TrustedKeysPath(root))
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance trust show: %v\n", err)
		return 1
	}
	want := fs.Arg(0)
	for _, p := range store.Publishers {
		if p.PublisherID != want {
			continue
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(p)
		return 0
	}
	fmt.Fprintf(os.Stderr, "awg governance trust show: publisher %s not found\n", want)
	return 1
}

func runGovernanceTrustRotateCheck(args []string) int {
	fs := flag.NewFlagSet("awg governance trust rotate-check", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootFlag := fs.String("project-root", "", "project root (auto-detect)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	root, err := resolveProjectRoot(*rootFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance trust rotate-check: resolve project root: %v\n", err)
		return 1
	}
	store, err := governancepack.LoadTrustStore(governancepack.TrustedKeysPath(root))
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance trust rotate-check: %v\n", err)
		return 1
	}
	active, _ := readActiveGovernance(root)
	findings := governancepack.RotationCheck(store, active, time.Now().UTC())
	if len(findings) == 0 {
		fmt.Println("Rotation check:      ok")
		return 0
	}
	exit := 0
	for _, f := range findings {
		fmt.Printf("%s: %s %s %s\n", f.Severity, f.PublisherID, f.KeyID, f.Message)
		if f.Severity == "critical" {
			exit = 1
		}
	}
	return exit
}

func runGovernanceVerifyPack(args []string) int {
	fs := flag.NewFlagSet("awg governance verify-pack", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootFlag := fs.String("project-root", "", "project root (auto-detect)")
	trustedKeysFlag := fs.String("trusted-keys", "", "path to trusted publisher key set (default: <project>/.awg/governance/trusted-publishers.json)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Usage: awg governance verify-pack [flags] <path-or-dir>")
		return 2
	}
	root, err := resolveProjectRoot(*rootFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance verify-pack: resolve project root: %v\n", err)
		return 1
	}
	trustedKeysPath := strings.TrimSpace(*trustedKeysFlag)
	if trustedKeysPath == "" {
		trustedKeysPath = governancepack.TrustedKeysPath(root)
	}
	verified, err := governancepack.VerifyPack(fs.Arg(0), trustedKeysPath, Version)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance verify-pack: %v\n", err)
		return 1
	}
	fmt.Printf("Pack id:            %s\n", verified.Manifest.PackID)
	fmt.Printf("Pack version:       %s\n", verified.Manifest.PackVersion)
	fmt.Printf("Publisher:          %s\n", verified.Manifest.Publisher.ID)
	fmt.Printf("Payload digest:     %s\n", verified.PayloadMarker.Digest)
	fmt.Printf("Payload triples:    %d\n", verified.PayloadMarker.TripleCount)
	fmt.Printf("Payload marker:     %s\n", verified.PayloadMarker.IRI)
	fmt.Printf("Manifest digest:    %s\n", verified.ManifestDigestSHA256)
	fmt.Printf("Trusted key:        %s/%s\n", verified.PublisherKey.Algorithm, verified.PublisherKey.KeyID)
	fmt.Println("Verification:       ok")
	return 0
}

func runGovernanceActivate(args []string) int {
	fs := flag.NewFlagSet("awg governance activate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootFlag := fs.String("project-root", "", "project root (auto-detect)")
	trustedKeysFlag := fs.String("trusted-keys", "", "path to trusted publisher key set (default: <project>/.awg/governance/trusted-publishers.json)")
	storeURL := fs.String("store-url", "http://localhost:7878/store?default", "Oxigraph Graph Store endpoint")
	graphMarkerFile := fs.String("graph-marker-file", "", "write verified live graph identity to this file after a successful load (default: <project>/.awg/graph-authority.json)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Usage: awg governance activate [flags] <path-or-dir>")
		return 2
	}
	root, err := resolveProjectRoot(*rootFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance activate: resolve project root: %v\n", err)
		return 1
	}
	trustedKeysPath := strings.TrimSpace(*trustedKeysFlag)
	if trustedKeysPath == "" {
		trustedKeysPath = governancepack.TrustedKeysPath(root)
	}
	prevActive, _ := readActiveGovernance(root)
	logPath := governancepack.ActivationLogPath(root)
	verified, err := governancepack.VerifyPack(fs.Arg(0), trustedKeysPath, Version)
	if err != nil {
		appendGovernanceFailureLog(logPath, prevActive, nil, "", "", err)
		fmt.Fprintf(os.Stderr, "awg governance activate: %v\n", err)
		return 1
	}
	staged, err := stageGovernancePack(root, verified)
	if err != nil {
		appendGovernanceFailureLog(logPath, prevActive, &verified, governancepack.FailureActivationIncomplete, err.Error(), err)
		fmt.Fprintf(os.Stderr, "awg governance activate: %v\n", err)
		return 1
	}
	verified.Paths = staged
	combinedNT, marker, err := buildCombinedGovernedArtifact(root, &verified)
	if err != nil {
		appendGovernanceFailureLog(logPath, prevActive, &verified, governancepack.FailureActivationIncomplete, err.Error(), err)
		fmt.Fprintf(os.Stderr, "awg governance activate: %v\n", err)
		return 1
	}
	endpoint, err := normalizeStoreURL(*storeURL)
	if err != nil {
		appendGovernanceFailureLog(logPath, prevActive, &verified, governancepack.FailureActivationIncomplete, err.Error(), err)
		fmt.Fprintf(os.Stderr, "awg governance activate: invalid --store-url: %v\n", err)
		return 1
	}
	if err := uploadNTriples(httpDefaultClient(), endpoint, combinedNT); err != nil {
		appendGovernanceFailureLog(logPath, prevActive, &verified, governancepack.FailureGraphDown, err.Error(), err)
		fmt.Fprintf(os.Stderr, "awg governance activate: upload to %s: %v\n", endpoint, err)
		return 1
	}
	if err := verifyLoadedGraph(endpoint, combinedNT); err != nil {
		appendGovernanceFailureLog(logPath, prevActive, &verified, governancepack.FailureGraphUnknown, err.Error(), err)
		fmt.Fprintf(os.Stderr, "awg governance activate: live-store verification failed: %v\n", err)
		return 1
	}
	markerPath := strings.TrimSpace(*graphMarkerFile)
	if markerPath == "" {
		markerPath = seedmeta.RuntimeMarkerPath(root)
	}
	if err := seedmeta.WriteMarkerFile(markerPath, marker); err != nil {
		appendGovernanceFailureLog(logPath, prevActive, &verified, governancepack.FailureActivationIncomplete, err.Error(), err)
		fmt.Fprintf(os.Stderr, "awg governance activate: publish graph marker: %v\n", err)
		return 1
	}
	active := governancepack.ActiveRecord{
		SchemaVersion:             governancepack.ActiveRecordSchemaV1,
		PackID:                    verified.Manifest.PackID,
		PackVersion:               verified.Manifest.PackVersion,
		PublisherID:               verified.Manifest.Publisher.ID,
		PayloadDigestSHA256:       verified.PayloadMarker.Digest,
		PayloadTripleCount:        verified.PayloadMarker.TripleCount,
		PayloadMarkerIRI:          verified.PayloadMarker.IRI,
		ActivatedAt:               time.Now().UTC().Format(time.RFC3339),
		ManifestPath:              relTo(root, verified.Paths.ManifestPath),
		ManifestDigestSHA256:      verified.ManifestDigestSHA256,
		CombinedGraphDigestSHA256: marker.Digest,
		CombinedGraphTripleCount:  marker.TripleCount,
	}
	if err := governancepack.WriteActiveRecord(governancepack.ActiveRecordPath(root), active); err != nil {
		appendGovernanceFailureLog(logPath, prevActive, &verified, governancepack.FailureActivationIncomplete, err.Error(), err)
		fmt.Fprintf(os.Stderr, "awg governance activate: write active record: %v\n", err)
		return 1
	}
	_ = governancepack.AppendActivationLog(logPath, governancepack.ActivationLogEntry{
		AttemptedPackID:      verified.Manifest.PackID,
		AttemptedPackVersion: verified.Manifest.PackVersion,
		PublisherID:          verified.Manifest.Publisher.ID,
		ManifestDigestSHA256: verified.ManifestDigestSHA256,
		PayloadDigestSHA256:  verified.PayloadMarker.Digest,
		PreviousActivePack:   prevActive,
		Result:               "success",
		ResultingActivePack:  &active,
	})
	fmt.Printf("Activated pack:      %s %s\n", active.PackID, active.PackVersion)
	fmt.Printf("Publisher:           %s\n", active.PublisherID)
	fmt.Printf("Payload digest:      %s\n", active.PayloadDigestSHA256)
	fmt.Printf("Combined graph:      %s (%d triples)\n", active.CombinedGraphDigestSHA256, active.CombinedGraphTripleCount)
	fmt.Printf("Graph marker file:   %s\n", markerPath)
	fmt.Printf("Activation log:      %s\n", logPath)
	return 0
}

func runGovernanceStatus(args []string) int {
	fs := flag.NewFlagSet("awg governance status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	rootFlag := fs.String("project-root", "", "project root (auto-detect)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	root, err := resolveProjectRoot(*rootFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awg governance status: resolve project root: %v\n", err)
		return 1
	}
	status := governancepack.AssessLocalStatus(root, Version)
	fmt.Printf("Managed mode:        %v\n", governancepack.ManagedModeEnabled(root))
	fmt.Printf("Governance state:    %s\n", status.State)
	if status.Detail != "" {
		fmt.Printf("State detail:        %s\n", status.Detail)
	}
	if status.Active != nil {
		fmt.Printf("Active pack id:      %s\n", status.Active.PackID)
		fmt.Printf("Active pack version: %s\n", status.Active.PackVersion)
		fmt.Printf("Publisher:           %s\n", status.Active.PublisherID)
		fmt.Printf("Payload digest:      %s\n", status.Active.PayloadDigestSHA256)
		fmt.Printf("Activated at:        %s\n", status.Active.ActivatedAt)
	}
	fmt.Printf("Fetched state:       %s\n", status.FetchedState)
	if status.FetchedDetail != "" {
		fmt.Printf("Fetched detail:      %s\n", status.FetchedDetail)
	}
	if status.LatestFetched != nil {
		fmt.Printf("Fetched pack id:     %s\n", status.LatestFetched.PackID)
		fmt.Printf("Fetched version:     %s\n", status.LatestFetched.PackVersion)
		fmt.Printf("Fetched publisher:   %s\n", status.LatestFetched.PublisherID)
		fmt.Printf("Fetched source:      %s\n", status.LatestFetched.Source)
		if status.LatestFetched.RequestedChannel != "" {
			fmt.Printf("Fetched channel:     %s\n", status.LatestFetched.RequestedChannel)
		}
		fmt.Printf("Fetched at:          %s\n", status.LatestFetched.FetchedAt)
	}
	fmt.Printf("Staged trust state:  %s\n", status.StagedTrustState)
	if status.StagedTrustDetail != "" {
		fmt.Printf("Staged trust detail: %s\n", status.StagedTrustDetail)
	}
	if status.StagedTrust != nil {
		fmt.Printf("Staged trust src:    %s\n", status.StagedTrust.Source)
		fmt.Printf("Staged trust at:     %s\n", status.StagedTrust.FetchedAt)
		fmt.Printf("Staged publishers:   %d\n", status.StagedTrust.PublisherCount)
	}
	if status.CombinedGraph.Digest != "" {
		fmt.Printf("Combined digest:     %s\n", status.CombinedGraph.Digest)
		fmt.Printf("Combined triples:    %d\n", status.CombinedGraph.TripleCount)
	}
	fmt.Printf("Trust store:         %s\n", governancepack.TrustedKeysPath(root))
	fmt.Printf("Active record:       %s\n", governancepack.ActiveRecordPath(root))
	fmt.Printf("Graph authority:     %s\n", seedmeta.RuntimeMarkerPath(root))
	return 0
}

func readActiveGovernance(root string) (*governancepack.ActiveRecord, error) {
	rec, err := governancepack.ReadActiveRecord(governancepack.ActiveRecordPath(root))
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func appendGovernanceFailureLog(path string, prev *governancepack.ActiveRecord, verified *governancepack.VerifiedPack, failureState, detail string, err error) {
	entry := governancepack.ActivationLogEntry{
		Result:             "failure",
		PreviousActivePack: prev,
		FailureState:       failureState,
		FailureDetail:      detail,
	}
	if verified != nil {
		entry.AttemptedPackID = verified.Manifest.PackID
		entry.AttemptedPackVersion = verified.Manifest.PackVersion
		entry.PublisherID = verified.Manifest.Publisher.ID
		entry.ManifestDigestSHA256 = verified.ManifestDigestSHA256
		entry.PayloadDigestSHA256 = verified.PayloadMarker.Digest
	}
	if entry.FailureState == "" && err != nil {
		entry.FailureState = inferGovernanceFailureState(err)
	}
	if entry.FailureDetail == "" && err != nil {
		entry.FailureDetail = err.Error()
	}
	_ = governancepack.AppendActivationLog(path, entry)
}

func inferGovernanceFailureState(err error) string {
	switch {
	case err == nil:
		return ""
	case strings.Contains(err.Error(), governancepack.FailureSignatureInvalid):
		return governancepack.FailureSignatureInvalid
	case strings.Contains(err.Error(), governancepack.FailureManifestInvalid):
		return governancepack.FailureManifestInvalid
	case strings.Contains(err.Error(), governancepack.FailurePayloadInvalid):
		return governancepack.FailurePayloadInvalid
	case strings.Contains(err.Error(), governancepack.FailureCompatibilityBlocked):
		return governancepack.FailureCompatibilityBlocked
	default:
		return governancepack.FailureActivationIncomplete
	}
}

func stageGovernancePack(root string, verified governancepack.VerifiedPack) (governancepack.BundlePaths, error) {
	stageDir := filepath.Join(governancepack.GovernanceDirPath(root), "packs", verified.Manifest.PackID, verified.Manifest.PackVersion)
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return governancepack.BundlePaths{}, err
	}
	manifestPath := filepath.Join(stageDir, "governance-pack.manifest.json")
	sigPath := filepath.Join(stageDir, "governance-pack.manifest.sig")
	payloadPath := filepath.Join(stageDir, "governance-pack.nt")
	for _, item := range []struct {
		path string
		data []byte
	}{
		{manifestPath, verified.ManifestBytes},
		{sigPath, []byte(base64Encode(verified.SignatureBytes) + "\n")},
		{payloadPath, verified.PayloadBytes},
	} {
		if err := writeFileAtomic(item.path, item.data); err != nil {
			return governancepack.BundlePaths{}, err
		}
	}
	return governancepack.BundlePaths{
		Dir:           stageDir,
		ManifestPath:  manifestPath,
		SignaturePath: sigPath,
		PayloadPath:   payloadPath,
	}, nil
}

func buildCombinedGovernedArtifact(root string, verified *governancepack.VerifiedPack) ([]byte, seedmeta.Marker, error) {
	projectArtifact, err := buildProjectArtifact(root)
	if err != nil {
		return nil, seedmeta.Marker{}, err
	}
	combined, marker, _, _ := combineGraphArtifacts(verified.PayloadBytes, projectArtifact)
	if errs := extractorValidate(combined); len(errs) > 0 {
		return nil, seedmeta.Marker{}, fmt.Errorf("combined graph has %d N-Triples validation error(s)", len(errs))
	}
	return combined, marker, nil
}
