// SPDX-License-Identifier: AGPL-3.0-only

package runnercomposition

import "testing"

func TestValidateCandidatePathRejectsInvalidPaths(t *testing.T) {
	cases := []string{
		"",
		"/absolute",
		"a/../b",
		"..",
		".",
		"a/./b",
		"a//b",
		"a\x00b",
		"a\nb",
		`a\b`,
		`..\escape`,
		`\absolute`,
	}
	for _, p := range cases {
		if err := ValidateCandidatePath(p); err == nil {
			t.Errorf("ValidateCandidatePath(%q) = nil, want an error", p)
		}
	}
}

// TestValidateCandidatePathRejectsBackslashOutright proves backslash is
// rejected unconditionally, per hard law 9 -- not merely "backslash can
// never become a separator here" reasoning, but an outright ban, since
// Windows filesystem APIs do treat '\' as a path separator and this
// manifest's canonical paths must mean the same thing on every platform
// that might ever read one.
func TestValidateCandidatePathRejectsBackslashOutright(t *testing.T) {
	if err := ValidateCandidatePath(`a\b`); err == nil {
		t.Error(`ValidateCandidatePath("a\b") = nil, want an error`)
	}
}

func TestValidateCandidatePathAcceptsValidPaths(t *testing.T) {
	cases := []string{
		"main.go",
		"a/b/c.txt",
		"README.md",
		"a.b/c..d/e...f",
	}
	for _, p := range cases {
		if err := ValidateCandidatePath(p); err != nil {
			t.Errorf("ValidateCandidatePath(%q) = %v, want nil", p, err)
		}
	}
}

func TestCanonicalizeManifestSortsByPath(t *testing.T) {
	entries := []CandidateManifestEntry{
		{Path: "z.txt", Mode: ModeRegular, Content: []byte("z")},
		{Path: "a.txt", Mode: ModeRegular, Content: []byte("a")},
		{Path: "m.txt", Mode: ModeRegular, Content: []byte("m")},
	}
	for i := range entries {
		entries[i].ContentDigestSHA256 = sha256Hex(entries[i].Content)
	}
	out, err := CanonicalizeManifest(entries)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.txt", "m.txt", "z.txt"}
	for i, w := range want {
		if out[i].Path != w {
			t.Errorf("out[%d].Path = %q, want %q", i, out[i].Path, w)
		}
	}
	// Input slice must not be mutated.
	if entries[0].Path != "z.txt" {
		t.Error("CanonicalizeManifest mutated its input slice's order")
	}
}

func TestCanonicalizeManifestRejectsDuplicatePaths(t *testing.T) {
	entries := []CandidateManifestEntry{
		{Path: "a.txt", Mode: ModeRegular, Content: []byte("1")},
		{Path: "a.txt", Mode: ModeRegular, Content: []byte("2")},
	}
	for i := range entries {
		entries[i].ContentDigestSHA256 = sha256Hex(entries[i].Content)
	}
	if _, err := CanonicalizeManifest(entries); err == nil {
		t.Error("expected duplicate paths to be rejected")
	}
}

func TestCanonicalizeManifestRejectsMismatchedContentDigest(t *testing.T) {
	entries := []CandidateManifestEntry{
		{Path: "a.txt", Mode: ModeRegular, Content: []byte("real"), ContentDigestSHA256: zeroDigest},
	}
	if _, err := CanonicalizeManifest(entries); err == nil {
		t.Error("expected a mismatched declared content_digest_sha256 to be rejected")
	}
}

func TestCanonicalizeManifestRejectsSymlinkWithContent(t *testing.T) {
	entries := []CandidateManifestEntry{
		{Path: "link", Mode: ModeSymlink, Content: []byte("not empty"), SymlinkTarget: "target", ContentDigestSHA256: sha256Hex([]byte("target"))},
	}
	if _, err := CanonicalizeManifest(entries); err == nil {
		t.Error("expected a symlink entry with non-empty content to be rejected")
	}
}

func TestCanonicalizeManifestRejectsRegularWithSymlinkTarget(t *testing.T) {
	entries := []CandidateManifestEntry{
		{Path: "a.txt", Mode: ModeRegular, Content: []byte("x"), SymlinkTarget: "somewhere", ContentDigestSHA256: sha256Hex([]byte("x"))},
	}
	if _, err := CanonicalizeManifest(entries); err == nil {
		t.Error("expected a regular entry with a non-empty symlink_target to be rejected")
	}
}

func TestManifestDigestIsDeterministic(t *testing.T) {
	entries := fixtureManifestEntries(t)
	d1, err := ManifestDigest(entries)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := ManifestDigest(entries)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Errorf("ManifestDigest is not deterministic: %q != %q", d1, d2)
	}
}

// TestManifestDigestIndependentOfInputOrder proves the digest depends only
// on the entry set, not the order entries were supplied in -- canonical
// sorting makes this true.
func TestManifestDigestIndependentOfInputOrder(t *testing.T) {
	entries := fixtureManifestEntries(t)
	reversed := make([]CandidateManifestEntry, len(entries))
	for i, e := range entries {
		reversed[len(entries)-1-i] = e
	}
	d1, err := ManifestDigest(entries)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := ManifestDigest(reversed)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Error("ManifestDigest depends on input order, but manifests must be order-independent")
	}
}

func TestManifestDigestChangesWhenContentChanges(t *testing.T) {
	entries := fixtureManifestEntries(t)
	base, err := ManifestDigest(entries)
	if err != nil {
		t.Fatal(err)
	}

	tampered := append([]CandidateManifestEntry{}, entries...)
	tampered[0].Content = append([]byte{}, tampered[0].Content...)
	tampered[0].Content = append(tampered[0].Content, '!')
	tampered[0].ContentDigestSHA256 = sha256Hex(tampered[0].Content)

	got, err := ManifestDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if got == base {
		t.Error("changing an entry's content did not change ManifestDigest")
	}
}

func TestManifestDigestChangesWhenPathChanges(t *testing.T) {
	entries := fixtureManifestEntries(t)
	base, err := ManifestDigest(entries)
	if err != nil {
		t.Fatal(err)
	}

	tampered := append([]CandidateManifestEntry{}, entries...)
	tampered[0].Path = tampered[0].Path + ".renamed"

	got, err := ManifestDigest(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if got == base {
		t.Error("changing an entry's path did not change ManifestDigest")
	}
}

func TestManifestDigestChangesWhenModeChanges(t *testing.T) {
	entries := []CandidateManifestEntry{
		{Path: "a.sh", Mode: ModeRegular, Content: []byte("#!/bin/sh\n")},
	}
	entries[0].ContentDigestSHA256 = sha256Hex(entries[0].Content)
	base, err := ManifestDigest(entries)
	if err != nil {
		t.Fatal(err)
	}

	executable := []CandidateManifestEntry{
		{Path: "a.sh", Mode: ModeExecutable, Content: []byte("#!/bin/sh\n")},
	}
	executable[0].ContentDigestSHA256 = sha256Hex(executable[0].Content)
	got, err := ManifestDigest(executable)
	if err != nil {
		t.Fatal(err)
	}
	if got == base {
		t.Error("changing an entry's mode (regular -> executable) did not change ManifestDigest")
	}
}

// naiveManifestEncoding reproduces the UNFRAMED encoding ManifestDigest does
// NOT use: mode_byte || path_bytes || content_digest_bytes, concatenated
// directly per entry with no length prefix. It exists only in this test, to
// prove -- permanently, every time this test runs, not as a one-off manual
// check -- that the ambiguity writeLengthPrefixed exists to prevent is real.
func naiveManifestEncoding(t *testing.T, entries []CandidateManifestEntry) []byte {
	t.Helper()
	canonical, err := CanonicalizeManifest(entries)
	if err != nil {
		t.Fatal(err)
	}
	var buf []byte
	for _, e := range canonical {
		buf = append(buf, modeByte(e.Mode))
		buf = append(buf, []byte(e.Path)...)
		buf = append(buf, []byte(e.ContentDigestSHA256)...)
	}
	return buf
}

// TestManifestDigestCollisionSafety proves the length-prefixed encoding
// prevents a real, entry-boundary concatenation collision. It has two
// halves, in order:
//
//  1. Prove the deliberately UNFRAMED encoding (naiveManifestEncoding)
//     actually collides for a genuinely different two-entry vs. one-entry
//     manifest pair -- not asserted, demonstrated: both encode to
//     byte-identical output.
//  2. Prove the real, length-prefixed ManifestDigest keeps that exact same
//     pair distinct.
//
// Proving only the second half (as an earlier version of this test did)
// leaves open whether the "collision" being guarded against was ever real.
func TestManifestDigestCollisionSafety(t *testing.T) {
	contentA := []byte("contentA")
	contentB := []byte("contentB")
	digestA := sha256Hex(contentA)
	digestB := sha256Hex(contentB)

	twoEntries := []CandidateManifestEntry{
		{Path: "a", Mode: ModeRegular, Content: contentA, ContentDigestSHA256: digestA},
		{Path: "b", Mode: ModeExecutable, Content: contentB, ContentDigestSHA256: digestB},
	}

	// mode byte for ModeExecutable is 1 (see modeByte in manifest.go) -- a
	// valid path byte (not NUL, newline, or backslash), so it can be
	// embedded verbatim in a crafted path.
	collidingPath := "a" + digestA + string([]byte{1}) + "b"
	oneEntry := []CandidateManifestEntry{
		{Path: collidingPath, Mode: ModeRegular, Content: contentB, ContentDigestSHA256: digestB},
	}

	// Half 1: the naive (unframed) encoding genuinely collides.
	naiveTwo := naiveManifestEncoding(t, twoEntries)
	naiveOne := naiveManifestEncoding(t, oneEntry)
	if string(naiveTwo) != string(naiveOne) {
		t.Fatalf("test setup invalid: the naive unframed encoding was expected to collide for these two manifests but did not -- naiveTwo=%x naiveOne=%x", naiveTwo, naiveOne)
	}

	// Half 2: the real, length-prefixed ManifestDigest keeps them distinct.
	d1, err := ManifestDigest(twoEntries)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := ManifestDigest(oneEntry)
	if err != nil {
		t.Fatal(err)
	}
	if d1 == d2 {
		t.Error("a two-entry manifest and a crafted one-entry manifest that collide under the naive unframed encoding also produced the same real ManifestDigest -- length-prefixing is not preventing the collision it exists to prevent")
	}
}

func TestManifestDigestRejectsPathTraversal(t *testing.T) {
	entries := []CandidateManifestEntry{
		{Path: "../escape", Mode: ModeRegular, Content: []byte("x"), ContentDigestSHA256: sha256Hex([]byte("x"))},
	}
	if _, err := ManifestDigest(entries); err == nil {
		t.Error("expected a manifest containing a traversal path to be rejected")
	}
}

// TestManifestDigestNeverFollowsSymlinkTarget proves a symlink's target is
// treated as opaque data affecting the digest like any other content, never
// resolved to a path that could be validated or traversed.
func TestManifestDigestNeverFollowsSymlinkTarget(t *testing.T) {
	entries := []CandidateManifestEntry{
		{Path: "link", Mode: ModeSymlink, SymlinkTarget: "../../etc/passwd"},
	}
	entries[0].ContentDigestSHA256 = sha256Hex([]byte(entries[0].SymlinkTarget))
	if _, err := ManifestDigest(entries); err != nil {
		t.Fatalf("a symlink target containing '..' must be accepted as opaque data, got error: %v", err)
	}
}
