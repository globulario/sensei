// SPDX-License-Identifier: AGPL-3.0-only

package governedmutation

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	yaml "gopkg.in/yaml.v3"

	"github.com/globulario/sensei/golang/architecture/closureprotocol"
	"github.com/globulario/sensei/golang/propose"
)

// planned is the fully resolved, validated, classified mutation — everything
// needed to write, computed without mutating anything.
type planned struct {
	kind           string
	id             string
	relPath        string // repo-relative under the repository root
	topKey         string
	path           string // absolute target file
	isCandidate    bool
	item           interface{}
	itemText       string
	mutationDigest string
	existing       bool
	replay         bool
	existingDigest string
}

// Plan validates, routes, derives the canonical ID, renders the canonical body,
// and classifies the mutation (new / replay / contradiction) WITHOUT writing.
// A contradiction (same ID, different body) is a typed error, never a silent
// overwrite.
func Plan(req Request) (Result, error) {
	pl, err := plan(req)
	if err != nil {
		return Result{}, err
	}
	return pl.result(), nil
}

// Apply performs the governed-source mutation. It classifies via Plan; an exact
// replay writes nothing; a new record is appended atomically (temp-write +
// rename) after an optional compare-and-swap against the expected governed
// manifest. Apply performs NO locking — the caller holds the repository
// governed-mutation lock (see AcquireLock) across this call.
func Apply(req Request) (Result, error) {
	pl, err := plan(req)
	if err != nil {
		return Result{}, err
	}
	res := pl.result()
	if pl.replay {
		// The exact canonical record already exists; nothing to write.
		return res, nil
	}

	pre, err := GovernedManifestDigest(req.RepositoryRoot)
	if err != nil {
		return Result{}, fmt.Errorf("pre-mutation manifest: %w", err)
	}
	// Compare-and-swap: a governed mutation fails closed if the expected manifest
	// no longer matches. Candidate-queue writes do not change the governed
	// manifest and are not gated.
	if !pl.isCandidate && strings.TrimSpace(req.ExpectedManifestDigestSHA256) != "" &&
		req.ExpectedManifestDigestSHA256 != pre {
		return Result{}, &StaleManifestError{Expected: req.ExpectedManifestDigestSHA256, Actual: pre}
	}

	if err := atomicAppend(pl.path, pl.topKey, pl.itemText); err != nil {
		return Result{}, fmt.Errorf("append %s: %w", pl.relPath, err)
	}

	post, err := GovernedManifestDigest(req.RepositoryRoot)
	if err != nil {
		return Result{}, fmt.Errorf("post-mutation manifest: %w", err)
	}
	res.PreManifestDigestSHA256 = pre
	res.PostManifestDigestSHA256 = post
	return res, nil
}

func (pl planned) result() Result {
	disp := DispositionApplied
	if pl.isCandidate {
		disp = DispositionCandidateQueued
	}
	if pl.replay {
		disp = DispositionReplay
	}
	return Result{
		Kind:                 pl.kind,
		CanonicalID:          pl.id,
		TargetRelPath:        filepath.ToSlash(pl.relPath),
		TopKey:               pl.topKey,
		Disposition:          disp,
		IsCandidate:          pl.isCandidate,
		Preview:              pl.itemText,
		MutationDigestSHA256: pl.mutationDigest,
	}
}

func plan(req Request) (planned, error) {
	root := strings.TrimSpace(req.RepositoryRoot)
	if root == "" {
		return planned{}, &ValidationError{Errors: []string{"repository root is required"}}
	}
	p := req.Proposal
	propose.Normalize(&p)
	if errs := propose.Validate(p); len(errs) > 0 {
		return planned{}, &ValidationError{Errors: errs}
	}

	id := DeriveID(p)
	item := buildCanonicalItem(p, id)
	if item == nil {
		return planned{}, &ValidationError{Errors: []string{fmt.Sprintf("no canonical mapping for kind %q", p.Kind)}}
	}
	itemText, err := renderItem(item)
	if err != nil {
		return planned{}, err
	}
	itemMap, err := itemAsMap(item)
	if err != nil {
		return planned{}, err
	}
	mutationDigest, err := closureprotocol.SemanticDigest(itemMap)
	if err != nil {
		return planned{}, err
	}

	var relPath, topKey string
	isCandidate := false
	if p.Kind == "contract_unknown" {
		relPath = filepath.Join(GovernedSourceDir, "candidates", "contract_unknown_"+slugify(id)+".yaml")
		topKey = "contract_unknown"
		isCandidate = true
	} else {
		route, ok := governedKinds[p.Kind]
		if !ok {
			return planned{}, &ValidationError{Errors: []string{fmt.Sprintf("no canonical file mapping for kind %q", p.Kind)}}
		}
		relPath = filepath.Join(GovernedSourceDir, route.file)
		topKey = route.key
	}
	path := filepath.Join(root, filepath.FromSlash(relPath))

	pl := planned{
		kind: p.Kind, id: id, relPath: relPath, topKey: topKey, path: path,
		isCandidate: isCandidate, item: item, itemText: itemText, mutationDigest: mutationDigest,
	}

	existingDigest, found, err := existingEntryDigest(path, topKey, id)
	if err != nil {
		return planned{}, err
	}
	if found {
		pl.existing = true
		pl.existingDigest = existingDigest
		if existingDigest == mutationDigest {
			pl.replay = true
		} else {
			return planned{}, &ContradictionError{
				CanonicalID: id, TargetRelPath: filepath.ToSlash(relPath),
				ExistingDigest: existingDigest, ProposedDigest: mutationDigest,
			}
		}
	}
	return pl, nil
}

// existingEntryDigest returns the semantic body digest of the entry with the
// given id under topKey, if present. A missing file has no entries.
func existingEntryDigest(path, topKey, id string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", false, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	list, ok := doc[topKey].([]any)
	if !ok {
		return "", false, nil
	}
	for _, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if eid, _ := m["id"].(string); eid == id {
			digest, derr := closureprotocol.SemanticDigest(m)
			if derr != nil {
				return "", false, derr
			}
			return digest, true, nil
		}
	}
	return "", false, nil
}

var topKeyLine = func(key string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:`)
}

// atomicAppend appends one rendered list item to path under topKey via a
// temp-write + rename, so a concurrent reader never sees a half-written file.
// The canonical governed files carry a single top-level list running to EOF, so
// appending at end-of-file preserves existing entries and their comments.
func atomicAppend(path, topKey, itemText string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var text string
	data, rerr := os.ReadFile(path)
	switch {
	case rerr == nil:
		text = string(data)
		if !topKeyLine(topKey).MatchString(text) {
			if len(text) > 0 && !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			text += topKey + ":\n" + itemText
		} else {
			if len(text) > 0 && !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			text += itemText
		}
	case os.IsNotExist(rerr):
		text = topKey + ":\n" + itemText
	default:
		return rerr
	}
	return writeFileAtomic(path, []byte(text))
}

// writeFileAtomic writes data to a sibling temp file then renames it over path.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
