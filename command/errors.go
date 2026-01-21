package command

import (
	"errors"

	"github.com/goliatone/go-users/pkg/types"
)

var (
	// ErrLifecycleUserIDRequired indicates the transition command lacks a user ID.
	ErrLifecycleUserIDRequired = errors.New("go-users: lifecycle transition requires user id")
	// ErrLifecycleTargetRequired indicates the desired lifecycle state is missing.
	ErrLifecycleTargetRequired = errors.New("go-users: lifecycle transition requires target state")
	// ErrActorRequired indicates an actor reference was not supplied.
	ErrActorRequired = types.ErrActorRequired
	// ErrUserRequired indicates a user payload was not supplied.
	ErrUserRequired = errors.New("go-users: user payload required")
	// ErrUserNotFound indicates the requested user was not found.
	ErrUserNotFound = errors.New("go-users: user not found")
	// ErrUserEmailRequired indicates a user email address was missing.
	ErrUserEmailRequired = errors.New("go-users: user email required")
	// ErrInviteEmailRequired occurs when an invite omits the email address.
	ErrInviteEmailRequired = errors.New("go-users: invite requires email")
	// ErrInviteDisabled indicates the invite flow is disabled via feature gate.
	ErrInviteDisabled = errors.New("go-users: invite disabled")
	// ErrPasswordHashRequired occurs when a password reset omits the hashed password.
	ErrPasswordHashRequired = errors.New("go-users: password reset requires password hash")
	// ErrPasswordResetDisabled indicates password reset is disabled via feature gate.
	ErrPasswordResetDisabled = errors.New("go-users: password reset disabled")
	// ErrUserIDsRequired occurs when bulk handlers are invoked without targets.
	ErrUserIDsRequired = errors.New("go-users: user ids required")
	// ErrUsersRequired occurs when bulk user import lacks users.
	ErrUsersRequired = errors.New("go-users: users required")
	// ErrRoleNameRequired occurs when a role command omits the role name.
	ErrRoleNameRequired = errors.New("go-users: role name required")
	// ErrRoleIDRequired signals the role ID was missing.
	ErrRoleIDRequired = errors.New("go-users: role id required")
	// ErrUserIDRequired occurs when assignment commands omit the user.
	ErrUserIDRequired = types.ErrUserIDRequired
	// ErrActivityVerbRequired indicates an activity log entry is missing a verb.
	ErrActivityVerbRequired = errors.New("go-users: activity verb required")
	// ErrPreferenceKeyRequired indicates the preference key was missing.
	ErrPreferenceKeyRequired = errors.New("go-users: preference key required")
	// ErrPreferenceValueRequired indicates the preference value payload was missing.
	ErrPreferenceValueRequired = errors.New("go-users: preference value required")
	// ErrTokenRequired indicates a securelink token was missing.
	ErrTokenRequired = errors.New("go-users: token required")
	// ErrTokenTypeRequired indicates a token type was missing.
	ErrTokenTypeRequired = errors.New("go-users: token type required")
	// ErrTokenJTIRequired indicates the token payload lacked a JTI.
	ErrTokenJTIRequired = errors.New("go-users: token jti required")
	// ErrTokenNotFound indicates the token registry has no matching record.
	ErrTokenNotFound = errors.New("go-users: token not found")
	// ErrTokenExpired indicates the token has expired.
	ErrTokenExpired = errors.New("go-users: token expired")
	// ErrTokenAlreadyUsed indicates the token has already been consumed.
	ErrTokenAlreadyUsed = errors.New("go-users: token already used")
	// ErrTokenUserMismatch indicates the token user id mismatch.
	ErrTokenUserMismatch = errors.New("go-users: token user mismatch")
	// ErrResetIdentifierRequired indicates a reset identifier was missing.
	ErrResetIdentifierRequired = errors.New("go-users: password reset requires identifier")
	// ErrResetCommandRequired indicates the reset command dependency is missing.
	ErrResetCommandRequired = errors.New("go-users: password reset command required")
	// ErrSignupDisabled indicates self-registration is disabled via feature gate.
	ErrSignupDisabled = errors.New("go-users: signup disabled")
)
