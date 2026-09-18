// SPDX-License-Identifier: AGPL-3.0-only

package main

// RELOAD SUCCESS MUST NAME THE STORE IT REFRESHED (#377).
//
// Measured on sensei-code#183: a new invariant was authored correctly and
// committed, `propose` reported a successful reload, and the invariant was not
// reachable from the store the project declares. The command had refreshed its
// own built-in default. Nothing lied -- "reload: ok" simply never said where,
// so a reload of the legacy default read exactly like a reload of the declared
// authority.
//
// The repair asks the question `build` has asked since #212, through the same
// owner, and records the answer.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// proposeWithConfiguredStore seeds a repo whose .sensei/config.yaml declares
// one store, and returns the root.
func proposeWithConfiguredStore(t *testing.T, storeURL string) string {
	t.Helper()
	root := initProposeRepo(t)
	if storeURL != "" {
		mustWrite(t, filepath.Join(root, ".sensei", "config.yaml"),
			"store:\n    store_url: "+storeURL+"\nserver:\n    addr: localhost:10122\n")
	}
	return root
}

func proposeInvariant(title string) *ProposeRequest {
	return &ProposeRequest{
		Kind:            "invariant",
		Title:           title,
		SourceFiles:     []string{"golang/server/reload.go"},
		RelatedFailures: []string{"awareness.existing_failure"},
		RequiredTests:   []string{"golang/server/reload_test.go:TestReloadValidates"},
	}
}

// requireEndpointAgreementForTest forms the verdict the command forms, through
// the SAME comparison -- not a second rule that happens to agree today.
func requireEndpointAgreementForTest(configured, resolved string) error {
	return endpointDisagreement("/repo", "-oxigraph-url", "store.store_url", configured, resolved)
}

//  1. The project declares store A and the command would refresh store B:
//     refused, before anything is written, and never reported as success.
func TestProposeRefusesAStoreTheProjectDoesNotName(t *testing.T) {
	root := proposeWithConfiguredStore(t, "http://localhost:7882/store?default")
	calls := stubRebuild(t)

	before := readFileString(t, filepath.Join(root, "docs/awareness/invariants.yaml"))
	res, code := applyProposal(proposeInvariant("Reload must name its store"), proposeOptions{
		targetRepo: root, agRepo: root,
		// The built-in default: the legacy store, belonging to no governed domain.
		oxigraphURL: "http://localhost:7878/store?default",
		storeAgreement: requireEndpointAgreementForTest(
			"http://localhost:7882/store?default", "http://localhost:7878/store?default"),
	})

	if code == 0 {
		t.Fatalf("publishing into a store the project does not name was accepted: %+v", res)
	}
	if res.Reload == "ok" {
		t.Fatal("a reload of an undeclared store was reported as success")
	}
	if res.Reload != "refused" {
		t.Errorf("reload = %q, want refused", res.Reload)
	}
	// Named in the refusal, both of them: an operator cannot act on "mismatch".
	joined := strings.Join(res.ValidationErrors, "\n")
	for _, want := range []string{"7882", "7878", "store.store_url"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the refusal does not name %s:\n%s", want, joined)
		}
	}
	// NOTHING WAS WRITTEN. The refusal lands before the mutation, so a rejected
	// publication cannot leave an authored entry behind claiming to be live.
	if after := readFileString(t, filepath.Join(root, "docs/awareness/invariants.yaml")); after != before {
		t.Fatal("a refused propose still appended to the corpus")
	}
	if len(*calls) != 0 {
		t.Fatalf("a refused propose still ran %d rebuild(s)", len(*calls))
	}
}

//  2. The project declares store A and the command refreshes A: success, and the
//     result says which store it was.
func TestProposeRecordsTheStoreItRefreshed(t *testing.T) {
	const declared = "http://localhost:7882/store?default"
	root := proposeWithConfiguredStore(t, declared)
	calls := stubRebuild(t)

	res, code := applyProposal(proposeInvariant("Reload must name its store"), proposeOptions{
		targetRepo: root, agRepo: root, oxigraphURL: declared,
		storeAgreement: requireEndpointAgreementForTest(declared, declared),
	})
	if code != 0 {
		t.Fatalf("exit = %d, want 0: %v", code, res.ValidationErrors)
	}
	if res.Reload != "ok" {
		t.Fatalf("reload = %q, want ok", res.Reload)
	}
	if res.ReloadStore != declared {
		t.Fatalf("reload_store = %q, want the declared %q", res.ReloadStore, declared)
	}
	// The declared store corroborates the endpoint, so there is nothing to warn
	// about: a notice here would train an operator to ignore notices.
	if res.ReloadDetail != "" {
		t.Errorf("a reload of the declared store carried a notice: %q", res.ReloadDetail)
	}
	if len(*calls) != 1 {
		t.Fatalf("rebuild calls = %d, want 1", len(*calls))
	}
	if got := strings.Join((*calls)[0], " "); !strings.Contains(got, declared) {
		t.Errorf("the rebuild did not target the declared store: %q", got)
	}
}

// 3. No project-configured authority: the claim is explicit, not inferred.
func TestProposeWithNoConfiguredStoreSaysSoRatherThanImplyingAgreement(t *testing.T) {
	root := proposeWithConfiguredStore(t, "") // no .sensei/config.yaml at all
	stubRebuild(t)
	const resolved = "http://localhost:7878/store?default"

	res, code := applyProposal(proposeInvariant("Reload must name its store"), proposeOptions{
		targetRepo: root, agRepo: root, oxigraphURL: resolved,
		// Nothing to disagree with, so nothing is refused -- but the absence of
		// corroboration is stated.
		storeNotice: nonCanonicalStoreURLNotice("", resolved),
	})
	if code != 0 {
		t.Fatalf("exit = %d, want 0: %v", code, res.ValidationErrors)
	}
	if res.ReloadStore != resolved {
		t.Fatalf("reload_store = %q, want %q", res.ReloadStore, resolved)
	}
	if !strings.Contains(res.ReloadDetail, "names no store") {
		t.Fatalf("an unconfigured project got no explicit notice: %q", res.ReloadDetail)
	}
	if !strings.Contains(res.ReloadDetail, resolved) {
		t.Errorf("the notice does not name the endpoint actually refreshed: %q", res.ReloadDetail)
	}
}

// 4. Moving only the default cannot redirect publication without detection.
//
// The default is exactly what changed under sensei-code#183: nobody edited a
// config, and the publication went somewhere else anyway.
func TestChangingOnlyTheDefaultCannotRedirectPublicationSilently(t *testing.T) {
	const declared = "http://localhost:7882/store?default"
	for name, resolved := range map[string]string{
		"the built-in legacy default": "http://localhost:7878/store?default",
		"another live store":          "http://localhost:7999/store?default",
		"the same host, another path": "http://localhost:7882/store?graph=other",
	} {
		t.Run(name, func(t *testing.T) {
			root := proposeWithConfiguredStore(t, declared)
			calls := stubRebuild(t)
			res, code := applyProposal(proposeInvariant("Reload must name its store"), proposeOptions{
				targetRepo: root, agRepo: root, oxigraphURL: resolved,
				storeAgreement: requireEndpointAgreementForTest(declared, resolved),
			})
			if code == 0 || res.Reload == "ok" {
				t.Fatalf("a redirected publication was accepted: reload=%q code=%d", res.Reload, code)
			}
			if len(*calls) != 0 {
				t.Fatalf("a redirected publication still ran %d rebuild(s)", len(*calls))
			}
		})
	}
}

// The paths that publish nothing are not gated: they make no claim to be wrong
// about, and refusing them would make the guard fire where there is no defect.
func TestProposeDoesNotGateThePathsThatTouchNoStore(t *testing.T) {
	const declared = "http://localhost:7882/store?default"
	disagreeing := proposeOptions{
		oxigraphURL:    "http://localhost:7878/store?default",
		storeAgreement: requireEndpointAgreementForTest(declared, "http://localhost:7878/store?default"),
	}
	if disagreeing.storeAgreement == nil {
		t.Fatal("the fixture does not disagree, so this proves nothing")
	}

	t.Run("--no-rebuild", func(t *testing.T) {
		root := proposeWithConfiguredStore(t, declared)
		opt := disagreeing
		opt.targetRepo, opt.agRepo, opt.noRebuild = root, root, true
		stubRebuild(t)
		res, code := applyProposal(proposeInvariant("Reload must name its store"), opt)
		if code != 0 {
			t.Fatalf("--no-rebuild was refused for an endpoint it never touches: %v", res.ValidationErrors)
		}
		if res.Reload != "skipped" {
			t.Errorf("reload = %q, want skipped", res.Reload)
		}
		if res.ReloadStore != "" {
			t.Errorf("a command that touched no store named one: %q", res.ReloadStore)
		}
	})

	t.Run("a candidate that is not yet a live node", func(t *testing.T) {
		root := proposeWithConfiguredStore(t, declared)
		opt := disagreeing
		opt.targetRepo, opt.agRepo = root, root
		stubRebuild(t)
		res, code := applyProposal(&ProposeRequest{
			Kind: "contract_unknown", Title: "What owns the reload endpoint?",
			Description:      "The contract is unknown and is queued for a human.",
			ProposedContract: "propose must prove which store it refreshed",
			Evidence:         []string{"reload reported ok while the declared store was never refreshed"},
		}, opt)
		if code != 0 {
			t.Fatalf("a candidate was refused for an endpoint it never touches: %v", res.ValidationErrors)
		}
		if res.Reload != "skipped" {
			t.Errorf("reload = %q, want skipped", res.Reload)
		}
	})
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
