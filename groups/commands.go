package groups

import (
	"context"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

// ManageInput exposes native mutation workflows to go-command-compatible
// transports. Run/Execute delegate authorization and transactions to Service.
type ManageInput struct {
	Actor       types.ActorRef
	GroupID     uuid.UUID
	PrincipalID uuid.UUID
	RoleID      uuid.UUID
	Operation   Operation
	Name        string
}

type Operation string

const (
	Create       Operation = "create"
	Archive      Operation = "archive"
	Activate     Operation = "activate"
	AddMember    Operation = "member.add"
	RemoveMember Operation = "member.remove"
	AssignRole   Operation = "role.assign"
	RevokeRole   Operation = "role.revoke"
)

func (ManageInput) Type() string { return "command.group.manage" }
func (i ManageInput) Validate() error {
	if i.Actor.ID == uuid.Nil {
		return ErrInvalid
	}
	if i.Operation == Create {
		if i.Name == "" {
			return ErrInvalid
		}
		return nil
	}
	if i.GroupID == uuid.Nil {
		return ErrInvalid
	}
	switch i.Operation {
	case Archive, Activate:
		return nil
	case AddMember, RemoveMember:
		if i.PrincipalID != uuid.Nil {
			return nil
		}
	case AssignRole, RevokeRole:
		if i.PrincipalID != uuid.Nil && i.RoleID != uuid.Nil {
			return nil
		}
	}
	return ErrInvalid
}

type ManageCommand struct{ service *Service }

func NewManageCommand(service *Service) *ManageCommand { return &ManageCommand{service: service} }
func (c *ManageCommand) Run(ctx context.Context, i ManageInput) (*Group, error) {
	if c == nil || c.service == nil {
		return nil, ErrInvalid
	}
	if err := i.Validate(); err != nil {
		return nil, err
	}
	switch i.Operation {
	case Create:
		return c.service.Create(ctx, i.Actor, i.Name)
	case Archive, Activate:
		return nil, c.service.SetActive(ctx, i.Actor, i.GroupID, i.Operation == Activate)
	case AddMember, RemoveMember:
		return nil, c.service.SetMember(ctx, i.Actor, i.GroupID, i.PrincipalID, i.Operation == AddMember)
	case AssignRole, RevokeRole:
		return nil, c.service.SetRole(ctx, i.Actor, i.GroupID, i.PrincipalID, i.RoleID, i.Operation == AssignRole)
	}
	return nil, ErrInvalid
}
func (c *ManageCommand) Execute(ctx context.Context, i ManageInput) error {
	_, err := c.Run(ctx, i)
	return err
}
