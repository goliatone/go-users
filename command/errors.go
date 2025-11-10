package command

import "errors"

var (
	// ErrLifecycleUserIDRequired indicates the transition command lacks a user ID.
	ErrLifecycleUserIDRequired = errors.New("go-users: lifecycle transition requires user id")
	// ErrLifecycleTargetRequired indicates the desired lifecycle state is missing.
	ErrLifecycleTargetRequired = errors.New("go-users: lifecycle transition requires target state")
	// ErrActorRequired indicates an actor reference was not supplied.
	ErrActorRequired = errors.New("go-users: actor reference required")
	// ErrInviteEmailRequired occurs when an invite omits the email address.
	ErrInviteEmailRequired = errors.New("go-users: invite requires email")
	// ErrPasswordHashRequired occurs when a password reset omits the hashed password.
	ErrPasswordHashRequired = errors.New("go-users: password reset requires password hash")
	// ErrUserIDsRequired occurs when bulk handlers are invoked without targets.
	ErrUserIDsRequired = errors.New("go-users: user ids required")
	// ErrRoleNameRequired occurs when a role command omits the role name.
	ErrRoleNameRequired = errors.New("go-users: role name required")
	// ErrRoleIDRequired signals the role ID was missing.
	ErrRoleIDRequired = errors.New("go-users: role id required")
	// ErrUserIDRequired occurs when assignment commands omit the user.
	ErrUserIDRequired = errors.New("go-users: user id required")
	// ErrActivityVerbRequired indicates an activity log entry is missing a verb.
	ErrActivityVerbRequired = errors.New("go-users: activity verb required")
	// ErrPreferenceKeyRequired indicates the preference key was missing.
	ErrPreferenceKeyRequired = errors.New("go-users: preference key required")
	// ErrPreferenceValueRequired indicates the preference value payload was missing.
	ErrPreferenceValueRequired = errors.New("go-users: preference value required")
)
