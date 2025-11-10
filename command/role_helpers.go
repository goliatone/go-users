package command

import (
	"strings"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

func validateRoleMutation(actor types.ActorRef, name string) error {
	if actor.ID == uuid.Nil {
		return ErrActorRequired
	}
	if strings.TrimSpace(name) == "" {
		return ErrRoleNameRequired
	}
	return nil
}

func validateRoleTarget(roleID uuid.UUID, actor types.ActorRef) error {
	if roleID == uuid.Nil {
		return ErrRoleIDRequired
	}
	if actor.ID == uuid.Nil {
		return ErrActorRequired
	}
	return nil
}
