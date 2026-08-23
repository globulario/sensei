// SPDX-License-Identifier: AGPL-3.0-only

// Command prospective-label is the human labelling interface for #259.
//
// It establishes the applicability reference and nothing else. It has no model,
// no graph connection, no retrieval, no ranking and no notion of relevance: the
// corpus is presented in one deterministic order — class, then title, then id —
// and the only thing that reorders it is text the human typed. A tool that
// offered "items likely relevant to this change" would make some machine the
// oracle and let the rest of the corpus quietly disappear, which is the exact
// failure the blind corpus exists to prevent.
//
// It is not part of the ruler. It reads a frozen reference set, never writes to
// one, and can only open the manifest, the blind corpus and the change packages.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/globulario/sensei/golang/architecture/prospectivelabel"
)

func main() {
	set := flag.String("reference-set", "docs/evaluation/prospective-v1-reference-set", "frozen reference set to adjudicate")
	adjudicator := flag.String("adjudicator", "", "who is deciding (required: an answer key nobody is named on cannot be compared to a second one)")
	sessionPath := flag.String("session", "prospective-labels.session.json", "where in-progress work is kept")
	outPath := flag.String("out", "prospective-labels.json", "where the frozen label set is written")
	frozenAt := flag.String("frozen-at", "", "RFC3339 timestamp recorded when labels are frozen (required to export)")
	addr := flag.String("addr", "127.0.0.1:8765", "local address to serve the interface on")
	flag.Parse()

	if strings.TrimSpace(*adjudicator) == "" {
		fatal(fmt.Errorf("--adjudicator is required"))
	}
	rs, err := loadReferenceSet(*set)
	if err != nil {
		fatal(err)
	}
	session, err := prospectivelabel.New(rs.Manifest.DigestSHA256, rs.Corpus.DigestSHA256, *adjudicator, rs.ItemKeys, rs.corpusIDs())
	if err != nil {
		fatal(err)
	}
	srv := &server{rs: rs, session: session, sessionPath: *sessionPath, outPath: *outPath, frozenAt: *frozenAt}
	if err := srv.restore(); err != nil {
		fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", srv.handleIndex)
	mux.HandleFunc("/api/bootstrap", srv.handleBootstrap)
	mux.HandleFunc("/api/present", srv.handlePresent)
	mux.HandleFunc("/api/label", srv.handleLabel)
	mux.HandleFunc("/api/sweep", srv.handleSweep)
	mux.HandleFunc("/api/export", srv.handleExport)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("prospective-label — %d changes x %d eligible items = %d pairs\n",
		len(rs.ItemKeys), len(rs.Corpus.Items), len(rs.ItemKeys)*len(rs.Corpus.Items))
	fmt.Printf("adjudicator: %s\nreference set: %s (manifest %s)\n", *adjudicator, *set, short(rs.Manifest.DigestSHA256))
	fmt.Printf("\nopen http://%s\n", ln.Addr())
	if err := http.Serve(ln, mux); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "prospective-label:", err)
	os.Exit(1)
}

func short(d string) string {
	if len(d) > 12 {
		return d[:12] + "…"
	}
	return d
}

type server struct {
	mu          sync.Mutex
	rs          *referenceSet
	session     *prospectivelabel.Session
	sessionPath string
	outPath     string
	frozenAt    string
}

type persisted struct {
	Adjudicator             string                   `json:"adjudicator"`
	ManifestDigestSHA256    string                   `json:"sample_manifest_digest_sha256"`
	BlindCorpusDigestSHA256 string                   `json:"blind_corpus_digest_sha256"`
	Labels                  []prospectivelabel.Label `json:"labels"`
	Presented               map[string][]string      `json:"presented"`
}

func (s *server) restore() error {
	body, err := os.ReadFile(s.sessionPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var p persisted
	if err := json.Unmarshal(body, &p); err != nil {
		return fmt.Errorf("session file: %w", err)
	}
	// A session belongs to one adjudicator and one sample. Resuming somebody
	// else's work, or work over a different sample, would silently merge two
	// answer keys.
	if p.ManifestDigestSHA256 != s.rs.Manifest.DigestSHA256 || p.BlindCorpusDigestSHA256 != s.rs.Corpus.DigestSHA256 {
		return fmt.Errorf("session file answers a different sample (%s / %s)", short(p.ManifestDigestSHA256), short(p.BlindCorpusDigestSHA256))
	}
	if p.Adjudicator != s.session.Adjudicator {
		return fmt.Errorf("session file belongs to %q, not %q", p.Adjudicator, s.session.Adjudicator)
	}
	return s.session.Restore(p.Labels, p.Presented)
}

func (s *server) save() error {
	presented := map[string][]string{}
	for _, k := range s.rs.ItemKeys {
		if ids := s.session.PresentedIDs(k); len(ids) > 0 {
			presented[k] = ids
		}
	}
	body, err := json.MarshalIndent(persisted{
		Adjudicator:             s.session.Adjudicator,
		ManifestDigestSHA256:    s.session.ManifestDigestSHA256,
		BlindCorpusDigestSHA256: s.session.BlindCorpusDigestSHA256,
		Labels:                  s.session.Labels(),
		Presented:               presented,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.sessionPath, append(body, '\n'), 0o644)
}

func (s *server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, indexHTML)
}

func (s *server) handleBootstrap(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	type corpusItem struct {
		ID        string `json:"id"`
		Class     string `json:"class"`
		Title     string `json:"title"`
		Statement string `json:"statement"`
	}
	byID := map[string]corpusItem{}
	for _, it := range s.rs.Corpus.Items {
		byID[it.ID] = corpusItem{it.ID, it.Class, it.Title, it.Statement}
	}
	ordered := make([]corpusItem, 0, len(byID))
	for _, id := range s.rs.corpusIDs() {
		ordered = append(ordered, byID[id])
	}

	type change struct {
		ItemKey  string   `json:"item_key"`
		ChangeID string   `json:"change_id"`
		Base     string   `json:"base_revision"`
		Paths    []string `json:"paths"`
		Content  string   `json:"content"`
	}
	changes := make([]change, 0, len(s.rs.ItemKeys))
	for _, k := range s.rs.ItemKeys {
		p := s.rs.Packages[k]
		var paths []string
		for _, x := range p.Change.Paths {
			mark := "modified"
			if !x.ExistedBefore {
				mark = "new"
			}
			paths = append(paths, x.Path+"  ("+mark+")")
		}
		changes = append(changes, change{k, p.Change.ChangeID, p.Change.BaseRevision, paths, p.Change.Content})
	}
	writeJSON(w, map[string]any{
		"adjudicator":  s.session.Adjudicator,
		"manifest":     s.rs.Manifest.DigestSHA256,
		"blind_corpus": s.rs.Corpus.DigestSHA256,
		"overlap":      s.rs.Manifest.OverlapItemKeys,
		"changes":      changes,
		"corpus":       ordered,
		"labels":       s.session.Labels(),
		"state":        s.stateLocked(),
	})
}

func (s *server) stateLocked() map[string]any {
	cov := map[string]prospectivelabel.Coverage{}
	presented := map[string][]string{}
	for _, k := range s.rs.ItemKeys {
		cov[k] = s.session.Coverage(k)
		presented[k] = s.session.PresentedIDs(k)
	}
	return map[string]any{"coverage": cov, "presented": presented}
}

func (s *server) handlePresent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ItemKey string   `json:"item_key"`
		IDs     []string `json:"ids"`
	}
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.session.Present(req.ItemKey, req.IDs...); err != nil {
		httpError(w, err)
		return
	}
	if err := s.save(); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.stateLocked())
}

func (s *server) handleLabel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ItemKey      string `json:"item_key"`
		CorpusItemID string `json:"corpus_item_id"`
		Label        string `json:"label"`
	}
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(req.Label) == "" {
		s.session.Unset(req.ItemKey, req.CorpusItemID)
	} else if err := s.session.Assign(req.ItemKey, req.CorpusItemID, req.Label); err != nil {
		httpError(w, err)
		return
	}
	if err := s.save(); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, s.stateLocked())
}

func (s *server) handleSweep(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ItemKey string `json:"item_key"`
	}
	if !decode(w, r, &req) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.session.Sweep(req.ItemKey)
	if err != nil {
		httpError(w, err)
		return
	}
	if err := s.save(); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"swept": n, "state": s.stateLocked()})
}

func (s *server) handleExport(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(s.frozenAt) == "" {
		httpError(w, fmt.Errorf("--frozen-at is required to export: a self-stamped answer key is not reproducible"))
		return
	}
	var incomplete []string
	ls := prospectivelabel.LabelSet{
		SchemaVersion:              prospectivelabel.LabelSetSchemaVersion,
		ProtocolID:                 s.rs.Manifest.ProtocolID,
		SampleManifestDigestSHA256: s.rs.Manifest.DigestSHA256,
		BlindCorpusDigestSHA256:    s.rs.Corpus.DigestSHA256,
		WorldRevision:              s.rs.Manifest.World.Revision,
		Adjudicator:                s.session.Adjudicator,
		SecondAdjudicatorStatus:    prospectivelabel.SecondAdjudicatorUnavailable,
		FrozenAt:                   s.frozenAt,
		Labels:                     s.session.Labels(),
		Totals:                     map[string]int{},
	}
	for _, k := range s.rs.ItemKeys {
		c := s.session.Coverage(k)
		ls.Coverage = append(ls.Coverage, c)
		if !c.AdjudicationCoverageComplete {
			incomplete = append(incomplete, k)
		}
		ls.Totals["individually_assigned"] += c.IndividuallyAssigned
		ls.Totals["bulk_swept_not_applicable"] += c.BulkSweptNotApplicable
		ls.Totals["unresolved"] += c.Unresolved
		ls.Totals["unlabelled"] += c.Unlabelled
	}
	ls.Totals["pairs"] = len(s.rs.ItemKeys) * len(s.rs.Corpus.Items)
	for _, l := range ls.Labels {
		ls.Totals["label_"+l.Label]++
	}
	if len(incomplete) > 0 {
		sort.Strings(incomplete)
		httpError(w, fmt.Errorf("%d change(s) are not complete — every eligible item must be presented and every pair labelled before the key is frozen: %s",
			len(incomplete), strings.Join(incomplete, ", ")))
		return
	}
	ls, err := ls.Seal()
	if err != nil {
		httpError(w, err)
		return
	}
	body, err := json.MarshalIndent(ls, "", "  ")
	if err != nil {
		httpError(w, err)
		return
	}
	if err := os.WriteFile(s.outPath, append(body, '\n'), 0o644); err != nil {
		httpError(w, err)
		return
	}
	writeJSON(w, map[string]any{"written": s.outPath, "digest": ls.DigestSHA256, "totals": ls.Totals})
}

func decode(w http.ResponseWriter, r *http.Request, into any) bool {
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		httpError(w, err)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
