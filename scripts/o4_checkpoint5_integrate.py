#!/usr/bin/env python3
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    if old not in text:
        raise SystemExit(f"{label} not found")
    return text.replace(old, new, 1)


compose = Path("golang/architecture/evaluatorcomposition/compose.go")
text = compose.read_text()
text = text.replace("\tcleanupFailures := make([]string, 0)\n", "")
text = text.replace(
    '''\t\tif !*execution.Result.CleanupSucceeded {
\t\t\tcleanupFailures = append(cleanupFailures, id)
\t\t}
''',
    "",
)
start = text.index("func verifyRequiredProofDischarges(")
end = text.index("func ensureJSONEOF(")
replacement = '''func verifyRequiredProofDischarges(ctx context.Context, required []string, evidence []EvidenceReference, cited map[string]bool, resolver EvidenceResolver) error {
\tif len(required) == 0 {
\t\treturn nil
\t}
\tif resolver == nil {
\t\treturn fmt.Errorf("no evidence resolver was supplied for %d required proof discharges", len(required))
\t}
\tbyReference := make(map[string]EvidenceReference, len(evidence))
\tfor _, reference := range evidence {
\t\tif existing, ok := byReference[reference.Reference]; ok && existing.DigestSHA256 != reference.DigestSHA256 {
\t\t\treturn fmt.Errorf("evidence reference %q has conflicting digests", reference.Reference)
\t\t}
\t\tbyReference[reference.Reference] = reference
\t}
\treferences := make([]EvidenceReference, 0, len(byReference))
\tfor _, reference := range byReference {
\t\tif cited[reference.Reference] {
\t\t\treferences = append(references, reference)
\t\t}
\t}
\tsort.Slice(references, func(i, j int) bool { return references[i].Reference < references[j].Reference })
\tvalidated := make(map[string]bool, len(required))
\tfor _, reference := range references {
\t\tcontent, err := resolver.Resolve(ctx, reference)
\t\tif err != nil {
\t\t\treturn fmt.Errorf("resolve %q: %w", reference.Reference, err)
\t\t}
\t\tsum := sha256.Sum256(content)
\t\tactual := hex.EncodeToString(sum[:])
\t\tif actual != reference.DigestSHA256 {
\t\t\treturn fmt.Errorf("evidence reference %q declares digest %q but bytes hash to %q", reference.Reference, reference.DigestSHA256, actual)
\t\t}
\t\tvar marker struct {
\t\t\tDischargeDigestSHA256 string `json:"discharge_digest_sha256"`
\t\t}
\t\tif err := json.Unmarshal(content, &marker); err != nil || marker.DischargeDigestSHA256 == "" {
\t\t\tcontinue
\t\t}
\t\tvar discharge closureprotocol.ProofDischarge
\t\tdecoder := json.NewDecoder(bytes.NewReader(content))
\t\tdecoder.DisallowUnknownFields()
\t\tif err := decoder.Decode(&discharge); err != nil {
\t\t\treturn fmt.Errorf("decode ProofDischarge %q: %w", marker.DischargeDigestSHA256, err)
\t\t}
\t\tif err := ensureJSONEOF(decoder); err != nil {
\t\t\treturn fmt.Errorf("decode ProofDischarge %q: %w", marker.DischargeDigestSHA256, err)
\t\t}
\t\tif err := closureprotocol.ValidateProofDischarge(discharge); err != nil {
\t\t\treturn fmt.Errorf("validate ProofDischarge %q: %w", marker.DischargeDigestSHA256, err)
\t\t}
\t\trecomputed, err := closureprotocol.ProofDischargeDigest(discharge)
\t\tif err != nil {
\t\t\treturn fmt.Errorf("digest ProofDischarge %q: %w", marker.DischargeDigestSHA256, err)
\t\t}
\t\tif discharge.DischargeDigestSHA256 != recomputed {
\t\t\treturn fmt.Errorf("ProofDischarge declared digest %q does not match recomputed %q", discharge.DischargeDigestSHA256, recomputed)
\t\t}
\t\tif discharge.Status != closureprotocol.ReceiptValid {
\t\t\treturn fmt.Errorf("ProofDischarge %q status is %q, not valid", recomputed, discharge.Status)
\t\t}
\t\tvalidated[recomputed] = true
\t}
\tfor _, digest := range required {
\t\tif !validated[digest] {
\t\t\treturn fmt.Errorf("required discharge digest %q is absent from check-cited, validated evaluator evidence", digest)
\t\t}
\t}
\treturn nil
}

'''
text = text[:start] + replacement + text[end:]
compose.write_text(text)

run = Path("golang/architecture/evaluatorcomposition/run.go")
text = run.read_text()
text = replace_once(
    text,
    '''\tReceipt      *EvaluationReceipt
\tCandidate    *runnercomposition.CandidateArtifact
}
''',
    '''\tReceipt      *EvaluationReceipt
\tCandidate    *runnercomposition.CandidateArtifact
\tEvaluation   *synthesis.Evaluation
}
''',
    "Result fields",
)
run.write_text(text)

command = Path("golang/architecture/evaluatorcomposition/command.go")
text = command.read_text()
marker = '''func (s *MemoryEvidenceSink) Get(digest string) ([]byte, bool) {
\ts.mu.Lock()
\tdefer s.mu.Unlock()
\tcontent, ok := s.blobs[digest]
\treturn append([]byte(nil), content...), ok
}
'''
addition = marker + '''
func (s *MemoryEvidenceSink) Resolve(ctx context.Context, reference EvidenceReference) ([]byte, error) {
\tif err := ctx.Err(); err != nil {
\t\treturn nil, err
\t}
\tcontent, ok := s.Get(reference.DigestSHA256)
\tif !ok {
\t\treturn nil, fmt.Errorf("MemoryEvidenceSink.Resolve: digest %q not found", reference.DigestSHA256)
\t}
\tif err := validateResolvedEvidence(reference, content); err != nil {
\t\treturn nil, err
\t}
\treturn content, nil
}
'''
text = replace_once(text, marker, addition, "memory resolver")
marker = '''func evidenceDigest(content []byte) string {
\tsum := sha256.Sum256(content)
\treturn hex.EncodeToString(sum[:])
}
'''
addition = '''func (s *FSEvidenceSink) Resolve(ctx context.Context, reference EvidenceReference) ([]byte, error) {
\tif s == nil {
\t\treturn nil, fmt.Errorf("FSEvidenceSink.Resolve: nil sink")
\t}
\tif err := ctx.Err(); err != nil {
\t\treturn nil, err
\t}
\tcontent, err := os.ReadFile(filepath.Join(s.root, reference.DigestSHA256+".blob"))
\tif err != nil {
\t\treturn nil, fmt.Errorf("FSEvidenceSink.Resolve: read: %w", err)
\t}
\tif err := validateResolvedEvidence(reference, content); err != nil {
\t\treturn nil, err
\t}
\treturn content, nil
}

func validateResolvedEvidence(reference EvidenceReference, content []byte) error {
\twantReference := "evidence://sha256/" + reference.DigestSHA256
\tif reference.Reference != wantReference {
\t\treturn fmt.Errorf("evidence reference %q does not match digest-bound reference %q", reference.Reference, wantReference)
\t}
\tactual := evidenceDigest(content)
\tif actual != reference.DigestSHA256 {
\t\treturn fmt.Errorf("evidence reference digest %q does not match bytes %q", reference.DigestSHA256, actual)
\t}
\treturn nil
}

''' + marker
text = replace_once(text, marker, addition, "filesystem resolver")
command.write_text(text)
