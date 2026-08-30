package publication

import (
	"strings"
	"testing"
)

func v2Receipt() Receipt {
	return Receipt{
		Version:      ReceiptV2,
		Domain:       "github.com/globulario/sensei-code",
		Revision:     "f6b4755ff4d12591e9e802b2094b16a938260cc2",
		Tree:         "ad916f771bbc07523c92ff299c27af53c852aacd",
		State:        CleanExact,
		SourcePath:   "docs/awareness",
		SourceDigest: "cff0d6113939b6f986b873dffad22847491669d903d1254386ef57c18cdf9c23",
	}
}

func mustStatements(t *testing.T, r Receipt) []RDFStatement {
	t.Helper()
	st, err := statementsOf(r)
	if err != nil {
		t.Fatalf("rendering the specimen failed: %v", err)
	}
	return st
}

// The valid specimen must decode. Every generated mutation below is only
// meaningful because this passes.
func TestAValidV2ReceiptDecodes(t *testing.T) {
	r := v2Receipt()
	got, err := DecodeStoredReceipt(r.IRI(), mustStatements(t, r))
	if err != nil {
		t.Fatalf("a valid receipt was refused: %v", err)
	}
	if got != r {
		t.Fatalf("decode changed the receipt:\n got %+v\nwant %+v", got, r)
	}
}

// SCHEMA-GENERATED ADVERSARIAL MATRIX.
//
// The mutations are derived FROM THE SCHEMA rather than enumerated by hand, so
// a field added later is attacked automatically instead of waiting for someone
// to remember it. This is the replacement for chasing one reviewer
// counterexample at a time.
func TestEveryFieldMutationIsRefused(t *testing.T) {
	base := v2Receipt()
	schema, _ := SchemaFor(ReceiptV2)
	valid := mustStatements(t, base)

	// SUBJECT DELIBERATELY EMPTY.
	//
	// Keeping the real subject makes every mutation of an identity-bearing
	// field fail at the final hash comparison, so the test would pass even if
	// the SCHEMA accepted the malformed value -- passing for the wrong reason,
	// and hiding exactly the gaps this generator exists to find. Identity
	// mismatch is proved separately below.
	const subject = ""

	find := func(pred string) (int, bool) {
		for i, st := range valid {
			if st.Predicate == pred {
				return i, true
			}
		}
		return 0, false
	}

	for _, pred := range sortedFieldNames(schema.Fields) {
		spec := schema.Fields[pred]
		idx, present := find(pred)

		type mutation struct {
			name  string
			apply func() []RDFStatement
			skip  bool
		}
		clone := func() []RDFStatement { return append([]RDFStatement(nil), valid...) }

		muts := []mutation{
			{"remove required field", func() []RDFStatement {
				out := clone()
				return append(out[:idx], out[idx+1:]...)
			}, !present || spec.MinCount == 0},

			{"duplicate with a different value", func() []RDFStatement {
				out := clone()
				dup := out[idx]
				dup.Object.Value = dup.Object.Value + "-other"
				return append(out, dup)
			}, !present || spec.MaxCount > 1},

			{"change the RDF term kind", func() []RDFStatement {
				out := clone()
				// Must differ FROM THE SPEC. Hard-coding TermIRI made this a
				// no-op on fields that are already IRI-valued, so the mutation
				// was "accepted" because nothing had changed.
				out[idx].Object.Kind = TermLiteral
				if spec.TermKind == TermLiteral {
					out[idx].Object.Kind = TermIRI
				}
				return out
			}, !present},

			{"attach a datatype", func() []RDFStatement {
				out := clone()
				out[idx].Object.Datatype = "http://www.w3.org/2001/XMLSchema#string"
				return out
			}, !present},

			{"attach a language tag", func() []RDFStatement {
				out := clone()
				out[idx].Object.Language = "en"
				return out
			}, !present},

			{"make the lexical value invalid", func() []RDFStatement {
				out := clone()
				out[idx].Object.Value = "/absolute/../nonsense"
				return out
			}, !present || spec.ValidateLexical == nil},

			{"add a forbidden field", func() []RDFStatement {
				return append(clone(), RDFStatement{
					Predicate: pred,
					Object:    Term{Kind: TermLiteral, Value: "x"},
				})
			}, present || spec.MaxCount != 0},
		}

		for _, m := range muts {
			if m.skip {
				continue
			}
			t.Run(shortPred(pred)+"/"+m.name, func(t *testing.T) {
				if _, err := DecodeStoredReceipt(subject, m.apply()); err == nil {
					t.Fatalf("mutation %q on %s was ACCEPTED", m.name, pred)
				}
			})
		}
	}
}

// Cross-cutting mutations that are not per-field.
func TestStructuralMutationsAreRefused(t *testing.T) {
	base := v2Receipt()
	subject := base.IRI()
	valid := mustStatements(t, base)

	cases := map[string][]RDFStatement{
		"unknown publication predicate": append(append([]RDFStatement(nil), valid...),
			RDFStatement{Predicate: PublicationFieldPrefix + "Mystery", Object: Term{Kind: TermLiteral, Value: "x"}}),

		"v1-only field injected into v2": append(append([]RDFStatement(nil), valid...),
			RDFStatement{Predicate: pRoot, Object: Term{Kind: TermLiteral, Value: "/tmp/build/docs/awareness"}}),

		"future version": func() []RDFStatement {
			out := append([]RDFStatement(nil), valid...)
			for i := range out {
				if out[i].Predicate == pVersion {
					out[i].Object.Value = "v99"
				}
			}
			return out
		}(),

		"two stated versions": append(append([]RDFStatement(nil), valid...),
			RDFStatement{Predicate: pVersion, Object: Term{Kind: TermLiteral, Value: "v1"}}),

		"unknown term kind": func() []RDFStatement {
			out := append([]RDFStatement(nil), valid...)
			out[0].Object.Kind = TermUnknown
			return out
		}(),
	}
	for name, terms := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeStoredReceipt(subject, terms); err == nil {
				t.Fatalf("%s was ACCEPTED", name)
			}
		})
	}
}

// v1 keeps its historical shape: SourceRoot is legal baggage, the version
// predicate is forbidden because v1's frozen identity cannot authenticate it,
// and A1's receipt must still decode.
func TestV1KeepsItsHistoricalShape(t *testing.T) {
	legacy := Receipt{
		Domain:       "github.com/globulario/sensei-code",
		Revision:     "f6b4755ff4d12591e9e802b2094b16a938260cc2",
		Tree:         "ad916f771bbc07523c92ff299c27af53c852aacd",
		State:        CleanExact,
		SourceRoot:   "/tmp/build/docs/awareness",
		SourceDigest: "cff0d6113939b6f986b873dffad22847491669d903d1254386ef57c18cdf9c23",
	}
	if _, err := DecodeStoredReceipt(legacy.IRI(), mustStatements(t, legacy)); err != nil {
		t.Fatalf("a historical v1 receipt was refused: %v", err)
	}
	// An explicitly injected "v1" is present-and-unhashed under the frozen
	// algorithm, so v1 forbids the predicate outright.
	injected := append(mustStatements(t, legacy),
		RDFStatement{Predicate: pVersion, Object: Term{Kind: TermLiteral, Value: "v1"}})
	if _, err := DecodeStoredReceipt(legacy.IRI(), injected); err == nil {
		t.Fatal("an explicit v1 version predicate was accepted, though v1 cannot authenticate it")
	}
}

// Writer and verifier share one model: a receipt that would be refused must
// never be published.
func TestPublicationValidationRefusesWhatTheReaderWouldRefuse(t *testing.T) {
	if err := ValidateForPublication(v2Receipt()); err != nil {
		t.Fatalf("a valid receipt failed pre-publication validation: %v", err)
	}
	bad := v2Receipt()
	bad.State = Unknown // UNKNOWN with a revision violates the cross-field rule
	if err := ValidateForPublication(bad); err == nil {
		t.Fatal("a receipt its own reader would refuse passed pre-publication validation")
	}
	absolute := v2Receipt()
	absolute.SourcePath = "/tmp/build/docs/awareness"
	if err := ValidateForPublication(absolute); err == nil {
		t.Fatal("an absolute SourcePath passed pre-publication validation")
	}
}

func shortPred(p string) string {
	if i := strings.LastIndex(p, "#"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// Identity mismatch is its own property, tested where it cannot mask a schema
// gap.
func TestIdentityMismatchIsRefusedSeparately(t *testing.T) {
	base := v2Receipt()
	terms := mustStatements(t, base)
	if _, err := DecodeStoredReceipt(base.IRI(), terms); err != nil {
		t.Fatalf("the valid specimen failed under its own subject: %v", err)
	}
	if _, err := DecodeStoredReceipt(receiptPrefix+strings.Repeat("0", 64), terms); err == nil {
		t.Fatal("a receipt decoded under a subject it does not hash to")
	}
}

// EVERY identity-bearing field must actually change the identity.
//
// FieldSpec.IdentityBearing states the law while Receipt.Identity() keeps a
// hand-maintained field list, so a future version could mark a field
// identity-bearing in the schema and forget it in the hash -- reintroducing
// present-and-unhashed through schema evolution rather than through a missing
// check. This proves the two agree, mechanically, for every version.
func TestEveryIdentityBearingFieldChangesTheIdentity(t *testing.T) {
	setters := map[string]func(*Receipt, string){
		pDomain:    func(r *Receipt, v string) { r.Domain = v },
		pRevision:  func(r *Receipt, v string) { r.Revision = v },
		pTree:      func(r *Receipt, v string) { r.Tree = v },
		pState:     func(r *Receipt, v string) { r.State = SourceState(v) },
		pPath:      func(r *Receipt, v string) { r.SourcePath = v },
		pSourceDig: func(r *Receipt, v string) { r.SourceDigest = v },
		pRoot:      func(r *Receipt, v string) { r.SourceRoot = v },
		pVersion:   func(r *Receipt, v string) { r.Version = ReceiptVersion(v) },
	}
	for _, version := range KnownVersions() {
		schema, _ := SchemaFor(version)
		for _, pred := range sortedFieldNames(schema.Fields) {
			spec := schema.Fields[pred]
			set, known := setters[pred]
			if !known {
				// A CONSTANT field -- one whose only legal value is fixed by
				// the schema, like the receipt's rdf:type -- has no struct
				// representation and cannot vary. It therefore cannot be
				// identity-bearing, and saying so is the proof rather than an
				// exemption.
				if spec.ValidateLexical == nil {
					t.Fatalf("schema %s defines %s with no setter and no fixed value: "+
						"a new field must be wired here or its identity coverage is unproven", version, pred)
				}
				if spec.IdentityBearing {
					t.Errorf("%s/%s has no struct representation yet is marked IdentityBearing: "+
						"a constant cannot be hashed as if it varied", version, pred)
				}
				continue
			}
			base := v2Receipt()
			if version == ReceiptV1 {
				base = Receipt{
					Domain: "d", Revision: "abc", Tree: "t",
					State: CleanExact, SourceDigest: strings.Repeat("a", 64),
				}
			}
			before := base.Identity()
			mutated := base
			set(&mutated, "mutated-value")
			changed := mutated.Identity() != before

			if spec.IdentityBearing && !changed {
				t.Errorf("%s/%s is marked IdentityBearing but changing it does not change the identity: "+
					"it is present and unhashed", version, pred)
			}
			if !spec.IdentityBearing && changed {
				t.Errorf("%s/%s is not marked IdentityBearing yet changing it changes the identity: "+
					"the schema understates what the digest covers", version, pred)
			}
		}
	}
}

// The pointer's TYPE is authority-bearing and belongs to its closed schema.
func TestThePointerTypeIsPartOfItsSchema(t *testing.T) {
	good := v2Receipt()
	pointer := func(mut func([]RDFStatement) []RDFStatement) []RDFStatement {
		var out []RDFStatement
		for _, line := range strings.Split(string(good.Triples()), "\n") {
			p := "<" + PointerIRI(good.Domain) + "> <"
			if !strings.HasPrefix(line, p) {
				continue
			}
			_, pred, obj, ok := splitTriple(line)
			if !ok {
				continue
			}
			kind := TermLiteral
			if pred == typeIRI || pred == pCurrent {
				kind = TermIRI
			}
			out = append(out, RDFStatement{Predicate: pred, Object: Term{Kind: kind, Value: obj}})
		}
		if mut != nil {
			out = mut(out)
		}
		return out
	}

	if _, outcome, err := DecodePointer(good.Domain, pointer(nil)); outcome != PointerOK {
		t.Fatalf("the writer's own pointer was refused: %v %v", outcome, err)
	}

	cases := map[string]func([]RDFStatement) []RDFStatement{
		"type changed to a literal": func(in []RDFStatement) []RDFStatement {
			for i := range in {
				if in[i].Predicate == typeIRI {
					in[i].Object.Kind = TermLiteral
				}
			}
			return in
		},
		"type changed to another class": func(in []RDFStatement) []RDFStatement {
			for i := range in {
				if in[i].Predicate == typeIRI {
					in[i].Object.Value = receiptClassIRI
				}
			}
			return in
		},
		"type removed": func(in []RDFStatement) []RDFStatement {
			var out []RDFStatement
			for _, st := range in {
				if st.Predicate != typeIRI {
					out = append(out, st)
				}
			}
			return out
		},
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			if _, outcome, _ := DecodePointer(good.Domain, pointer(mut)); outcome == PointerOK {
				t.Fatalf("%s was accepted", name)
			}
		})
	}

	// A type-only remnant is BROKEN, not absent: a half-deleted pointer must
	// not read as never-published.
	remnant := []RDFStatement{{Predicate: typeIRI, Object: Term{Kind: TermIRI, Value: pointerClassIRI}}}
	if _, outcome, _ := DecodePointer(good.Domain, remnant); outcome != PointerBroken {
		t.Fatalf("a type-only remnant resolved %v, want PointerBroken", outcome)
	}
	// Genuinely nothing is still ABSENT.
	if _, outcome, _ := DecodePointer(good.Domain, nil); outcome != PointerNone {
		t.Fatalf("an empty pointer resolved %v, want PointerNone", outcome)
	}
}

// v2 DIRTY must carry the tree its own writer records.
func TestV2DirtyRequiresTheCommittedTree(t *testing.T) {
	r := v2Receipt()
	r.State = Dirty
	if err := ValidateForPublication(r); err != nil {
		t.Fatalf("a v2 DIRTY receipt WITH a tree was refused: %v", err)
	}
	r.Tree = ""
	if err := ValidateForPublication(r); err == nil {
		t.Fatal("a v2 DIRTY receipt with no tree passed validation")
	}
	// v1 keeps the looser rule: its receipts are historical and not reissued.
	legacy := Receipt{
		Domain: "d", Revision: strings.Repeat("a", 40), State: Dirty,
		SourceDigest: strings.Repeat("c", 64),
	}
	if _, err := DecodeStoredReceipt("", mustStatements(t, legacy)); err != nil {
		t.Fatalf("a v1 DIRTY receipt without a tree was refused, repainting history: %v", err)
	}
}

// FAMILY A: the receipt's own rdf:type is authority-bearing.
//
// It was added to the POINTER schema and not to the receipt schema, so a
// receipt whose type was removed or changed still verified -- the endpoint
// reporting VERIFIED for a resource the store no longer identifies as a
// publication receipt. The same fix applied to one of two places, again.
func TestTheReceiptTypeIsPartOfItsSchema(t *testing.T) {
	base := v2Receipt()
	valid := mustStatements(t, base)
	if _, err := DecodeStoredReceipt(base.IRI(), valid); err != nil {
		t.Fatalf("the valid specimen was refused: %v", err)
	}

	cases := map[string]func([]RDFStatement) []RDFStatement{
		"type removed": func(in []RDFStatement) []RDFStatement {
			var out []RDFStatement
			for _, st := range in {
				if st.Predicate != typeIRI {
					out = append(out, st)
				}
			}
			return out
		},
		"type as a literal": func(in []RDFStatement) []RDFStatement {
			out := append([]RDFStatement(nil), in...)
			for i := range out {
				if out[i].Predicate == typeIRI {
					out[i].Object.Kind = TermLiteral
				}
			}
			return out
		},
		"type of another class": func(in []RDFStatement) []RDFStatement {
			out := append([]RDFStatement(nil), in...)
			for i := range out {
				if out[i].Predicate == typeIRI {
					out[i].Object.Value = pointerClassIRI
				}
			}
			return out
		},
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeStoredReceipt(base.IRI(), mut(append([]RDFStatement(nil), valid...))); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

// F4: no unauthenticated authority-bearing statement is STORED.
//
// The receipt used to emit an rdfs:label repeating domain, revision and state
// as prose. It sat outside the schema, so an altered label left the receipt
// VERIFIED while stating contradictory provenance. The repair is deletion, not
// another validator: a second copy of authenticated facts can only ever
// disagree with the first.
func TestNoUnauthenticatedStatementIsStored(t *testing.T) {
	r := v2Receipt()
	nt := string(r.Triples())
	if strings.Contains(nt, "rdf-schema#label") {
		t.Fatalf("the receipt stores a label it does not authenticate:\n%s", nt)
	}
	// Every stored predicate must be one the schema defines, or be the pointer's.
	schema, _ := SchemaFor(ReceiptV2)
	for _, line := range strings.Split(nt, "\n") {
		if !strings.HasPrefix(line, "<"+r.IRI()+">") {
			continue
		}
		_, pred, _, ok := splitTriple(line)
		if !ok {
			continue
		}
		if _, defined := schema.Fields[pred]; !defined {
			t.Errorf("the receipt stores %s, which its schema does not define or authenticate", pred)
		}
	}
	// The presentation string still exists, derived from authenticated fields.
	if got := r.Label(); !strings.Contains(got, r.Domain) || !strings.Contains(got, string(r.State)) {
		t.Fatalf("the derived label lost its content: %q", got)
	}
}
