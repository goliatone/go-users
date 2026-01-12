package command

import (
	"strings"

	"github.com/goliatone/go-users/pkg/types"
)

func normalizeAuthUser(input *types.AuthUser) *types.AuthUser {
	if input == nil {
		return nil
	}
	user := *input
	user.Email = strings.TrimSpace(user.Email)
	user.Username = strings.TrimSpace(user.Username)
	user.FirstName = strings.TrimSpace(user.FirstName)
	user.LastName = strings.TrimSpace(user.LastName)
	user.Role = strings.TrimSpace(user.Role)
	if input.Metadata == nil {
		user.Metadata = nil
	} else {
		user.Metadata = cloneMap(input.Metadata)
	}
	return &user
}
