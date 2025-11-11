package crudsvc

import (
	"strings"

	auth "github.com/goliatone/go-auth"
	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-crud"
	goerrors "github.com/goliatone/go-errors"
	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/adapter/goauth"
	"github.com/goliatone/go-users/crudguard"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

// UserServiceConfig wires dependencies for the user inventory controller.
type UserServiceConfig struct {
	Guard     GuardAdapter
	Inventory gocommand.Querier[types.UserInventoryFilter, types.UserInventoryPage]
	AuthRepo  types.AuthRepository
}

// UserService provides a read-only go-crud service backed by the user inventory
// query so admin panels can list/search users without bypassing guards.
type UserService struct {
	guard     GuardAdapter
	inventory gocommand.Querier[types.UserInventoryFilter, types.UserInventoryPage]
	repo      types.AuthRepository
	logger    types.Logger
}

// NewUserService constructs the adapter.
func NewUserService(cfg UserServiceConfig, opts ...ServiceOption) *UserService {
	options := applyOptions(opts)
	return &UserService{
		guard:     cfg.Guard,
		inventory: cfg.Inventory,
		repo:      cfg.AuthRepo,
		logger:    options.logger,
	}
}

func (s *UserService) Create(crud.Context, *auth.User) (*auth.User, error) {
	return nil, notSupported(crud.OpCreate)
}

func (s *UserService) CreateBatch(crud.Context, []*auth.User) ([]*auth.User, error) {
	return nil, notSupported(crud.OpCreateBatch)
}

func (s *UserService) Update(crud.Context, *auth.User) (*auth.User, error) {
	return nil, notSupported(crud.OpUpdate)
}

func (s *UserService) UpdateBatch(crud.Context, []*auth.User) ([]*auth.User, error) {
	return nil, notSupported(crud.OpUpdateBatch)
}

func (s *UserService) Delete(crud.Context, *auth.User) error {
	return notSupported(crud.OpDelete)
}

func (s *UserService) DeleteBatch(crud.Context, []*auth.User) error {
	return notSupported(crud.OpDeleteBatch)
}

func (s *UserService) Index(ctx crud.Context, _ []repository.SelectCriteria) ([]*auth.User, int, error) {
	if s.inventory == nil {
		return nil, 0, goerrors.New("user inventory query missing", goerrors.CategoryInternal).WithCode(goerrors.CodeInternal)
	}
	res, err := s.guard.Enforce(crudguard.GuardInput{
		Context:   ctx,
		Operation: crud.OpList,
	})
	if err != nil {
		return nil, 0, err
	}
	filter := types.UserInventoryFilter{
		Actor:      res.Actor,
		Scope:      res.Scope,
		Keyword:    ctx.Query("q"),
		Pagination: types.Pagination{Limit: queryInt(ctx, "limit", 50), Offset: queryInt(ctx, "offset", 0)},
		Statuses:   parseLifecycleStates(ctx, "status"),
	}
	applyUserInventoryRowPolicy(&filter, res.Actor)
	page, err := s.inventory.Query(ctx.UserContext(), filter)
	if err != nil {
		return nil, 0, err
	}
	users := filterUserInventoryResults(page.Users, res.Actor)
	records := make([]*auth.User, 0, len(users))
	for _, user := range users {
		record := sanitizeUser(goauth.UserFromDomain(&user))
		records = append(records, applyUserFieldPolicy(record, res.Actor))
	}
	return records, len(users), nil
}

func (s *UserService) Show(ctx crud.Context, id string, _ []repository.SelectCriteria) (*auth.User, error) {
	if s.repo == nil {
		return nil, goerrors.New("auth repository missing", goerrors.CategoryInternal).WithCode(goerrors.CodeInternal)
	}
	userID, err := uuid.Parse(id)
	if err != nil {
		return nil, goerrors.New("invalid user id", goerrors.CategoryValidation).WithCode(goerrors.CodeBadRequest)
	}
	res, err := s.guard.Enforce(crudguard.GuardInput{
		Context:   ctx,
		Operation: crud.OpRead,
		TargetID:  userID,
	})
	if err != nil {
		return nil, err
	}
	if err := enforceUserRowAccess(res.Actor, userID); err != nil {
		return nil, err
	}
	authUser, err := s.repo.GetByID(ctx.UserContext(), userID)
	if err != nil {
		return nil, err
	}
	if authUser == nil {
		return nil, goerrors.New("user not found", goerrors.CategoryNotFound).WithCode(goerrors.CodeNotFound)
	}
	return applyUserFieldPolicy(sanitizeUser(goauth.UserFromDomain(authUser)), res.Actor), nil
}

func sanitizeUser(user *auth.User) *auth.User {
	if user == nil {
		return nil
	}
	clone := *user
	clone.PasswordHash = ""
	return &clone
}

func applyUserInventoryRowPolicy(filter *types.UserInventoryFilter, actor types.ActorRef) {
	if filter == nil {
		return
	}
	if actor.IsSupport() {
		filter.UserIDs = []uuid.UUID{actor.ID}
	}
}

func filterUserInventoryResults(users []types.AuthUser, actor types.ActorRef) []types.AuthUser {
	if !actor.IsSupport() {
		return users
	}
	filtered := make([]types.AuthUser, 0, 1)
	for _, user := range users {
		if user.ID == actor.ID {
			filtered = append(filtered, user)
		}
	}
	return filtered
}

func applyUserFieldPolicy(user *auth.User, actor types.ActorRef) *auth.User {
	if user == nil {
		return nil
	}
	if !actor.IsSupport() || user.ID == actor.ID {
		return user
	}
	user.Email = obfuscateEmail(user.Email)
	user.Username = ""
	user.FirstName = ""
	user.LastName = ""
	user.Metadata = nil
	user.Phone = ""
	user.ProfilePicture = ""
	user.LoginAttempts = 0
	user.LoginAttemptAt = nil
	user.LoggedInAt = nil
	user.SuspendedAt = nil
	user.ResetedAt = nil
	return user
}

func enforceUserRowAccess(actor types.ActorRef, target uuid.UUID) error {
	if !actor.IsSupport() || target == actor.ID {
		return nil
	}
	return goerrors.New("go-users: support actors can only access their own user record", goerrors.CategoryAuthz).
		WithCode(goerrors.CodeForbidden)
}

func obfuscateEmail(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return email
	}
	parts := strings.SplitN(email, "@", 2)
	local := parts[0]
	domain := ""
	if len(parts) == 2 {
		domain = parts[1]
	}
	switch {
	case len(local) <= 1:
		local = strings.Repeat("*", len(local))
	default:
		local = local[:1] + strings.Repeat("*", len(local)-1)
	}
	if domain != "" {
		return local + "@" + domain
	}
	return local
}
