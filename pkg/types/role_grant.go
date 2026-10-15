package types

import (
	"context"
	"errors"
)

// RoleGrantOperation distinguishes creating a grant from revoking one.
type RoleGrantOperation string

const (
	RoleGrantAssign RoleGrantOperation = "assign"
	RoleGrantRevoke RoleGrantOperation = "revoke"
)

// RoleGrantCheck supplies the candidate grant in addition to generic scope
// authorization. Role contains the current tenant/org catalog definition;
// Assignment.Scope contains the complete resolved scope, including labels.
type RoleGrantCheck struct {
	Operation  RoleGrantOperation
	Actor      ActorRef
	Assignment RoleAssignment
	Role       RoleDefinition
}

// RoleGrantValidator checks delegation, target eligibility and current grants
// before an assignment mutation. Implementations must treat the check as read-only.
type RoleGrantValidator interface {
	ValidateRoleGrant(context.Context, RoleGrantCheck) error
}

// RoleGrantValidatorFunc adapts a host function to RoleGrantValidator.
type RoleGrantValidatorFunc func(context.Context, RoleGrantCheck) error

func (f RoleGrantValidatorFunc) ValidateRoleGrant(ctx context.Context, check RoleGrantCheck) error {
	return f(ctx, check)
}

var (
	ErrMissingRoleGrantValidator  = errors.New("go-users: role grant validator is required")
	ErrInvalidRoleGrantDefinition = errors.New("go-users: invalid role definition for grant validation")
)
