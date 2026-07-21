// SPDX-License-Identifier: AGPL-3.0-only

package resultpipeline

import (
	"context"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/globulario/sensei/golang/architecture/resulttransition"
)

// TestValidateDeterministicRepeated proves the gate is a pure predicate: the same
// valid result validates identically every time, and a repeated clean build
// produces a byte-identical, still-valid result.
func TestValidateDeterministicRepeated(t *testing.T) {
	repo, taskDir, resultRev := e2eSeedClean(t)
	req := BuildRequest{RepositoryRoot: repo, TaskDirectory: taskDir, ResultMode: resulttransition.ResultModeRevision, ResultRevision: resultRev, RepositoryDomain: e2eDomain}
	first, err := Build(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	want := canonicalProjection(first)
	for i := 0; i < 3; i++ {
		got, err := Build(context.Background(), req)
		if err != nil {
			t.Fatalf("repeat build %d: %v", i, err)
		}
		if canonicalProjection(got) != want {
			t.Fatalf("repeat build %d not identical", i)
		}
		if err := ValidateBuildResult(got); err != nil {
			t.Fatalf("repeat build %d not valid: %v", i, err)
		}
	}
}

// TestValidateParallel proves validation of a shared valid result is race-clean.
func TestValidateParallel(t *testing.T) {
	res := validBuilt(t)
	var wg sync.WaitGroup
	errs := make([]error, 16)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = ValidateBuildResult(res)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("parallel validation %d failed: %v", i, err)
		}
	}
}

// TestValidateDoesNotMutate proves the gate never mutates the result: the
// canonical projection is identical before and after validation.
func TestValidateDoesNotMutate(t *testing.T) {
	res := validBuilt(t)
	before := canonicalProjection(res)
	if err := ValidateBuildResult(res); err != nil {
		t.Fatal(err)
	}
	if canonicalProjection(res) != before {
		t.Fatal("validation mutated the result")
	}
}

// TestBuildCleanRelocated proves a relocated clean checkout still builds and
// gates valid, with an identical canonical projection.
func TestBuildCleanRelocated(t *testing.T) {
	repo, taskDir, resultRev := e2eSeedClean(t)
	first, err := Build(context.Background(), BuildRequest{RepositoryRoot: repo, TaskDirectory: taskDir, ResultMode: resulttransition.ResultModeRevision, ResultRevision: resultRev, RepositoryDomain: e2eDomain})
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	repo2 := filepath.Join(base, "relocated-repo")
	task2 := filepath.Join(base, "relocated-task")
	if out, err := exec.Command("cp", "-a", repo, repo2).CombinedOutput(); err != nil {
		t.Fatalf("cp repo: %v\n%s", err, out)
	}
	if out, err := exec.Command("cp", "-a", taskDir, task2).CombinedOutput(); err != nil {
		t.Fatalf("cp task: %v\n%s", err, out)
	}
	relocated, err := Build(context.Background(), BuildRequest{RepositoryRoot: repo2, TaskDirectory: task2, ResultMode: resulttransition.ResultModeRevision, ResultRevision: resultRev, RepositoryDomain: e2eDomain})
	if err != nil {
		t.Fatalf("relocated clean build: %v", err)
	}
	if canonicalProjection(relocated) != canonicalProjection(first) {
		t.Fatal("relocated clean checkout produced different canonical output")
	}
}
