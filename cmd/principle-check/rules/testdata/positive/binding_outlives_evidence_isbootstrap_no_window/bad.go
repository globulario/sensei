// Positive-control fixture for binding_outlives_evidence_isbootstrap_no_window.
// Bare read of authCtx.IsBootstrap.
package badfix

type authContext struct {
	IsBootstrap bool
}

func grant(authCtx *authContext) bool {
	if authCtx.IsBootstrap { // BAD: consumed without window re-check
		return true
	}
	return false
}
