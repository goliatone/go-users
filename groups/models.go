// Package groups provides optional flat Groups, independent membership and
// exact role grants. It does not interpret tenant/org as Group identities.
package groups

import (
	"errors"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/uptrace/bun"
	"time"
)

var ErrDenied = errors.New("groups: authorization denied")
var ErrInvalid = errors.New("groups: invalid configuration or input")

const (
	ActionRead    types.PolicyAction = "groups:read"
	ActionCreate  types.PolicyAction = "groups:create"
	ActionManage  types.PolicyAction = "groups:manage"
	ActionMembers types.PolicyAction = "groups:members:write"
	ActionRoles   types.PolicyAction = "groups:roles:write"
)

// Group has no tenant/org parent or implicit hierarchy.
type Group struct {
	bun.BaseModel `bun:"table:user_groups"`
	ID            uuid.UUID `bun:"id,pk,type:uuid"`
	Name          string    `bun:"name,notnull"`
	Active        bool      `bun:"active,notnull"`
	CreatedAt     time.Time `bun:"created_at,notnull"`
	UpdatedAt     time.Time `bun:"updated_at,notnull"`
}

// Membership is independent of grants. PrincipalID is an opaque identifier and
// need not be a human user or a row in an authentication table.
type Membership struct {
	bun.BaseModel `bun:"table:user_group_memberships"`
	GroupID       uuid.UUID `bun:"group_id,pk,type:uuid"`
	PrincipalID   uuid.UUID `bun:"principal_id,pk,type:uuid"`
	Active        bool      `bun:"active,notnull"`
	CreatedAt     time.Time `bun:"created_at,notnull"`
	UpdatedAt     time.Time `bun:"updated_at,notnull"`
}

func Scope(id uuid.UUID) types.ScopeFilter { return types.ScopeFilter{}.WithLabel("group", id) }
