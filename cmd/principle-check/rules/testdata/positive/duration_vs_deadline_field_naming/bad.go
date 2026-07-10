// Positive-control fixture for duration_vs_deadline_field_naming.
// Struct field named Timeout with int type and no unit qualifier.
package badfix

type retryConfig struct {
	Name    string
	Timeout int // BAD: seconds? ms? deadline? ambiguous
	Tries   int
}

type backoffConfig struct {
	Backoff int64 // BAD: ambiguous int-family time field
}
