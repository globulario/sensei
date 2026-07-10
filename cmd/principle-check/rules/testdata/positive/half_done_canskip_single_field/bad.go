// Positive-control fixture for half_done_canskip_single_field.
// canSkip* function whose entire body is a single return-equality.
package badfix

type artifact struct {
	State    string
	Manifest string
}

func canSkipDueToExistingState(a artifact, want string) bool {
	return a.State == want // BAD: single-field completion check
}
