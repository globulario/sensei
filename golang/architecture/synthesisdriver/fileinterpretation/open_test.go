// SPDX-License-Identifier: AGPL-3.0-only

package fileinterpretation

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenNoFollow_AllowsRegularFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	if err := os.WriteFile(path, []byte(validInterpretationJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := openNoFollow(path)
	if err != nil {
		t.Fatalf("openNoFollow: %v", err)
	}
	defer f.Close()
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != validInterpretationJSON {
		t.Fatalf("read content did not match the written file")
	}
}

// TestOpenNoFollow_RejectsDirectSymlink covers a symlink already in place
// at call time -- the ordinary case TestNew_RejectsSymlink also covers at
// the Provider level, repeated here at the openNoFollow unit level.
func TestOpenNoFollow_RejectsDirectSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	if err := os.WriteFile(real, []byte(validInterpretationJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported in this environment: %v", err)
	}
	f, err := openNoFollow(link)
	if err == nil {
		f.Close()
		t.Fatal("expected an error opening a symlink")
	}
}

// TestOpenNoFollow_RejectsReplacementDuringRace is the real regression
// test for the TOCTOU race a live review found: a prior implementation
// checked the path with a separate Lstat, then opened it in a later,
// independent OpenFile call, leaving a window where another process could
// swap the checked regular file for a symlink in between. openNoFollow
// closes that window by refusing to follow a symlink in the SAME call that
// creates the file descriptor, so this drives many concurrent
// open-vs-replace iterations and asserts the invariant that must hold on
// every single one: openNoFollow never returns a file descriptor backed by
// a symlink, regardless of how the replacement is timed. (It may
// legitimately observe either the original regular file or a clean
// rejection -- both are correct; only a successful open of the symlinked
// replacement would indicate the race survived.)
func TestOpenNoFollow_RejectsReplacementDuringRace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interpretation.json")
	other := filepath.Join(dir, "other.json")
	if err := os.WriteFile(other, []byte(`{"objective":"attacker-controlled"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	const iterations = 200
	symlinkSupported := true
	for i := 0; i < iterations; i++ {
		if err := os.WriteFile(path, []byte(validInterpretationJSON), 0o644); err != nil {
			t.Fatal(err)
		}

		swapDone := make(chan struct{})
		go func() {
			defer close(swapDone)
			_ = os.Remove(path)
			if err := os.Symlink(other, path); err != nil {
				symlinkSupported = false
			}
		}()

		f, err := openNoFollow(path)
		<-swapDone
		if !symlinkSupported {
			if f != nil {
				f.Close()
			}
			t.Skipf("symlinks unsupported in this environment: iteration %d", i)
		}
		if err == nil {
			// openNoFollow won this iteration's race and opened the file
			// before the swap landed (or the swap missed this iteration
			// entirely) -- legitimate, but the content it read must be the
			// real interpretation, never the attacker-controlled target.
			got, readErr := io.ReadAll(f)
			f.Close()
			if readErr != nil {
				t.Fatalf("iteration %d: read: %v", i, readErr)
			}
			if string(got) != validInterpretationJSON {
				t.Fatalf("iteration %d: openNoFollow returned content from the swapped-in replacement, not the original file: %s", i, got)
			}
		}
		// err != nil (the symlink was already in place when openNoFollow's
		// underlying syscall ran) is also a correct outcome -- refusing is
		// exactly what a no-follow open must do.

		_ = os.Remove(path)
	}
}
