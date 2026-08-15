// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/globulario/sensei/golang/architecture/knowledgeadmission"
	"github.com/globulario/sensei/golang/governancepack"
)

// runKnowledgeAdmission exposes the deliberate human boundary around freezing
// and signing knowledge admission. No command other than sign needs the private
// key; ordinary sensei, yaml2nt, closure and import verify with public trust.
func runKnowledgeAdmission(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, `Usage: sensei knowledge-admission <subcommand>

Subcommands:
  corpus-digest  Derive the canonical authored-corpus digest for a manifest
  verify-binding Verify a v2 manifest binds every governed record to that corpus
  sign           Verify the binding, then sign exact manifest bytes
`)
		return 2
	}
	switch args[0] {
	case "corpus-digest":
		return runKnowledgeAdmissionCorpusDigest(args[1:])
	case "verify-binding":
		return runKnowledgeAdmissionVerifyBinding(args[1:])
	case "sign":
		return runKnowledgeAdmissionSign(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "sensei knowledge-admission: unknown subcommand %q\n", args[0])
		return 2
	}
}

// corpus-digest is the freezer-side entry point. It intentionally accepts a
// stale or v1 manifest because its job is to tell the human what digest the
// re-frozen v2 records must contain. It establishes no authority and signs
// nothing.
func runKnowledgeAdmissionCorpusDigest(args []string) int {
	fs := flag.NewFlagSet("sensei knowledge-admission corpus-digest", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: sensei knowledge-admission corpus-digest <manifest.yaml>")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	manifestPath := fs.Arg(0)
	manifestBytes, root, err := admissionManifestAndRepoRoot(manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei knowledge-admission corpus-digest: %v\n", err)
		return 1
	}
	digest, err := knowledgeadmission.AdmissionCorpusDigestForManifest(root, manifestBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei knowledge-admission corpus-digest: %v\n", err)
		return 1
	}
	fmt.Println(digest)
	return 0
}

func runKnowledgeAdmissionVerifyBinding(args []string) int {
	fs := flag.NewFlagSet("sensei knowledge-admission verify-binding", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: sensei knowledge-admission verify-binding <manifest.yaml>")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	manifestBytes, root, err := admissionManifestAndRepoRoot(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei knowledge-admission verify-binding: %v\n", err)
		return 1
	}
	digest, err := knowledgeadmission.VerifyManifestCorpusBinding(root, manifestBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei knowledge-admission verify-binding: %v\n", err)
		return 1
	}
	fmt.Printf("corpus binding: VERIFIED %s\n", digest)
	return 0
}

func runKnowledgeAdmissionSign(args []string) int {
	fs := flag.NewFlagSet("sensei knowledge-admission sign", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	signingKey := fs.String("signing-key", "", "path to the governance signing key (MUST be outside the checkout and outside .sensei/)")
	out := fs.String("out", "", "signature output path (default: <manifest>.sig)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei knowledge-admission sign --signing-key <path> <manifest.yaml>

First derives the canonical authored-corpus digest and FAILS CLOSED unless every
governed record in the v2 manifest binds to exactly that digest. Only then does
it sign the EXACT manifest bytes with Ed25519, using the same signing-key loader
and implementation as governance packs.

The signature covers every byte, so the actor binding, admitting role,
dispositions, admitted identities and source-corpus binding are all inside the
authenticated envelope. Publication output is deliberately not part of the
binding: admission controls publication and cannot safely depend on its own effect.

The signing key must live outside the repository and outside .sensei/. Generate
one with:

  sensei governance publish gen-key --out ~/.config/sensei-governance/key.json

Flags:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return 2
	}
	manifestPath := fs.Arg(0)
	if strings.TrimSpace(*signingKey) == "" {
		fmt.Fprintln(os.Stderr, "sensei knowledge-admission sign: --signing-key is required")
		return 2
	}

	// Refuse a key inside the checkout or inside .sensei/. A key stored there is
	// reachable by anything that can write the repository, which dissolves the
	// property the signature exists to establish.
	if err := refuseInRepositoryKey(*signingKey); err != nil {
		fmt.Fprintf(os.Stderr, "sensei knowledge-admission sign: %v\n", err)
		return 1
	}

	manifestBytes, root, err := admissionManifestAndRepoRoot(manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei knowledge-admission sign: %v\n", err)
		return 1
	}
	corpusDigest, err := knowledgeadmission.VerifyManifestCorpusBinding(root, manifestBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei knowledge-admission sign: REFUSED: %v\n", err)
		fmt.Fprintln(os.Stderr, "derive the current value with `sensei knowledge-admission corpus-digest <manifest>` and re-freeze before signing")
		return 1
	}

	key, priv, err := governancepack.LoadSigningKey(*signingKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei knowledge-admission sign: signing key: %v\n", err)
		return 1
	}

	sigPath := strings.TrimSpace(*out)
	if sigPath == "" {
		sigPath = strings.TrimSuffix(manifestPath, filepath.Ext(manifestPath)) + ".sig"
	}
	sig := ed25519.Sign(priv, manifestBytes)
	if err := os.WriteFile(sigPath, []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "sensei knowledge-admission sign: %v\n", err)
		return 1
	}

	fmt.Printf("signed:        %s\n", manifestPath)
	fmt.Printf("signature:     %s\n", sigPath)
	fmt.Printf("corpus digest: %s\n", corpusDigest)
	fmt.Printf("publisher:     %s\n", key.PublisherID)
	fmt.Printf("key id:        %s\n", key.KeyID)
	fmt.Println()
	fmt.Println("The verifier must be configured to expect this publisher for knowledge")
	fmt.Println("admission; a trusted publisher is not automatically an authorized one.")
	return 0
}

func admissionManifestAndRepoRoot(manifestPath string) ([]byte, string, error) {
	abs, err := filepath.Abs(manifestPath)
	if err != nil {
		return nil, "", err
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", err
	}
	root := findRepoRoot(filepath.Dir(abs))
	if root == "" {
		return nil, "", fmt.Errorf("cannot find git checkout containing %s", abs)
	}
	return raw, root, nil
}

// refuseInRepositoryKey fails when the signing key sits inside the git checkout
// or inside .sensei/ runtime state.
func refuseInRepositoryKey(keyPath string) error {
	abs, err := filepath.Abs(keyPath)
	if err != nil {
		return err
	}
	if strings.Contains(filepath.ToSlash(abs), "/.sensei/") {
		return fmt.Errorf("signing key %s is inside .sensei/ runtime state; keep it outside the repository", abs)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	root := findRepoRoot(cwd)
	if root == "" {
		return nil
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return nil
	}
	return fmt.Errorf("signing key %s is inside the checkout %s; repository write access must not reach the key", abs, root)
}

func findRepoRoot(start string) string {
	dir := start
	for {
		if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && (info.IsDir() || info.Mode().IsRegular()) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
