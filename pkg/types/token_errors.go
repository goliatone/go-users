package types

import "errors"

var (
	// ErrMissingSecureLinkManager occurs when securelink manager is not configured.
	ErrMissingSecureLinkManager = errors.New("go-users: missing securelink manager")
	// ErrMissingUserTokenRepository occurs when token persistence is unavailable.
	ErrMissingUserTokenRepository = errors.New("go-users: missing user token repository")
	// ErrMissingPasswordResetRepository occurs when reset persistence is unavailable.
	ErrMissingPasswordResetRepository = errors.New("go-users: missing password reset repository")
)
