// Stub collector package so the fixture's Evaluate signature
// `Evaluate($snap *collector.Snapshot, ...)` resolves and the
// gogrep pattern matches `*collector.Snapshot`.
package collector

type Snapshot struct {
	Nodes  []string
	Errors []error
}
