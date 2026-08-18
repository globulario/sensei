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
	// Candidate-queue kinds. These are reviewable proposals, NOT canonical
	// records: they route to candidates/, never to a governed source file, and
	// are not graph nodes until a human promotes them.
	//
	// applied_repair is here deliberately rather than beside forbidden_fix. Its
	// counterpart is a rule the project ENFORCES; an applied repair is a claim
	// that something worked, and "it worked" is exactly the kind of assertion
	// that should be reviewed before it becomes advice the next agent follows.
	// Promoting it into canonical knowledge without review would let one
	// session's fix become a law nobody agreed to.
	if p.Kind == "contract_unknown" || p.Kind == "applied_repair" {
		relPath = filepath.Join(GovernedSourceDir, "candidates", p.Kind+"_"+slugify(id)+".yaml")
		topKey = p.Kind
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

	// Match the indentation the file's existing sequence already uses.
	//
	// renderItem always emits 2-space-indented items, and both of YAML's block
	// sequence styles are legal:
	//
	//     invariants:            invariants:
	//       - id: a              - id: a
	//         title: ...           title: ...
	//
	// A corpus written in the second style got a 2-space item appended after a
	// 2-space mapping key, which YAML reads as a key inside the previous entry
	// and rejects -- "did not find expected key". The append path assumed one
	// style because every corpus it had been tried against used that one.
	//
	// Computed HERE rather than inside atomicAppend so the dry-run preview is
	// the exact text that will be written. A preview that shows a different
	// indentation than the write would be its own small lie, in a command
	// whose entire purpose is that the diff is reviewable before it lands.
	itemText = reindentItem(itemText, appendItemIndent(path, topKey))

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

// emptyFlowSeqLine matches a top-level key whose value is an EMPTY flow
// sequence -- `key: []`, the marker `sensei init` scaffolds. A trailing comment
// is tolerated; anything else after the brackets is not this shape.
//
// The capture group deliberately includes the whitespace BEFORE the comment. A
// comment needs preceding whitespace to be a comment: re-emitting `key:` and
// `#...` with nothing between them yields `key:#...`, which YAML reads as a
// mapping value and refuses. verifyAppendResult caught exactly that while this
// was being written.
var emptyFlowSeqLine = func(key string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:[ \t]*\[[ \t]*\]([ \t]*#[^\n]*)?[ \t]*$`)
}

// nonEmptyFlowSeqLine matches a top-level key holding a NON-empty flow sequence,
// e.g. `key: [a, b]`. Block items cannot be appended to that textually, and
// rewriting it is not a safe mechanical transform, so it is refused.
var nonEmptyFlowSeqLine = func(key string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `:[ \t]*\[[ \t]*[^\]\s]`)
}

// atomicAppend appends one rendered list item to path under topKey via a
// temp-write + rename, so a concurrent reader never sees a half-written file.
// The canonical governed files carry a single top-level list running to EOF, so
// appending at end-of-file preserves existing entries and their comments.
//
// Two things here are load-bearing.
//
// FIRST, the scaffolded empty-list marker. `sensei init` writes `key: []`, and
// topKeyLine's `^key:` matches that just as happily as a block marker -- so the
// original code took the "key exists" branch and appended block items under an
// inline empty sequence, producing `decisions: []` followed by `  - id: ...`.
// That is not valid YAML. The marker must be converted to a block marker first.
//
// SECOND, and more important: this function now REFUSES TO WRITE A FILE THAT
// DOES NOT PARSE. The reason the marker bug survived so long is not that the
// transform was subtle; it is that nothing re-read the result, so a corrupting
// write reported success. Every decision recorded after the first was silently
// absent from the graph, and a corpus that is never rebuilt never reveals that
// it stopped parsing. Verifying the composed text before the rename closes that
// whole class -- not just the one shape known to have caused it.
func atomicAppend(path, topKey, itemText string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var text string
	data, rerr := os.ReadFile(path)
	switch {
	case rerr == nil:
		text = string(data)
		switch {
		case nonEmptyFlowSeqLine(topKey).MatchString(text):
			return fmt.Errorf("%s: %q holds an inline (flow) sequence; refusing to append a block item to it — convert it to a block list first", path, topKey)
		case emptyFlowSeqLine(topKey).MatchString(text):
			// Convert the scaffolded `key: []` marker into a block marker,
			// preserving any trailing comment on that line.
			text = emptyFlowSeqLine(topKey).ReplaceAllString(text, topKey+":${1}")
			if !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			text += itemText
		case !topKeyLine(topKey).MatchString(text):
			if len(text) > 0 && !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			text += topKey + ":\n" + itemText
		default:
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
	if err := verifyAppendResult(path, topKey, text); err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(text))
}

// verifyAppendResult refuses a composed document that does not parse, or whose
// top-level key is not a sequence. It runs BEFORE the rename, so a refusal
// leaves the existing file untouched rather than replacing good content with
// unparseable content.
func verifyAppendResult(path, topKey, text string) error {
	var doc map[string]interface{}
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return fmt.Errorf("%s: appending under %q would produce a document that does not parse (%v); refusing to write", path, topKey, err)
	}
	value, ok := doc[topKey]
	if !ok {
		return fmt.Errorf("%s: appending under %q produced a document without that key; refusing to write", path, topKey)
	}
	if _, ok := value.([]interface{}); !ok {
		return fmt.Errorf("%s: appending under %q produced a %T rather than a list; refusing to write", path, topKey, value)
	}
	return nil
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

// RecordBodyDigest returns the semantic body digest of an existing governed
// record identified by (repo-relative source document, top-level key, canonical
// id) — the same value bound as a mutation digest. It lets a consumer re-prove a
// promoted record is present with its exact identity without the original
// proposal. found is false when the file or record is absent.
func RecordBodyDigest(root, relPath, topKey, id string) (digest string, found bool, err error) {
	return existingEntryDigest(filepath.Join(root, filepath.FromSlash(relPath)), topKey, id)
}

// seqItemLine matches a block-sequence item line and captures its indentation.
//
// The indicator may stand alone: YAML allows `-` on its own line with the
// item's content on the following lines. Requiring `- ` skipped such an item
// and let the scan fall through to whatever came next -- which, in a governed
// entry, is a NESTED sequence like protects.files. The record was then
// reindented into that nested list, and verifyAppendResult only checks that
// the top-level key is still a list, so the append could report success while
// the record was not in the governed sequence at all.
var seqItemLine = regexp.MustCompile(`^([ \t]*)-([ \t].*)?$`)

// appendItemIndent reports the indentation of the first block-sequence item
// under topKey in the file at path, defaulting to renderItem's own two spaces
// when the file is absent, the key is absent, or the sequence is still empty.
//
// The FIRST item decides, not a survey: YAML requires every item in one
// sequence to share an indentation, so a file that disagrees with itself is
// already invalid and verifyAppendResult will say so with the parser's own
// words rather than this function guessing which style was intended.
func appendItemIndent(path, topKey string) string {
	const rendered = "  "
	data, err := os.ReadFile(path)
	if err != nil {
		return rendered
	}
	lines := strings.Split(string(data), "\n")
	inKey := false
	for _, ln := range lines {
		if !inKey {
			if topKeyLine(topKey).MatchString(ln) {
				inKey = true
			}
			continue
		}
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// The FIRST meaningful line after the key decides, and nothing after
		// it does. Continuing the scan is what let a nested sequence be
		// mistaken for the outer one: the item this function must match is the
		// sequence's own first item, which by definition is the first thing
		// under the key. If that line is not a sequence item, this key does not
		// hold a block sequence whose style could be matched, and the rendered
		// default stands -- verifyAppendResult then reports the real problem in
		// the parser's own words rather than this function guessing.
		if m := seqItemLine.FindStringSubmatch(ln); m != nil {
			return m[1]
		}
		return rendered
	}
	return rendered
}

// reindentItem shifts a rendered item to the target indentation, preserving the
// relative nesting renderItem produced.
func reindentItem(itemText, indent string) string {
	const rendered = "  "
	if indent == rendered {
		return itemText
	}
	var b strings.Builder
	for _, ln := range strings.Split(strings.TrimRight(itemText, "\n"), "\n") {
		if strings.TrimSpace(ln) == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString(indent + strings.TrimPrefix(ln, rendered) + "\n")
	}
	return b.String()
}
