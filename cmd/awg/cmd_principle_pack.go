// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/architecture/packcustody"
)

// The principle pack is authored in docs/awareness/generic/, generated into
// cmd/awg/templates/awareness/meta_principles.yaml by
// scripts/sync-principle-pack.py, and installed into a project by
// `sensei init`. That last hop only ever ran ONCE per project: init copies the
// template only when the destination does not exist, and the sync script writes
// to no project (its --services flag is a retained no-op). So a principle
// authored upstream had no sanctioned route to an ALREADY INSTALLED mirror — a
// correct derivation with no path to its consumer.
//
// `sensei principle-pack refresh` is that missing edge, and it is deliberately
// paranoid about the one thing it cannot see: what the project started from.
//
// WITHOUT A BASELINE, "present upstream and absent locally" IS AMBIGUOUS. It
// may be an upstream addition the project has not received, or an entry the
// project deliberately deleted. Those two states are observationally
// identical, so a baseline-free run reports cause UNKNOWN and refuses to
// apply. A baseline is one of:
//
//   - an install record written by `sensei init` whose pack digest still
//     matches the mirror (the project has not touched it), or
//   - a prior adoption record whose resulting digest still matches the
//     mirror (we put it there and nobody has edited it since), or
//   - an explicit one-time --reconcile-legacy authorization from an operator
//     who has reviewed the divergence themselves.

// The record shapes, provenance readers, and path helpers live in
// golang/architecture/packcustody. They moved there because `sensei build` now
// has to answer the same question this command does — "is this mirror a proven
// projection of a pack, and of which one?" — in order to decide publication
// custody. Two copies of that reasoning would be two things to keep in
// agreement about what counts as proof, and the build's answer disagreeing with
// refresh's answer is precisely how a mirror ends up published by a repository
// that this command would have refused to touch.
const (
	principlePackReceiptSchemaVersion = packcustody.ReceiptSchemaVersion
	principlePackReceiptKind          = packcustody.AdoptionKind
	principlePackInstallKind          = packcustody.InstallKind

	// The generated mirror must announce itself. A file lacking this marker is
	// not a managed mirror and is never written.
	principlePackGeneratedMarker = packcustody.GeneratedMarker

	packTemplatePath = "templates/awareness/meta_principles.yaml"
	mirrorRelPath    = packcustody.MirrorRelPath
)

// The principle-pack state paths (and the reasoning about resolving them
// through statedir rather than a ".sensei/..." literal) live in packcustody.
func principlePackDirPath(root string) string { return packcustody.DirPath(root) }

func adoptionsDirPath(root string) string { return packcustody.AdoptionsDirPath(root) }

func installRecordFilePath(root string) string { return packcustody.InstallRecordFilePath(root) }

func refreshLockFilePath(root string) string { return packcustody.RefreshLockFilePath(root) }

// packDiff is the entry-level comparison between the embedded pack and an
// installed mirror, keyed by canonical id.
type packDiff struct {
	// UpstreamOnly is present upstream and absent locally. Whether that is an
	// upstream ADDITION or a local DELETION cannot be told without a baseline,
	// so the name deliberately does not claim either.
	UpstreamOnly []string
	Changed      []string // shared id whose content differs
	LocalOnly    []string // present locally, absent upstream
	Preamble     bool     // leading comment block differs
	SharedSame   int
}

func (d packDiff) clean() bool {
	return len(d.Changed) == 0 && len(d.LocalOnly) == 0 && !d.Preamble
}

func (d packDiff) conflicting() []string {
	out := append([]string{}, d.Changed...)
	out = append(out, d.LocalOnly...)
	sort.Strings(out)
	return out
}

// The adoption record and the install baseline are packcustody's types. These
// are aliases, not conversions: the on-disk YAML is unchanged, and `sensei
// build` reads the very same records this command writes.
type principlePackRecord = packcustody.AdoptionRecord

type installRecord = packcustody.InstallRecord

func runPrinciplePack(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: sensei principle-pack <refresh> [flags]")
		return 2
	}
	switch args[0] {
	case "refresh":
		return runPrinciplePackRefresh(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "sensei principle-pack: unknown subcommand %q (refresh)\n", args[0])
		return 2
	}
}

func runPrinciplePackRefresh(args []string) int {
	fs := flag.NewFlagSet("sensei principle-pack refresh", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	repo := fs.String("repo", "", "path to the project whose managed principle mirror should be refreshed")
	apply := fs.Bool("apply", false, "write the refreshed mirror and its adoption record (default: plan only)")
	reconcile := fs.Bool("reconcile-legacy", false,
		"authorize a one-time adoption for a mirror with no verified baseline (operator asserts the divergence has been reviewed)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*repo) == "" {
		fmt.Fprintln(os.Stderr, "sensei principle-pack refresh: --repo is required")
		return 2
	}
	root, err := filepath.Abs(*repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei principle-pack refresh: %v\n", err)
		return 2
	}
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		fmt.Fprintf(os.Stderr, "sensei principle-pack refresh: %s is not a directory\n", root)
		return 2
	}

	// Exclusive lock for any run that may write. Concurrent agents and editors
	// are a live condition in this codebase, not a theoretical one.
	if *apply {
		release, err := acquireRefreshLock(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sensei principle-pack refresh: %v\n", err)
			return 1
		}
		defer release()
	}

	packBytes, err := templates.ReadFile(packTemplatePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei principle-pack refresh: embedded pack unreadable: %v\n", err)
		return 1
	}
	packEntries, packKey, packPre, err := parsePrinciplePack(packBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei principle-pack refresh: embedded pack is malformed: %v\n", err)
		return 1
	}
	packDigest := sha256Hex(packBytes)

	mirrorPath := filepath.Join(root, mirrorRelPath)
	mirrorBytes, err := readManagedMirror(root, mirrorPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei principle-pack refresh: %v\n", err)
		return 1
	}
	mirrorEntries, mirrorKey, mirrorPre, err := parsePrinciplePack(mirrorBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei principle-pack refresh: %s is not a recognized generated mirror: %v\n", mirrorRelPath, err)
		return 1
	}
	if mirrorKey != packKey {
		fmt.Fprintf(os.Stderr, "sensei principle-pack refresh: %s has top-level key %q, expected %q\n", mirrorRelPath, mirrorKey, packKey)
		return 1
	}
	if !strings.Contains(mirrorPre, principlePackGeneratedMarker) {
		fmt.Fprintf(os.Stderr, "sensei principle-pack refresh: %s does not declare itself generated (%q); refusing to write a possibly hand-authored file\n",
			mirrorRelPath, principlePackGeneratedMarker)
		return 1
	}
	mirrorDigest := sha256Hex(mirrorBytes)

	diff := diffPrinciplePack(packEntries, mirrorEntries, packPre, mirrorPre)
	baseline, baselineWhy := verifyBaseline(root, mirrorDigest)

	fmt.Printf("principle-pack refresh\n")
	fmt.Printf("  pack:    %d principles, digest %s\n", len(packEntries), short(packDigest))
	fmt.Printf("  mirror:  %s — %d principles, digest %s\n", mirrorRelPath, len(mirrorEntries), short(mirrorDigest))
	fmt.Printf("  baseline: %s\n", baselineWhy)
	fmt.Printf("  shared identical: %d\n", diff.SharedSame)
	if len(diff.UpstreamOnly) > 0 {
		if baseline {
			reportIDs("  upstream additions", diff.UpstreamOnly)
		} else {
			reportIDs("  present upstream, absent locally — cause: UNKNOWN (no baseline; may be an upstream addition OR a local deletion)", diff.UpstreamOnly)
		}
	}
	reportIDs("  locally changed", diff.Changed)
	reportIDs("  present locally, absent upstream", diff.LocalOnly)
	if diff.Preamble {
		fmt.Printf("  preamble: DIFFERS from the pack\n")
	}

	// Recovery: an open intent from an interrupted apply.
	if rec, found := openIntentFor(root, mirrorDigest, packDigest); found {
		fmt.Printf("\nrecovering an interrupted adoption (intent %s)\n", short(rec.Target.ResultingDigest))
		if !*apply {
			fmt.Printf("disposition: intent_open — re-run with --apply to complete it\n")
			return 0
		}
		// The receipt being visible here (openIntentFor found it) is not
		// proof its own directory sync ever succeeded -- the earlier
		// atomicCreateFile could have linked it successfully and then hit
		// a transient sync failure, same as the exact-replay case
		// writePrinciplePackRecord itself already guards. This recovery
		// branch never calls writePrinciplePackRecord again (the receipt
		// already exists and is correct), so it must repeat that resync
		// itself before committing the mirror, or a "successfully resumed"
		// apply could still leave the receipt's own durability unconfirmed.
		if err := syncDir(adoptionsDirPath(root)); err != nil {
			fmt.Fprintf(os.Stderr, "sensei principle-pack refresh: re-syncing recovered receipt directory: %v\n", err)
			return 1
		}
		if err := commitMirror(root, mirrorPath, mirrorBytes, packBytes); err != nil {
			fmt.Fprintf(os.Stderr, "sensei principle-pack refresh: %v\n", err)
			return 1
		}
		fmt.Printf("disposition: applied (resumed) — mirror %s → %s\n", short(mirrorDigest), short(packDigest))
		return 0
	}

	if mirrorDigest == packDigest {
		// Current, as of the read at the top of this function. An --apply
		// replay must not trust that stale digest as still true, nor treat
		// "content was already right" as proof its directory sync ever
		// succeeded:
		//   - an editor could have changed the mirror since that initial
		//     read, and unlike commitMirror this fast path performs no
		//     write of its own to naturally re-validate against -- without
		//     an explicit re-check it would report success against content
		//     that may no longer match the pack at all;
		//   - commitMirror's own rename from an EARLIER run could have
		//     completed while its final syncDir failed transiently, and a
		//     retry landing here would otherwise report success without
		//     ever confirming that durability step.
		// Plan mode (no --apply) stays read-only, as everywhere else in
		// this command.
		if *apply {
			// Test-only seam: nil in production. Lets a test deterministically
			// land an edit exactly between the initial digest computation at
			// the top of this function and this fast path's own revalidation,
			// instead of relying on timing.
			if applyAlreadyCurrentBeforeRevalidateHook != nil {
				applyAlreadyCurrentBeforeRevalidateHook()
			}
			current, err := readManagedMirror(root, mirrorPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "sensei principle-pack refresh: re-reading the mirror before reporting success: %v\n", err)
				return 1
			}
			if sha256Hex(current) != packDigest {
				fmt.Fprintf(os.Stderr, "sensei principle-pack refresh: the mirror changed while this run was deciding (now %s, expected %s); refusing to report stale success\n",
					short(sha256Hex(current)), short(packDigest))
				return 1
			}
			if err := syncDir(filepath.Dir(mirrorPath)); err != nil {
				fmt.Fprintf(os.Stderr, "sensei principle-pack refresh: re-syncing mirror directory: %v\n", err)
				return 1
			}
		}
		// If an adoption put it here the record exists and derives to
		// "applied"; if not, say so rather than implying evidence we lack.
		if rec, ok := recordForResult(root, packDigest); ok {
			fmt.Printf("\ndisposition: %s — mirror already matches the pack; record %s\n",
				rec.DerivedDisposition(mirrorDigest), short(rec.Target.ResultingDigest))
			return 0
		}

		// Matching bytes with no record is a DEAD END, not a success: custody
		// derives from a record, never from content, so this mirror stays
		// Refused no matter how exactly it matches -- and every other path in
		// this command declines to act precisely because there is nothing to
		// change. An operator who reconciled by hand lands here permanently.
		//
		// --apply --reconcile-legacy is the exit. It writes nothing to the
		// mirror (there is nothing to write) and only records what an operator
		// attests: this content is the pack's, adopted here. The record is
		// honest that no id moved -- previous and resulting digests are equal
		// and every id list is empty -- so it proves provenance without
		// claiming a change it did not make.
		if *apply && *reconcile {
			rec := buildPrinciplePackRecord(packDigest, len(packEntries), mirrorRelPath, packDigest, packDigest, packDiff{SharedSame: len(packEntries)}, "reconcile_legacy")
			recPath, err := writePrinciplePackRecord(root, rec)
			if err != nil {
				fmt.Fprintf(os.Stderr, "sensei principle-pack refresh: recording adoption of already-current content: %v\n", err)
				return 1
			}
			fmt.Printf("\ndisposition: adopted_in_place — mirror already matched the pack; provenance recorded\n")
			fmt.Printf("  no id changed; the mirror was not modified\n")
			fmt.Printf("  record:  %s (authorization: reconcile_legacy)\n", mustRel(root, recPath))
			return 0
		}
		fmt.Printf("\ndisposition: already_current — mirror matches the pack, with no adoption record on file\n")
		fmt.Printf("  Custody derives from a record, not from matching bytes, so this mirror is\n")
		fmt.Printf("  still unproven. Re-run with --reconcile-legacy --apply to record that this\n")
		fmt.Printf("  content is the pack's; nothing in the mirror will be modified.\n")
		return 0
	}

	// An entry present ONLY in the mirror is project-authored content living in
	// a file that declares itself generated. Adoption cannot resolve it: the
	// pack has nothing to restore it from, so applying would DELETE knowledge
	// only this project holds, permanently and with no upstream copy. No
	// operator authorization makes that safe, which is why this refusal is
	// unconditional -- --reconcile-legacy attests that local text carried no
	// intent worth keeping, and an id upstream has never seen is the one case
	// where that assertion cannot be true. Relocate it to a project-owned
	// identity in the project's own corpus first; then it is no longer part of
	// the managed projection and this refusal stops applying.
	if len(diff.LocalOnly) > 0 {
		fmt.Fprintf(os.Stderr, "\ndisposition: conflict — refusing to refresh.\n")
		fmt.Fprintf(os.Stderr, "  %d id(s) exist only in this mirror, and the pack cannot restore them:\n", len(diff.LocalOnly))
		fmt.Fprintf(os.Stderr, "    %s\n", strings.Join(diff.LocalOnly, ", "))
		fmt.Fprintf(os.Stderr, "  Refreshing would delete them. Move them into this project's own corpus\n")
		fmt.Fprintf(os.Stderr, "  under a project-owned id first; they do not belong in a managed mirror.\n")
		return 1
	}

	// A locally changed shared entry, or a rewritten preamble, is divergence on
	// an identity the PACK owns. Canonical wins -- but only an operator can
	// attest that the local text carried no intent worth preserving, and that
	// attestation is precisely what --reconcile-legacy is (see this file's
	// header, which lists it as one of the three valid baselines).
	//
	// This gate consults that flag because otherwise the flag is unreachable for
	// the exact population it exists to serve: a legacy mirror predating install
	// records has no baseline AND has usually drifted, so it fails here and
	// never reaches the baseline check below. Custody then refuses the file
	// forever while pointing the operator at a flag that could not have helped.
	diverged := len(diff.Changed) > 0 || diff.Preamble
	if diverged && !*reconcile {
		fmt.Fprintf(os.Stderr, "\ndisposition: conflict — refusing to refresh.\n")
		fmt.Fprintf(os.Stderr, "  This mirror is not a clean copy of a previously installed pack, so an\n")
		fmt.Fprintf(os.Stderr, "  automatic refresh could not tell an upstream change from a local edit.\n")
		if len(diff.Changed) > 0 {
			fmt.Fprintf(os.Stderr, "  conflicting ids (%d): %s\n", len(diff.Changed), strings.Join(diff.Changed, ", "))
		}
		if diff.Preamble {
			fmt.Fprintf(os.Stderr, "  the generated preamble also differs from the pack's\n")
		}
		fmt.Fprintf(os.Stderr, "  Review the divergence above. If none of it is intent worth keeping,\n")
		fmt.Fprintf(os.Stderr, "  re-run with --reconcile-legacy --apply to restore the canonical text;\n")
		fmt.Fprintf(os.Stderr, "  the adoption record will list exactly what was overwritten.\n")
		return 1
	}
	// Every entry agrees and so does the preamble, yet the bytes differ: the
	// pack was reformatted, not rewritten. A baseline cannot authorize this on
	// its own -- it proves what the project started from, not that an operator
	// accepted a whole-file overwrite yielding no entry change -- so it stays
	// refused unless authorized.
	formattingOnly := len(diff.UpstreamOnly) == 0 && !diverged
	if formattingOnly && !*reconcile {
		fmt.Fprintf(os.Stderr, "\ndisposition: conflict — entries match but bytes differ (formatting drift); refusing\n")
		return 1
	}

	// The record must name what ACTUALLY permitted the write. formattingOnly is
	// in this condition because that path is reachable only via --reconcile-legacy:
	// crediting it to the baseline would attribute the overwrite to evidence
	// that, on its own, refuses it -- and the receipt is the only place a later
	// reader can learn an operator was involved at all.
	authorization := "verified_baseline"
	if !baseline || diverged || formattingOnly {
		if !*reconcile {
			fmt.Fprintf(os.Stderr, "\ndisposition: needs_reconciliation — refusing to apply without a baseline.\n")
			fmt.Fprintf(os.Stderr, "  %d id(s) are present upstream and absent locally, but with no install or\n", len(diff.UpstreamOnly))
			fmt.Fprintf(os.Stderr, "  adoption record matching this mirror there is no way to tell an upstream\n")
			fmt.Fprintf(os.Stderr, "  addition from a deliberate local deletion. Applying would silently\n")
			fmt.Fprintf(os.Stderr, "  resurrect entries the project may have removed on purpose.\n")
			fmt.Fprintf(os.Stderr, "  Review the list above, then re-run with --reconcile-legacy --apply.\n")
			return 1
		}
		authorization = "reconcile_legacy"
	}

	if !*apply {
		fmt.Printf("\ndisposition: plan — %d id(s) would be adopted: %s\n",
			len(diff.UpstreamOnly), strings.Join(diff.UpstreamOnly, ", "))
		// Overwrites are reported separately from adoptions and never folded
		// into that count: adopting an entry the project never had and
		// discarding one it edited are different acts, and an operator
		// authorizing the second deserves to see it named.
		reportOverwrites(os.Stdout, diff)
		fmt.Printf("authorization: %s\n", authorization)
		fmt.Printf("re-run with --apply to write the mirror and its adoption record\n")
		return 0
	}

	// Write the immutable record FIRST. If the mirror write then fails, the
	// record is an open intent and a re-run resumes it — there is no state in
	// which the mirror moved and no evidence exists.
	rec := buildPrinciplePackRecord(packDigest, len(packEntries), mirrorRelPath, mirrorDigest, packDigest, diff, authorization)
	recPath, err := writePrinciplePackRecord(root, rec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sensei principle-pack refresh: recording adoption intent: %v\n", err)
		fmt.Fprintf(os.Stderr, "  the mirror has NOT been modified\n")
		return 1
	}
	if err := commitMirror(root, mirrorPath, mirrorBytes, packBytes); err != nil {
		fmt.Fprintf(os.Stderr, "sensei principle-pack refresh: %v\n", err)
		fmt.Fprintf(os.Stderr, "  intent %s remains open; re-run with --apply to resume\n", mustRel(root, recPath))
		return 1
	}

	fmt.Printf("\ndisposition: applied\n")
	fmt.Printf("  adopted: %s\n", strings.Join(diff.UpstreamOnly, ", "))
	reportOverwrites(os.Stdout, diff)
	fmt.Printf("  mirror:  %s → %s\n", short(mirrorDigest), short(packDigest))
	fmt.Printf("  record:  %s (authorization: %s)\n", mustRel(root, recPath), authorization)
	fmt.Printf("\nThe project graph is NOT rebuilt by this command — run the project's own\n")
	fmt.Printf("seed/graph build so the new principles reach their consumers.\n")
	return 0
}

// commitMirror re-validates immediately before the rename and refuses if the
// file moved under us. Read → decide → write is a TOCTOU window, and this repo
// demonstrably has concurrent writers.
//
// The replacement is prepared FIRST — write, sync, chmod a temp file — so
// none of that work sits between the re-check and the rename. Only then is
// the mirror re-read and re-digested, immediately before the one os.Rename
// that commits it. An earlier version re-checked before preparing the
// replacement, leaving the entire write+sync+chmod duration as a window in
// which a concurrent editor's save would be silently overwritten by the
// rename; this ordering shrinks that window to one digest read plus one
// rename syscall. No lock can close it further against an arbitrary external
// editor that does not participate in this repo's own refresh.lock.
var commitMirrorAfterPrepareHook func()

// applyAlreadyCurrentBeforeRevalidateHook is the equivalent seam for the
// "mirrorDigest == packDigest" --apply fast path in runPrinciplePackRefresh,
// which performs its own revalidation rather than going through
// commitMirror (there is nothing to rename on that path). nil in
// production.
var applyAlreadyCurrentBeforeRevalidateHook func()

func commitMirror(root, mirrorPath string, expected, next []byte) error {
	dir := filepath.Dir(mirrorPath)
	tmpName, err := prepareTempFile(dir, filepath.Base(mirrorPath), next, 0o644)
	if err != nil {
		return err
	}
	defer os.Remove(tmpName)

	// Test-only seam: nil in production. Lets a test deterministically land a
	// concurrent edit exactly between temp-file preparation and the
	// pre-rename re-check below, instead of relying on timing.
	if commitMirrorAfterPrepareHook != nil {
		commitMirrorAfterPrepareHook()
	}

	current, err := readManagedMirror(root, mirrorPath)
	if err != nil {
		return fmt.Errorf("re-reading the mirror immediately before commit: %w", err)
	}
	if sha256Hex(current) != sha256Hex(expected) {
		return fmt.Errorf("the mirror changed while this run was deciding (now %s, expected %s); refusing to overwrite a concurrent edit",
			short(sha256Hex(current)), short(sha256Hex(expected)))
	}
	if err := os.Rename(tmpName, mirrorPath); err != nil {
		return err
	}
	return syncDir(dir)
}

// readManagedMirror refuses symlinks and irregular files rather than following
// them, so a replaced path cannot redirect a read or a later write. This
// covers not just the leaf file but every parent directory component
// between root and the leaf: Lstat-ing only the leaf would let a symlinked
// "docs" or "docs/awareness" pass this check while the actual read and the
// later rename still traverse it, redirecting outside root.
func readManagedMirror(root, path string) ([]byte, error) {
	b, err := readNoFollow(root, path)
	if err != nil {
		return nil, fmt.Errorf("managed mirror %s: %w", mirrorRelPath, err)
	}
	return b, nil
}

// readNoFollow and refuseSymlinkedAncestors are packcustody's, so the build's
// custody derivation reads this project's governance evidence under exactly the
// same no-follow rules this command does.
func readNoFollow(root, path string) ([]byte, error) { return packcustody.ReadNoFollow(root, path) }

func refuseSymlinkedAncestors(root, path string) error {
	return packcustody.RefuseSymlinkedAncestors(root, path)
}

func acquireRefreshLock(root string) (func(), error) {
	// mkdirAllSynced, not plain MkdirAll: this may be the FIRST thing in the
	// whole run to create principle-pack/ (e.g. --reconcile-legacy --apply
	// on a project with no prior baseline or adoption at all). If that
	// creation is not made durable here, a later mkdirAllSynced call for the
	// adoptions/ receipt sees principle-pack/ already exists and has no way
	// to know its own creation was never confirmed -- a crash could then
	// preserve a synced, applied mirror while losing the unsynced
	// principle-pack/ entry (and everything durability-managed under it,
	// including the receipt). The lock file ITSELF is still ephemeral, but
	// the directory it happens to be first to create is not, the moment
	// real evidence gets stored under it later in the same run.
	if err := mkdirAllSynced(principlePackDirPath(root), root, 0o755); err != nil {
		return nil, err
	}
	lock := refreshLockFilePath(root)
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("another refresh holds %s; remove it if no refresh is running", lock)
		}
		return nil, err
	}
	fmt.Fprintf(f, "pid %d\n", os.Getpid())
	f.Close()
	return func() { os.Remove(lock) }, nil
}

// parsePrinciplePack enforces an EXACT document shape. Anything it merely
// ignored could be silently deleted by a whole-document replacement — a
// project's extra top-level key must refuse the refresh, not vanish in it.
//
// This includes a trailing "---" YAML document, not just extra keys within
// one document: yaml.Unmarshal decodes only the FIRST document in a stream,
// so a mirror with a second document would validate against that first
// document alone, have its diff computed from it alone, and then have the
// ENTIRE FILE -- including that unrecognized second document -- silently
// replaced by --apply. A Decoder that must hit EOF after exactly one
// document closes that gap the same way the single-top-level-key check
// closes it within a document.
func parsePrinciplePack(b []byte) (map[string]map[string]any, string, string, error) {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		if err == io.EOF {
			// Unlike yaml.Unmarshal (which leaves doc empty and falls
			// through to the len(doc)==0 check below), Decode on truly
			// empty input reports io.EOF as its own error. Normalize to
			// the same "empty document" message rather than leaking that.
			return nil, "", "", fmt.Errorf("empty document")
		}
		return nil, "", "", err
	}
	var extra any
	switch err := dec.Decode(&extra); err {
	case io.EOF:
		// Exactly one document, as required.
	case nil:
		return nil, "", "", fmt.Errorf("document contains more than one YAML document; " +
			"a whole-document refresh would discard everything after the first")
	default:
		return nil, "", "", fmt.Errorf("checking for a trailing YAML document: %w", err)
	}
	if len(doc) == 0 {
		return nil, "", "", fmt.Errorf("empty document")
	}
	if len(doc) != 1 {
		keys := make([]string, 0, len(doc))
		for k := range doc {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return nil, "", "", fmt.Errorf("expected exactly one top-level key, found %d (%s); "+
			"a whole-document refresh would discard the others", len(keys), strings.Join(keys, ", "))
	}
	var key string
	for k := range doc {
		key = k
	}
	list, ok := doc[key].([]any)
	if !ok {
		return nil, "", "", fmt.Errorf("top-level key %q is not a list of principles", key)
	}
	entries := map[string]map[string]any{}
	for i, raw := range list {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, "", "", fmt.Errorf("entry %d under %q is not a mapping", i, key)
		}
		id, ok := m["id"].(string)
		if !ok || strings.TrimSpace(id) == "" {
			return nil, "", "", fmt.Errorf("entry %d under %q has no string id", i, key)
		}
		if _, dup := entries[id]; dup {
			return nil, "", "", fmt.Errorf("duplicate id %q", id)
		}
		entries[id] = m
	}
	return entries, key, leadingComments(b), nil
}

func leadingComments(b []byte) string {
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, "#") {
			break
		}
		out = append(out, t)
	}
	return strings.Join(out, "\n")
}

func diffPrinciplePack(pack, mirror map[string]map[string]any, packPre, mirrorPre string) packDiff {
	d := packDiff{Preamble: packPre != mirrorPre}
	for id, pe := range pack {
		me, ok := mirror[id]
		if !ok {
			d.UpstreamOnly = append(d.UpstreamOnly, id)
			continue
		}
		if reflect.DeepEqual(pe, me) {
			d.SharedSame++
		} else {
			d.Changed = append(d.Changed, id)
		}
	}
	for id := range mirror {
		if _, ok := pack[id]; !ok {
			d.LocalOnly = append(d.LocalOnly, id)
		}
	}
	sort.Strings(d.UpstreamOnly)
	sort.Strings(d.Changed)
	sort.Strings(d.LocalOnly)
	return d
}

// verifyBaseline answers "do we know what this mirror started from?"
func verifyBaseline(root, mirrorDigest string) (bool, string) {
	// Delegated so that "what counts as a verified baseline" has ONE definition,
	// shared with the custody derivation `sensei build` runs. Both readers refuse
	// to follow a symlinked installed.yaml or receipt: one pointing at another
	// checkout's real governance state would otherwise let --apply classify
	// genuinely missing ids as upstream additions and replace this checkout's
	// mirror on a foreign baseline, bypassing the no-verified-baseline refusal
	// this whole file exists to enforce.
	if ir, ok := packcustody.LoadInstallRecord(root); ok && ir.PackDigest == mirrorDigest {
		return true, "verified (install record digest matches the mirror)"
	}
	for _, rec := range loadRecords(root) {
		if rec.Target.ResultingDigest == mirrorDigest {
			return true, "verified (prior adoption record " + short(rec.Target.ResultingDigest) + " matches the mirror)"
		}
	}
	return false, "NONE — no install or adoption record matches this mirror"
}

func loadRecords(root string) []principlePackRecord {
	return packcustody.LoadAdoptionRecords(root)
}

func recordForResult(root, digest string) (principlePackRecord, bool) {
	for _, r := range loadRecords(root) {
		if r.Target.ResultingDigest == digest {
			return r, true
		}
	}
	return principlePackRecord{}, false
}

// openIntentFor finds a record whose mirror move has not happened yet.
func openIntentFor(root, mirrorDigest, packDigest string) (principlePackRecord, bool) {
	for _, r := range loadRecords(root) {
		if r.Target.PreviousDigest == mirrorDigest && r.Target.ResultingDigest == packDigest &&
			r.DerivedDisposition(mirrorDigest) == "intent_open" {
			return r, true
		}
	}
	return principlePackRecord{}, false
}

// reportOverwrites names what a reconciled refresh discards. Adoption counts
// say what the project GAINED; without this an operator reading "3 id(s)
// adopted" would have no indication that four others were silently rewritten.
func reportOverwrites(w io.Writer, d packDiff) {
	if len(d.Changed) > 0 {
		fmt.Fprintf(w, "  overwritten with canonical text (%d): %s\n",
			len(d.Changed), strings.Join(d.Changed, ", "))
	}
	if d.Preamble {
		fmt.Fprintf(w, "  the generated preamble is restored to the pack's\n")
	}
}

func buildPrinciplePackRecord(packDigest string, count int, path, prev, next string, d packDiff, authorization string) principlePackRecord {
	var r principlePackRecord
	r.SchemaVersion = principlePackReceiptSchemaVersion
	r.Kind = principlePackReceiptKind
	r.Source.Authority = "github.com/globulario/sensei"
	r.Source.SenseiRevision = senseiRevision()
	r.Source.PackDigest = packDigest
	r.Source.PrincipleCount = count
	r.Target.Path = path
	r.Target.PreviousDigest = prev
	r.Target.ResultingDigest = next
	r.Change.AddedIDs = nonNil(d.UpstreamOnly)
	r.Change.ChangedIDs = nonNil(d.Changed)
	r.Change.RemovedIDs = nonNil(d.LocalOnly)
	r.Change.ConflictingIDs = nonNil(d.conflicting())
	r.Authorization = authorization
	r.DispositionIs = "derived"
	r.DispositionKey = "applied when the mirror digests to resulting_digest_sha256; " +
		"intent_open when it digests to previous_digest_sha256; conflict otherwise"
	return r
}

const principlePackRecordHeader = "# Immutable adoption record written by `sensei principle-pack refresh`.\n" +
	"# Written BEFORE the mirror is replaced, and never rewritten: its terminal\n" +
	"# disposition is derived by digesting the mirror, not stored here. Evidence\n" +
	"# that an upstream-authored pack reached this project — not an authority for\n" +
	"# the principles themselves.\n"

// writePrinciplePackRecord writes the immutable, digest-named record. Replay of
// an identical adoption finds it already present and does not rewrite.
func writePrinciplePackRecord(root string, r principlePackRecord) (string, error) {
	dir := adoptionsDirPath(root)
	if err := mkdirAllSynced(dir, root, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, short(r.Target.ResultingDigest)+".yaml")
	body, err := yaml.Marshal(r)
	if err != nil {
		return "", err
	}
	full := principlePackRecordHeader + string(body)

	// Try to create it with true no-clobber semantics first -- not merely
	// "we checked absence a moment ago, then renamed unconditionally".
	// atomicWriteFile's rename always replaces; if another writer creates
	// this exact digest-named receipt between an absence-check and that
	// rename, it would be silently destroyed despite being documented as
	// immutable evidence, and refresh.lock cannot protect against a writer
	// that does not participate in it. atomicCreateFile's hardlink instead
	// fails atomically with EEXIST if the target already exists by the
	// instant of that one syscall. If we lose that race, fall through to
	// read-and-decide exactly as if the record had been there from the
	// start -- unifying the "already existed" and "someone just won a race"
	// cases into one path, since they are indistinguishable from here.
	createErr := atomicCreateFile(path, []byte(full), 0o644)
	if createErr == nil {
		return path, nil
	}
	if !os.IsExist(createErr) {
		return "", createErr
	}

	switch existing, err := os.ReadFile(path); {
	case err == nil:
		if string(existing) == full {
			// Exact replay: the receipt's own bytes are already right, but
			// that says nothing about whether the directory sync after the
			// write that produced them ever actually succeeded. If a prior
			// run created the receipt and then hit a transient sync error,
			// a naive replay would return success here without ever
			// retrying the sync -- silently forgetting a failed durability
			// step and letting the caller proceed to commitMirror believing
			// the receipt is durable when it was never confirmed.
			if err := syncDir(dir); err != nil {
				return "", fmt.Errorf("re-syncing existing receipt directory: %w", err)
			}
			return path, nil // exact replay (or a race this call just lost)
		}
		return "", fmt.Errorf("record %s already exists with different content; refusing to overwrite", path)
	case os.IsNotExist(err):
		// The link reported EEXIST and then the file vanished before we
		// could read it -- another writer racing us on this exact path.
		// Neither "it's our own content" nor "it conflicts" can be proven
		// from here; refuse rather than guess.
		return "", fmt.Errorf("record %s existed then vanished during creation; refusing to guess at a concurrent writer's intent", path)
	default:
		// Exists but could not be read (permissions, a transient I/O error,
		// anything not "absent"). This record is documented as immutable
		// evidence; only proven absence may create one, and reading back
		// "I don't know what's there" must never be treated as license to
		// overwrite it.
		return "", fmt.Errorf("record %s exists but could not be read; refusing to overwrite: %w", path, err)
	}
}

// writeInstallRecord records the exact pack a project was initialized from, so
// a later refresh can prove the mirror is untouched. Without it, "absent
// locally" stays ambiguous forever.
func writeInstallRecord(root string, packBytes []byte) error {
	entries, _, _, err := parsePrinciplePack(packBytes)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var ir installRecord
	ir.SchemaVersion = principlePackReceiptSchemaVersion
	ir.Kind = principlePackInstallKind
	ir.PackDigest = sha256Hex(packBytes)
	ir.PrincipleIDs = ids
	body, err := yaml.Marshal(ir)
	if err != nil {
		return err
	}
	path := installRecordFilePath(root)
	if err := mkdirAllSynced(filepath.Dir(path), root, 0o755); err != nil {
		return err
	}
	header := "# Baseline written by `sensei init`: the exact principle pack this project\n" +
		"# was initialized from. `sensei principle-pack refresh` uses it to prove the\n" +
		"# mirror is an untouched projection before adopting upstream changes.\n"
	return atomicWriteFile(path, []byte(header+string(body)), 0o644)
}

// prepareTempFile creates, chmods, writes, and syncs a temp file in dir,
// returning its name for the caller to commit (rename or link) and clean up
// on any subsequent failure. Mode is set BEFORE write+sync, not after: a
// chmod with no sync of its own is only as durable as the next thing that
// happens to sync it, so putting it first means the one content sync below
// covers both -- see commitMirror's identical reasoning.
func prepareTempFile(dir, base string, data []byte, mode os.FileMode) (string, error) {
	tmp, err := os.CreateTemp(dir, "."+base+".tmp*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if err := os.Chmod(tmpName, mode); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", err
	}
	return tmpName, nil
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmpName, err := prepareTempFile(dir, filepath.Base(path), data, mode)
	if err != nil {
		return err
	}
	defer os.Remove(tmpName)
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return syncDir(dir)
}

// atomicCreateFile writes data to a new file at path using a temp-file +
// hardlink dance, succeeding only if path does not already exist at the
// instant of that link. Unlike atomicWriteFile's rename (an unconditional
// replace), os.Link fails atomically with EEXIST if the target is already
// there -- true create-only semantics, not merely "we checked absence a
// moment ago, then wrote unconditionally". Returns an error satisfying
// os.IsExist(err) when the target already existed; the caller decides what
// that means (replay, conflict, or a race with another writer).
func atomicCreateFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmpName, err := prepareTempFile(dir, filepath.Base(path), data, mode)
	if err != nil {
		return err
	}
	defer os.Remove(tmpName)
	if err := os.Link(tmpName, path); err != nil {
		return err
	}
	return syncDir(dir)
}

// syncDir fsyncs a directory's own entry so a preceding rename INTO it is
// durable across a crash, not merely visible in the page cache. rename(2) is
// atomic (a reader never observes a torn result) but not durable by itself:
// without this, a crash right after a successful rename can still lose that
// directory entry, reverting to whatever the name pointed at before.
//
// A package-level var, not a plain func: tests override it to prove a
// caller retries or surfaces a failed sync rather than silently treating
// already-correct file bytes as proof the directory itself was made
// durable. Never overridden in production. The real implementation is
// platform-specific (dirsync_unix.go / dirsync_windows.go) -- an
// os.Open+File.Sync directory handle is fsync-able on Unix but not on
// Windows (FlushFileBuffers cannot flush a read-only directory handle), and
// NTFS's own metadata journaling does not need or support the same
// explicit directory-entry flush ext4-family filesystems require.
var syncDir = syncDirImpl

// mkdirAllSynced is os.MkdirAll plus durability, unconditionally: every
// directory level from path up to and including boundary is synced after
// MkdirAll, whether or not THIS call is what created it. A directory
// already being visible on disk is not proof its creation was ever made
// durable -- a previous attempt may have created it and then failed the
// sync itself (a transient error, a crash), and there is no on-disk marker
// distinguishing "exists and durable" from "exists and never confirmed".
// Trying to be clever about syncing only newly-created levels was exactly
// that bug: a retry saw the level already existed and silently skipped
// resyncing it. Unconditional, idempotent resyncing is the only fully
// correct answer, and cheap enough at this call frequency that the
// redundant work is not worth optimizing away. boundary must be an
// ancestor of path that is proven to already exist independent of this
// call (the project root itself, since it was already Stat'd as a
// directory before any of this ran) -- NOT the state directory
// (statedir.Path), which this same call may be the one creating on a
// fresh repo with neither ".sensei" nor ".awg" yet. Stopping the sync at
// the state directory rather than root was exactly this bug one level
// up: it left the state directory's OWN creation -- the entry for
// ".sensei" inside root -- unsynced whenever this call was what created
// it.
//
// Also refuses symlinks the same way readManagedMirror does for the
// mirror: statedir.Path resolves ".sensei"/".awg" via os.Stat, which
// FOLLOWS a symlink, and plain os.MkdirAll on an existing path that Stats
// as a directory (even through a symlink) does nothing further and
// succeeds silently. A project whose ".sensei" (or ".awg") is a symlink
// to somewhere outside root would otherwise have --apply create
// principle-pack/, its lock, and its adoption evidence outside the
// repository named by --repo, before the mirror is ever even read.
func mkdirAllSynced(path, boundary string, perm os.FileMode) error {
	if err := refuseSymlinkedAncestors(boundary, path); err != nil {
		return err
	}
	if fi, err := os.Lstat(path); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink; refusing to create or write through it", path)
		}
		if !fi.IsDir() {
			return fmt.Errorf("%s exists and is not a directory", path)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(path, perm); err != nil {
		return err
	}
	boundary = filepath.Clean(boundary)
	for p := filepath.Clean(path); p != boundary; {
		parent := filepath.Dir(p)
		if err := syncDir(parent); err != nil {
			return fmt.Errorf("syncing %s: %w", parent, err)
		}
		if parent == p {
			return fmt.Errorf("mkdirAllSynced: %s is not under boundary %s", path, boundary)
		}
		p = parent
	}
	return nil
}

func senseiRevision() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				return s.Value
			}
		}
	}
	return Version
}

func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func mustRel(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}

func reportIDs(label string, ids []string) {
	if len(ids) == 0 {
		return
	}
	fmt.Printf("%s (%d): %s\n", label, len(ids), strings.Join(ids, ", "))
}
