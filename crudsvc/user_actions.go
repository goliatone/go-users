package crudsvc

import (
	"net/http"

	auth "github.com/goliatone/go-auth"
	"github.com/goliatone/go-crud"
	goerrors "github.com/goliatone/go-errors"
)

// UserInviteAction registers POST /users/invite to force invite mode creation.
func UserInviteAction(service *UserService) crud.Action[*auth.User] {
	return crud.Action[*auth.User]{
		Name:   "invite",
		Method: http.MethodPost,
		Target: crud.ActionTargetCollection,
		Path:   "/users/invite",
		Handler: func(ctx crud.ActionContext[*auth.User]) error {
			if service == nil {
				return goerrors.New("user service missing", goerrors.CategoryInternal).WithCode(goerrors.CodeInternal)
			}
			var record auth.User
			if err := ctx.BodyParser(&record); err != nil {
				return goerrors.Wrap(err, goerrors.CategoryValidation, "invalid invite payload").WithCode(goerrors.CodeBadRequest)
			}
			if record.Metadata == nil {
				record.Metadata = map[string]any{}
			}
			record.Metadata[userCreateModeMetadataKey] = userCreateModeInvite
			created, err := service.createUser(ctx, crud.OpCreate, &record)
			if err != nil {
				return err
			}
			return ctx.Status(http.StatusCreated).JSON(created)
		},
	}
}
