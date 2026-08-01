#!/usr/bin/env python3
from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    if old not in text:
        raise SystemExit(f"{label} not found")
    return text.replace(old, new, 1)


gate = Path("golang/architecture/evaluatorcomposition/sensei_gate.go")
text = gate.read_text()
text = replace_once(
    text,
    '''import (
\t"context"
\t"encoding/json"
\t"fmt"
\t"path/filepath"
\t"strings"
\t"time"
''',
    '''import (
\t"context"
\t"crypto/sha256"
\t"encoding/hex"
\t"encoding/json"
\t"fmt"
\t"os"
\t"path/filepath"
\t"strings"
\t"time"
''',
    "sensei gate imports",
)
text = replace_once(
    text,
    '''\tdescriptor EvaluatorDescriptor
\tconfig     SenseiGateConfig
\tsurface    EvaluatorSurface
\trunner     CommandRunner
\tsink       EvidenceSink
''',
    '''\tdescriptor    EvaluatorDescriptor
\tconfig        SenseiGateConfig
\tpolicyContent []byte
\tpolicyDigest  string
\tsurface       EvaluatorSurface
\trunner        CommandRunner
\tsink          EvidenceSink
''',
    "SenseiGateEvaluator fields",
)
text = replace_once(
    text,
    '''\tif runner == nil || sink == nil {
\t\treturn nil, fmt.Errorf("NewSenseiGateEvaluator: runner and sink are required")
\t}
\tconfig.Environment = append([]string(nil), config.Environment...)
''',
    '''\tif runner == nil || sink == nil {
\t\treturn nil, fmt.Errorf("NewSenseiGateEvaluator: runner and sink are required")
\t}
\tif strings.TrimSpace(config.PolicyPath) == "" || !filepath.IsAbs(config.PolicyPath) {
\t\treturn nil, fmt.Errorf("NewSenseiGateEvaluator: PolicyPath must be a non-empty absolute path outside the candidate surface")
\t}
\tsurfaceRoot, err := surface.RootPath()
\tif err != nil {
\t\treturn nil, fmt.Errorf("NewSenseiGateEvaluator: surface root: %w", err)
\t}
\tresolvedRoot, err := filepath.EvalSymlinks(surfaceRoot)
\tif err != nil {
\t\treturn nil, fmt.Errorf("NewSenseiGateEvaluator: resolve surface root: %w", err)
\t}
\tpolicyInfo, err := os.Lstat(config.PolicyPath)
\tif err != nil {
\t\treturn nil, fmt.Errorf("NewSenseiGateEvaluator: inspect policy: %w", err)
\t}
\tif policyInfo.Mode()&os.ModeSymlink != 0 || !policyInfo.Mode().IsRegular() {
\t\treturn nil, fmt.Errorf("NewSenseiGateEvaluator: PolicyPath must name a real, non-symlink regular file")
\t}
\tresolvedPolicy, err := filepath.EvalSymlinks(config.PolicyPath)
\tif err != nil {
\t\treturn nil, fmt.Errorf("NewSenseiGateEvaluator: resolve policy: %w", err)
\t}
\trel, err := filepath.Rel(resolvedRoot, resolvedPolicy)
\tif err != nil {
\t\treturn nil, fmt.Errorf("NewSenseiGateEvaluator: compare policy and surface paths: %w", err)
\t}
\tif rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
\t\treturn nil, fmt.Errorf("NewSenseiGateEvaluator: PolicyPath %q is inside the candidate surface; a candidate must never select or rewrite its own gate policy", config.PolicyPath)
\t}
\tpolicyContent, err := os.ReadFile(resolvedPolicy)
\tif err != nil {
\t\treturn nil, fmt.Errorf("NewSenseiGateEvaluator: read policy snapshot: %w", err)
\t}
\tif len(policyContent) == 0 {
\t\treturn nil, fmt.Errorf("NewSenseiGateEvaluator: policy snapshot must not be empty")
\t}
\tpolicySum := sha256.Sum256(policyContent)
\tpolicyDigest := hex.EncodeToString(policySum[:])
\tconfig.Environment = append([]string(nil), config.Environment...)
''',
    "gate constructor authority block",
)
text = replace_once(
    text,
    '''\t\tRequiredCapabilities: []string{"sensei-gate-cli", "sealed-candidate-git-diff-surface"},
''',
    '''\t\tRequiredCapabilities: []string{"sensei-gate-cli", "sealed-candidate-git-diff-surface", "sensei-gate-policy-sha256:" + policyDigest},
''',
    "gate descriptor capabilities",
)
text = replace_once(
    text,
    '''\treturn &SenseiGateEvaluator{descriptor: descriptor, config: config, surface: surface, runner: runner, sink: sink}, nil
''',
    '''\treturn &SenseiGateEvaluator{
\t\tdescriptor: descriptor, config: config,
\t\tpolicyContent: append([]byte(nil), policyContent...), policyDigest: policyDigest,
\t\tsurface: surface, runner: runner, sink: sink,
\t}, nil
''',
    "gate constructor return",
)
describe = '''func (e *SenseiGateEvaluator) Describe(context.Context) (EvaluatorDescriptor, error) {
\tif e == nil {
\t\treturn EvaluatorDescriptor{}, fmt.Errorf("SenseiGateEvaluator.Describe: nil evaluator")
\t}
\treturn e.descriptor, nil
}
'''
text = replace_once(
    text,
    describe,
    describe
    + '''
func materializeSenseiGatePolicy(rootPath string, content []byte) (string, error) {
\troot, err := os.OpenRoot(rootPath)
\tif err != nil {
\t\treturn "", err
\t}
\tdefer root.Close()
\tname := filepath.Join(".git", "sensei-o4-policy.yaml")
\tif err := root.WriteFile(name, content, 0o600); err != nil {
\t\treturn "", err
\t}
\treturn filepath.Join(rootPath, name), nil
}
''',
    "gate policy materializer insertion",
)
text = replace_once(
    text,
    '''\troot, err := e.surface.RootPath()
\tif err != nil {
\t\treturn EvaluatorResult{}, fmt.Errorf("SenseiGateEvaluator.Evaluate: surface: %w", err)
\t}

\targs := []string{
''',
    '''\troot, err := e.surface.RootPath()
\tif err != nil {
\t\treturn EvaluatorResult{}, fmt.Errorf("SenseiGateEvaluator.Evaluate: surface: %w", err)
\t}
\tpolicyPath, err := materializeSenseiGatePolicy(root, e.policyContent)
\tif err != nil {
\t\treturn EvaluatorResult{}, fmt.Errorf("SenseiGateEvaluator.Evaluate: materialize frozen policy %s: %w", e.policyDigest, err)
\t}

\targs := []string{
''',
    "gate evaluate policy snapshot",
)
text = replace_once(
    text,
    '''\t\t"--event-log", "",
\t\t"--total-timeout", e.config.TotalTimeout.String(),
''',
    '''\t\t"--event-log", "",
\t\t"--policy", policyPath,
\t\t"--total-timeout", e.config.TotalTimeout.String(),
''',
    "gate policy argument",
)
text = replace_once(
    text,
    '''\tif strings.TrimSpace(e.config.PolicyPath) != "" {
\t\targs = append(args, "--policy", e.config.PolicyPath)
\t}
''',
    "",
    "old optional policy block",
)
gate.write_text(text)


evaluator = Path("golang/architecture/evaluatorcomposition/evaluator.go")
text = evaluator.read_text()
text = replace_once(
    text,
    '''// ExecuteEvaluator performs one evaluator invocation and owns the disposable
''',
    '''func closeEvaluatorFailure(surface EvaluatorSurface, failure error) error {
\tif cleanupErr := surface.Close(); cleanupErr != nil {
\t\treturn fmt.Errorf("%v; cleanup: %w", failure, cleanupErr)
\t}
\treturn failure
}

// ExecuteEvaluator performs one evaluator invocation and owns the disposable
''',
    "cleanup failure helper insertion",
)
for old, new, label in [
    (
        '''\tif err := ValidateEvaluatorDescriptor(descriptor); err != nil {
\t\t_ = surface.Close()
\t\treturn EvaluatorExecution{}, fmt.Errorf("ExecuteEvaluator: invalid descriptor: %w", err)
\t}
''',
        '''\tif err := ValidateEvaluatorDescriptor(descriptor); err != nil {
\t\treturn EvaluatorExecution{}, closeEvaluatorFailure(surface, fmt.Errorf("ExecuteEvaluator: invalid descriptor: %w", err))
\t}
''',
        "invalid descriptor cleanup",
    ),
    (
        '''\tif result.EvaluatorID != descriptor.EvaluatorID {
\t\t_ = surface.Close()
\t\treturn EvaluatorExecution{}, fmt.Errorf("ExecuteEvaluator: result evaluator_id %q does not match descriptor %q", result.EvaluatorID, descriptor.EvaluatorID)
\t}
''',
        '''\tif result.EvaluatorID != descriptor.EvaluatorID {
\t\treturn EvaluatorExecution{}, closeEvaluatorFailure(surface, fmt.Errorf("ExecuteEvaluator: result evaluator_id %q does not match descriptor %q", result.EvaluatorID, descriptor.EvaluatorID))
\t}
''',
        "evaluator id cleanup",
    ),
    (
        '''\tif result.EvaluatorDescriptorDigestSHA256 != descriptor.DescriptorDigestSHA256 {
\t\t_ = surface.Close()
\t\treturn EvaluatorExecution{}, fmt.Errorf("ExecuteEvaluator: result descriptor digest does not match accepted descriptor")
\t}
''',
        '''\tif result.EvaluatorDescriptorDigestSHA256 != descriptor.DescriptorDigestSHA256 {
\t\treturn EvaluatorExecution{}, closeEvaluatorFailure(surface, fmt.Errorf("ExecuteEvaluator: result descriptor digest does not match accepted descriptor"))
\t}
''',
        "descriptor digest cleanup",
    ),
    (
        '''\tif result.EvaluationInputDigestSHA256 != input.EvaluationInputDigestSHA256 {
\t\t_ = surface.Close()
\t\treturn EvaluatorExecution{}, fmt.Errorf("ExecuteEvaluator: result input digest does not match exact EvaluationInput")
\t}
''',
        '''\tif result.EvaluationInputDigestSHA256 != input.EvaluationInputDigestSHA256 {
\t\treturn EvaluatorExecution{}, closeEvaluatorFailure(surface, fmt.Errorf("ExecuteEvaluator: result input digest does not match exact EvaluationInput"))
\t}
''',
        "input digest cleanup",
    ),
    (
        '''\tif result.CleanupSucceeded != nil {
\t\t_ = surface.Close()
\t\treturn EvaluatorExecution{}, fmt.Errorf("ExecuteEvaluator: evaluator must leave cleanup_succeeded nil; O4 owns surface cleanup truth")
\t}
''',
        '''\tif result.CleanupSucceeded != nil {
\t\treturn EvaluatorExecution{}, closeEvaluatorFailure(surface, fmt.Errorf("ExecuteEvaluator: evaluator must leave cleanup_succeeded nil; O4 owns surface cleanup truth"))
\t}
''',
        "cleanup truth refusal cleanup",
    ),
    (
        '''\tif err := ValidateEvaluatorResult(result); err != nil {
\t\t_ = surface.Close()
\t\treturn EvaluatorExecution{}, fmt.Errorf("ExecuteEvaluator: evaluator returned invalid result: %w", err)
\t}
''',
        '''\tif err := ValidateEvaluatorResult(result); err != nil {
\t\treturn EvaluatorExecution{}, closeEvaluatorFailure(surface, fmt.Errorf("ExecuteEvaluator: evaluator returned invalid result: %w", err))
\t}
''',
        "invalid result cleanup",
    ),
]:
    text = replace_once(text, old, new, label)
evaluator.write_text(text)


adapters = Path("golang/architecture/evaluatorcomposition/adapters_test.go")
text = adapters.read_text()
text = replace_once(
    text,
    '''func newSenseiGateTestEvaluator(t *testing.T, surface EvaluatorSurface, runner CommandRunner) *SenseiGateEvaluator {
\tt.Helper()
\texecutable := absoluteTestExecutable(t)
''',
    '''func newSenseiGateTestEvaluator(t *testing.T, surface EvaluatorSurface, runner CommandRunner) *SenseiGateEvaluator {
\tt.Helper()
\texecutable := absoluteTestExecutable(t)
\troot, err := surface.RootPath()
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
\t\tt.Fatal(err)
\t}
\tpolicyPath := filepath.Join(t.TempDir(), "gate-policy.yaml")
\tif err := os.WriteFile(policyPath, []byte("version: 1\\nrules: {}\\n"), 0o600); err != nil {
\t\tt.Fatal(err)
\t}
''',
    "gate test helper policy fixture",
)
text = replace_once(
    text,
    '''\t\tSenseiExecutable: executable,
\t\tAddress:          "127.0.0.1:10120",
''',
    '''\t\tSenseiExecutable: executable,
\t\tAddress:          "127.0.0.1:10120",
\t\tPolicyPath:       policyPath,
''',
    "gate test config policy path",
)
text = replace_once(
    text,
    '''\t\t\tfor _, required := range []string{"gate", "--diff HEAD", "--domain " + input.RepositoryDomain, "--repo-root " + surface.root, "--enforce", "--json"} {
''',
    '''\t\t\tfor _, required := range []string{"gate", "--diff HEAD", "--domain " + input.RepositoryDomain, "--repo-root " + surface.root, "--enforce", "--json", "--policy " + filepath.Join(surface.root, ".git", "sensei-o4-policy.yaml")} {
''',
    "gate args frozen policy assertion",
)
text += '''
func TestSenseiGateEvaluatorRejectsCandidateOwnedPolicyPath(t *testing.T) {
\tsurface := &recordingEvaluatorSurface{ref: "surface://test/sensei-gate-policy/git-diff", root: t.TempDir(), mode: SurfaceModeGitDiff}
\tpolicyDir := filepath.Join(surface.root, ".sensei")
\tif err := os.MkdirAll(policyDir, 0o755); err != nil {
\t\tt.Fatal(err)
\t}
\tpolicyPath := filepath.Join(policyDir, "policy.yaml")
\tif err := os.WriteFile(policyPath, []byte("version: 1\\nrules: {}\\n"), 0o600); err != nil {
\t\tt.Fatal(err)
\t}
\t_, err := NewSenseiGateEvaluator(SenseiGateConfig{
\t\tEvaluatorID: "sensei.gate", EvaluatorVersion: "v1",
\t\tSenseiExecutable: absoluteTestExecutable(t), PolicyPath: policyPath,
\t}, surface, &scriptedCommandRunner{}, NewMemoryEvidenceSink())
\tif err == nil || !strings.Contains(err.Error(), "inside the candidate surface") {
\t\tt.Fatalf("candidate-owned policy rejection = %v", err)
\t}
}
'''
adapters.write_text(text)


evaluator_test = Path("golang/architecture/evaluatorcomposition/evaluator_test.go")
text = evaluator_test.read_text()
text = replace_once(
    text,
    '''\t\tsurface := &recordingEvaluatorSurface{ref: "surface://test/binding-error/plain", root: t.TempDir(), mode: SurfaceModePlain}
''',
    '''\t\tcleanupErr := errors.New("binding cleanup failed")
\t\tsurface := &recordingEvaluatorSurface{ref: "surface://test/binding-error/plain", root: t.TempDir(), mode: SurfaceModePlain, closeErr: cleanupErr}
''',
    "binding refusal cleanup fixture",
)
text = replace_once(
    text,
    '''\t\tif _, err := ExecuteEvaluator(context.Background(), evaluator, input, surface); err == nil || !strings.Contains(err.Error(), "evaluator_id") {
\t\t\tt.Fatalf("binding refusal error = %v", err)
\t\t}
''',
    '''\t\tif _, err := ExecuteEvaluator(context.Background(), evaluator, input, surface); err == nil || !strings.Contains(err.Error(), "evaluator_id") || !strings.Contains(err.Error(), cleanupErr.Error()) {
\t\t\tt.Fatalf("binding refusal did not preserve cleanup failure: %v", err)
\t\t}
''',
    "binding refusal cleanup assertion",
)
evaluator_test.write_text(text)
