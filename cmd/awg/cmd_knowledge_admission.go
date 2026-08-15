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

	"github.com/globulario/sensei/golang/governancepack"
)

// runKnowledgeAdmission is the deliberate, human-invoked signing step for a
// knowledge-admission manifest.
//
// Narrowly scoped on purpose. `governance publish sign` is pack-specific — it
// expects a pack directory and reports pack metadata — so this reuses its
// SIGNING PRIMITIVE (the same LoadSigningKey loader and Ed25519 implementation)
// rather than the command. There must be one signing implementation; a second
// copy is a second thing to get wrong.
//
// No other command needs the private key. Ordinary sensei, yaml2nt, closure and
// import verify with the public trust store alone.
func runKnowledgeAdmission(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, `Usage: sensei knowledge-admission <subcommand>

Subcommands:
  sign  Sign a knowledge-admission manifest with an out-of-repository key
`)
		return 2
	}
	switch args[0] {
	case "sign":
		return runKnowledgeAdmissionSign(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "sensei knowledge-admission: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runKnowledgeAdmissionSign(args []string) int {
	fs := flag.NewFlagSet("sensei knowledge-admission sign", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	signingKey := fs.String("signing-key", "", "path to the governance signing key (MUST be outside the checkout and outside .sensei/)")
	out := fs.String("out", "", "signature output path (default: <manifest>.sig)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: sensei knowledge-admission sign --signing-key <path> <manifest.yaml>

Signs the EXACT manifest bytes with Ed25519, using the same signing-key loader
and implementation as governance packs.

The signature covers every byte, so the actor binding, admitting role,
dispositions and admitted identities are all inside the authenticated envelope.
Verification parses the manifest from the signed bytes and from nothing else.

The signing key must live outside the repository and outside .sensei/. That is
what makes the claim true: repository write access alone cannot mint governing
authority. Generate one with:

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

	key, priv, err := governancepack.LoadSigningKey(*signingKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei knowledge-admission sign: signing key: %v\n", err)
		return 1
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei knowledge-admission sign: %v\n", err)
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

	fmt.Printf("signed:      %s\n", manifestPath)
	fmt.Printf("signature:   %s\n", sigPath)
	fmt.Printf("publisher:   %s\n", key.PublisherID)
	fmt.Printf("key id:      %s\n", key.KeyID)
	fmt.Println()
	fmt.Println("The verifier must be configured to expect this publisher for knowledge")
	fmt.Println("admission; a trusted publisher is not automatically an authorized one.")
	return 0
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
