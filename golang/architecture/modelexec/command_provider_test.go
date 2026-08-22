// SPDX-License-Identifier: AGPL-3.0-only

package modelexec

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/globulario/sensei/golang/architecture/investigation"
)

// bridgeFixture compiles a real executable that speaks the adapter's wire
// contract. It is a genuine process boundary — the point of these tests is that
// the capability crosses one — while staying hermetic: no network, no
// credentials, no vendor.
func bridgeFixture(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture bridge uses a POSIX shebang-free go build; process semantics are covered on other platforms")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	prog := `package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

var _ = time.Sleep

func main() {
	in, _ := io.ReadAll(os.Stdin)
	var req map[string]interface{}
	_ = json.Unmarshal(in, &req)
	_ = req
` + body + `
}

func emit(v interface{}) {
	data, _ := json.Marshal(v)
	fmt.Println(string(data))
}
`
	if err := os.WriteFile(src, []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "bridge")
	build := exec.Command("go", "build", "-o", bin, src)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fixture bridge: %v\n%s", err, out)
	}
	return bin
}

func commandProvider(path string, argv ...string) *CommandProvider {
	return &CommandProvider{ProviderID: "bridge", ProviderVersion: "v1", Path: path, Argv: argv}
}

const successBody = `
	emit(map[string]interface{}{
		"schema": "sensei.modelexec.command_response.v1",
		"artifact": map[string]interface{}{
			"schema_version":             "sensei.model_artifact.v1",
			"nondeterminism_declaration": "model_response_not_replayable",
			"items": []map[string]interface{}{{
				"kind":               "candidate_claim",
				"text":               "A calls B",
				"cited_evidence_ids": []string{"ev-1"},
			}},
		},
	})`

// A fixture success must earn resolved through Execute's existing validation,
// not because a transport said so.
func TestCommandProviderSuccessEarnsResolvedThroughExecute(t *testing.T) {
	p := commandProvider(bridgeFixture(t, successBody))
	out := Execute(context.Background(), Config{Requested: true, ProviderID: "bridge", ModelName: "m"},
		Registry{"bridge": p}, testRequest())

	if out.Binding.Status != investigation.ModelStatusResolved {
		t.Fatalf("status = %q (reason %q), want %q", out.Binding.Status, out.Binding.Reason, investigation.ModelStatusResolved)
	}
	if out.ProviderCalls != 1 {
		t.Errorf("provider calls = %d, want 1", out.ProviderCalls)
	}
	if out.Binding.ProviderID != "bridge" || out.Binding.ProviderVersion != "v1" {
		t.Errorf("provider identity = %s/%s, want bridge/v1", out.Binding.ProviderID, out.Binding.ProviderVersion)
	}
	if errs := investigation.ValidateModelBinding(out.Binding); len(errs) != 0 {
		t.Errorf("binding from a real adapter fails the contract: %v", errs)
	}
}

// A structured refusal stays a refusal across the process boundary.
func TestCommandProviderRefusalStaysRefused(t *testing.T) {
	p := commandProvider(bridgeFixture(t, `
	emit(map[string]interface{}{
		"schema":  "sensei.modelexec.command_response.v1",
		"refusal": map[string]interface{}{"reason": "policy"},
	})`))
	out := Execute(context.Background(), Config{Requested: true, ProviderID: "bridge", ModelName: "m"},
		Registry{"bridge": p}, testRequest())
	if out.Binding.Status != investigation.ModelStatusRefused {
		t.Fatalf("status = %q, want %q", out.Binding.Status, investigation.ModelStatusRefused)
	}
}

// A non-zero exit is transport failure, and must not be mistaken for a refusal.
func TestCommandProviderProcessFailureIsErrored(t *testing.T) {
	p := commandProvider(bridgeFixture(t, `
	fmt.Fprintln(os.Stderr, "bridge exploded")
	os.Exit(3)`))
	out := Execute(context.Background(), Config{Requested: true, ProviderID: "bridge", ModelName: "m"},
		Registry{"bridge": p}, testRequest())
	if out.Binding.Status != investigation.ModelStatusErrored {
		t.Fatalf("status = %q, want %q", out.Binding.Status, investigation.ModelStatusErrored)
	}
}

func TestCommandProviderMalformedStdoutIsNeverResolved(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"not json", `fmt.Println("this is not json")`},
		{"unknown schema", `emit(map[string]interface{}{"schema": "someone.elses.v1"})`},
		{"neither artifact nor refusal", `emit(map[string]interface{}{"schema": "sensei.modelexec.command_response.v1"})`},
		{"both artifact and refusal", `emit(map[string]interface{}{
			"schema":   "sensei.modelexec.command_response.v1",
			"refusal":  map[string]interface{}{"reason": "no"},
			"artifact": map[string]interface{}{"schema_version": "sensei.model_artifact.v1"},
		})`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := commandProvider(bridgeFixture(t, tc.body))
			out := Execute(context.Background(), Config{Requested: true, ProviderID: "bridge", ModelName: "m"},
				Registry{"bridge": p}, testRequest())
			if out.Binding.Status == investigation.ModelStatusResolved {
				t.Fatal("malformed bridge output produced resolved")
			}
		})
	}
}

// stderr is diagnostic. Two runs differing only in stderr must produce the same
// accepted artifact identity, or stderr would be smuggled into semantic truth.
func TestStderrCannotAlterArtifactIdentity(t *testing.T) {
	quiet := commandProvider(bridgeFixture(t, successBody))
	noisy := commandProvider(bridgeFixture(t, `
	fmt.Fprintln(os.Stderr, "warning: tokens nearly exhausted, retrying, weather is fine")`+successBody))

	a := Execute(context.Background(), Config{Requested: true, ProviderID: "bridge", ModelName: "m"}, Registry{"bridge": quiet}, testRequest())
	b := Execute(context.Background(), Config{Requested: true, ProviderID: "bridge", ModelName: "m"}, Registry{"bridge": noisy}, testRequest())
	if a.Binding.Status != investigation.ModelStatusResolved || b.Binding.Status != investigation.ModelStatusResolved {
		t.Fatalf("expected both resolved, got %q and %q", a.Binding.Status, b.Binding.Status)
	}
	if a.Binding.ArtifactDigestSHA256 != b.Binding.ArtifactDigestSHA256 {
		t.Error("stderr changed the accepted artifact identity")
	}
}

// The adapter must not introduce material the request identity does not cover.
// The envelope is compared field-by-field against the request that produced it.
func TestAdapterSendsOnlyBoundedRequestMaterial(t *testing.T) {
	// The bridge echoes what it received so the wire can be inspected.
	p := commandProvider(bridgeFixture(t, `
	emit(map[string]interface{}{
		"schema": "sensei.modelexec.command_response.v1",
		"artifact": map[string]interface{}{
			"schema_version":             "sensei.model_artifact.v1",
			"nondeterminism_declaration": "echo",
			"items": []map[string]interface{}{{
				"kind": "limitation",
				"text": fmt.Sprintf("%v", req),
			}},
		},
	})`))
	req := testRequest()
	req.Provider = ProviderIdentity{ID: "bridge", Version: "v1"}
	req.Model.Name = "m"
	out := Execute(context.Background(), Config{Requested: true, ProviderID: "bridge", ModelName: "m"}, Registry{"bridge": p}, req)
	if out.Binding.Status != investigation.ModelStatusResolved {
		t.Fatalf("status = %q (%q)", out.Binding.Status, out.Binding.Reason)
	}
	echoed := out.Artifact.Items[0].Text
	// Everything the bridge saw must be traceable to the request.
	for _, forbidden := range []string{"SENSEI_API_KEY", "/etc/passwd", "system_prompt"} {
		if strings.Contains(echoed, forbidden) {
			t.Errorf("the adapter sent material outside the bounded request: %q", forbidden)
		}
	}
	if !strings.Contains(echoed, "ev-1") {
		t.Error("the bridge did not receive the supplied evidence it was supposed to see")
	}
}

// Argv reaches the process verbatim: no shell means no expansion, so a value
// that a shell would rewrite arrives unchanged.
func TestArgvIsPassedWithoutShellInterpretation(t *testing.T) {
	p := commandProvider(bridgeFixture(t, `
	emit(map[string]interface{}{
		"schema": "sensei.modelexec.command_response.v1",
		"artifact": map[string]interface{}{
			"schema_version":             "sensei.model_artifact.v1",
			"nondeterminism_declaration": "echo",
			"items": []map[string]interface{}{{"kind": "limitation", "text": os.Args[1]}},
		},
	})`), "$HOME/*; echo pwned")
	out := Execute(context.Background(), Config{Requested: true, ProviderID: "bridge", ModelName: "m"}, Registry{"bridge": p}, testRequest())
	if out.Binding.Status != investigation.ModelStatusResolved {
		t.Fatalf("status = %q (%q)", out.Binding.Status, out.Binding.Reason)
	}
	if got := out.Artifact.Items[0].Text; got != "$HOME/*; echo pwned" {
		t.Errorf("argv arrived as %q; a shell interpreted it", got)
	}
}

func TestCancellationTerminatesTheInvocation(t *testing.T) {
	p := commandProvider(bridgeFixture(t, `
	time.Sleep(30 * time.Second)`+successBody))
	// The fixture needs the time import; build with it present.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	out := Execute(ctx, Config{Requested: true, ProviderID: "bridge", ModelName: "m"}, Registry{"bridge": p}, testRequest())
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Errorf("cancellation did not terminate the invocation (took %s)", elapsed)
	}
	if out.Binding.Status == investigation.ModelStatusResolved {
		t.Error("a cancelled invocation produced resolved")
	}
}

// Disabled and not-requested must never launch a process at all. A sentinel
// path that cannot exist proves it: reaching exec would fail loudly.
func TestDisabledAndNotRequestedNeverLaunchAProcess(t *testing.T) {
	p := &CommandProvider{ProviderID: "bridge", ProviderVersion: "v1", Path: "/nonexistent/definitely-not-a-real-binary"}
	for _, tc := range []struct {
		name string
		cfg  Config
		want string
	}{
		{"disabled", Config{Disabled: true, Requested: true, ProviderID: "bridge", ModelName: "m"}, investigation.ModelStatusDisabled},
		{"not requested", Config{}, investigation.ModelStatusNotRequested},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := Execute(context.Background(), tc.cfg, Registry{"bridge": p}, testRequest())
			if out.Binding.Status != tc.want {
				t.Fatalf("status = %q, want %q", out.Binding.Status, tc.want)
			}
			if out.ProviderCalls != 0 {
				t.Errorf("provider calls = %d, want 0", out.ProviderCalls)
			}
		})
	}
}

// Provider identity is a semantic assertion, never inferred from a filename.
func TestCommandProviderRequiresExplicitIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    *CommandProvider
	}{
		{"no id", &CommandProvider{ProviderVersion: "v1", Path: "/bin/true"}},
		{"no version", &CommandProvider{ProviderID: "bridge", Path: "/bin/true"}},
		{"no path", &CommandProvider{ProviderID: "bridge", ProviderVersion: "v1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.p.Execute(context.Background(), testRequest())
			if err == nil {
				t.Fatal("the adapter ran without an explicit identity or executable")
			}
			var refusal *Refusal
			if errors.As(err, &refusal) {
				t.Error("a configuration failure was reported as a provider refusal")
			}
		})
	}
}

var _ = json.Marshal

// A bridge is usually a WRAPPER. exec.CommandContext kills only the wrapper, so
// a child inheriting the stdout pipe keeps the real request alive past the
// deadline while Run stays blocked. The earlier cancellation test missed this
// because its fixture was a single process.
func TestCancellationTerminatesDescendantProcesses(t *testing.T) {
	dir := t.TempDir()
	// A wrapper that launches a long-lived child holding the inherited stdout.
	wrapper := filepath.Join(dir, "wrapper.sh")
	if err := os.WriteFile(wrapper, []byte("#!/usr/bin/env bash\nsleep 60 &\nwait\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := commandProvider(wrapper)

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	start := time.Now()
	out := Execute(ctx, Config{Requested: true, ProviderID: "bridge", ModelName: "m"}, Registry{"bridge": p}, testRequest())
	elapsed := time.Since(start)

	if elapsed > 30*time.Second {
		t.Errorf("cancellation did not reach the bridge's children (took %s)", elapsed)
	}
	if out.Binding.Status == investigation.ModelStatusResolved {
		t.Error("a cancelled invocation produced resolved")
	}
}

// The response contract is closed, so it is decoded closed. A misspelled
// authority-shaped field must be REJECTED, not silently dropped by a parser
// with nowhere to put it.
func TestUnknownOrDuplicateResponseFieldsAreRejected(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"unknown field", `emit(map[string]interface{}{
			"schema": "sensei.modelexec.command_response.v1",
			"artifact": map[string]interface{}{
				"schema_version":             "sensei.model_artifact.v1",
				"nondeterminism_declaration": "x",
				"items":                      []map[string]interface{}{{"kind": "question", "text": "q"}},
			},
			"claims_canonicall": true,
		})`},
		{"misspelled authority field on an item", `emit(map[string]interface{}{
			"schema": "sensei.modelexec.command_response.v1",
			"artifact": map[string]interface{}{
				"schema_version":             "sensei.model_artifact.v1",
				"nondeterminism_declaration": "x",
				"items":                      []map[string]interface{}{{"kind": "question", "text": "q", "claims_canonicaI": true}},
			},
		})`},
		{"a second document", successBody + `
	emit(map[string]interface{}{"schema": "sensei.modelexec.command_response.v1"})`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := commandProvider(bridgeFixture(t, tc.body))
			out := Execute(context.Background(), Config{Requested: true, ProviderID: "bridge", ModelName: "m"},
				Registry{"bridge": p}, testRequest())
			if out.Binding.Status == investigation.ModelStatusResolved {
				t.Fatalf("output outside the closed contract produced resolved (%s)", tc.name)
			}
		})
	}
}
