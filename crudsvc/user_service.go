package crudsvc

import (
	"strings"

	auth "github.com/goliatone/go-auth"
	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-crud"
	goerrors "github.com/goliatone/go-errors"
	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/adapter/goauth"
	"github.com/goliatone/go-users/command"
	"github.com/goliatone/go-users/crudguard"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

// UserServiceConfig wires dependencies for the user inventory controller.
type UserServiceConfig struct {
	Guard         GuardAdapter
	Inventory     gocommand.Querier[types.UserInventoryFilter, types.UserInventoryPage]
	AuthRepo      types.AuthRepository
	Create        gocommand.Commander[command.UserCreateInput]
	Update        gocommand.Commander[command.UserUpdateInput]
	Invite        gocommand.Commander[command.UserInviteInput]
	Lifecycle     gocommand.Commander[command.UserLifecycleTransitionInput]
	BulkLifecycle gocommand.Commander[command.BulkUserTransitionInput]
}

// UserService provides a read-only go-crud service backed by the user inventory
// query so admin panels can list/search users without bypassing guards.
type UserService struct {
	guard         GuardAdapter
	inventory     gocommand.Querier[types.UserInventoryFilter, types.UserInventoryPage]
	repo          types.AuthRepository
	create        gocommand.Commander[command.UserCreateInput]
	update        gocommand.Commander[command.UserUpdateInput]
	invite        gocommand.Commander[command.UserInviteInput]
	lifecycle     gocommand.Commander[command.UserLifecycleTransitionInput]
	bulkLifecycle gocommand.Commander[command.BulkUserTransitionInput]
	logger        types.Logger
}

const (
	userCreateModeDirect       = "direct"
	userCreateModeInvite       = "invite"
	userCreateModeCreateInvite = "create_invite"
	userCreateModeMetadataKey  = "create_mode"
)

// NewUserService constructs the adapter.
func NewUserService(cfg UserServiceConfig, opts ...ServiceOption) *UserService {
	options := applyOptions(opts)
	return &UserService{
		guard:         cfg.Guard,
		inventory:     cfg.Inventory,
		repo:          cfg.AuthRepo,
		create:        cfg.Create,
		update:        cfg.Update,
		invite:        cfg.Invite,
		lifecycle:     cfg.Lifecycle,
		bulkLifecycle: cfg.BulkLifecycle,
		logger:        options.logger,
	}
}

func (s *UserService) Create(ctx crud.Context, record *auth.User) (*auth.User, error) {
	return s.createUser(ctx, crud.OpCreate, record)
}

func (s *UserService) CreateBatch(ctx crud.Context, records []*auth.User) ([]*auth.User, error) {
	return s.createUsersBatch(ctx, crud.OpCreateBatch, records)
}

func (s *UserService) Update(ctx crud.Context, record *auth.User) (*auth.User, error) {
	return s.updateUser(ctx, crud.OpUpdate, record)
}

func (s *UserService) UpdateBatch(ctx crud.Context, records []*auth.User) ([]*auth.User, error) {
	return s.updateUsersBatch(ctx, crud.OpUpdateBatch, records)
}

func (s *UserService) Delete(ctx crud.Context, record *auth.User) error {
	return s.deleteUser(ctx, crud.OpDelete, record)
}

func (s *UserService) DeleteBatch(ctx crud.Context, records []*auth.User) error {
	return s.deleteUsersBatch(ctx, crud.OpDeleteBatch, records)
}

func (s *UserService) createUser(ctx crud.Context, op crud.CrudOperation, record *auth.User) (*auth.User, error) {
	if record == nil {
		return nil, goerrors.New("user payload missing", goerrors.CategoryValidation).WithCode(goerrors.CodeBadRequest)
	}
	res, err := s.guard.Enforce(crudguard.GuardInput{
		Context:   ctx,
		Operation: op,
	})
	if err != nil {
		return nil, err
	}
	mode, metadata, err := resolveUserCreateMode(ctx, record)
	if err != nil {
		return nil, err
	}
	domain := goauth.UserToDomain(record)
	if domain != nil {
		domain.Metadata = metadata
	}

	switch mode {
	case userCreateModeDirect:
		if s.create == nil {
			return nil, goerrors.New("user create command missing", goerrors.CategoryInternal).WithCode(goerrors.CodeInternal)
		}
		result := &types.AuthUser{}
		if err := s.create.Execute(ctx.UserContext(), command.UserCreateInput{
			User:   domain,
			Status: domain.Status,
			Actor:  res.Actor,
			Scope:  res.Scope,
			Result: result,
		}); err != nil {
			return nil, err
		}
		return applyUserFieldPolicy(sanitizeUser(goauth.UserFromDomain(result)), res.Actor), nil
	case userCreateModeInvite, userCreateModeCreateInvite:
		if s.invite == nil {
			return nil, goerrors.New("user invite command missing", goerrors.CategoryInternal).WithCode(goerrors.CodeInternal)
		}
		inviteResult := &command.UserInviteResult{}
		if err := s.invite.Execute(ctx.UserContext(), command.UserInviteInput{
			Email:     domain.Email,
			Username:  domain.Username,
			FirstName: domain.FirstName,
			LastName:  domain.LastName,
			Role:      domain.Role,
			Metadata:  metadata,
			Actor:     res.Actor,
			Scope:     res.Scope,
			Result:    inviteResult,
		}); err != nil {
			return nil, err
		}
		return applyUserFieldPolicy(sanitizeUser(goauth.UserFromDomain(inviteResult.User)), res.Actor), nil
	default:
		return nil, goerrors.New("invalid create mode", goerrors.CategoryValidation).WithCode(goerrors.CodeBadRequest)
	}
}

func (s *UserService) createUsersBatch(ctx crud.Context, op crud.CrudOperation, records []*auth.User) ([]*auth.User, error) {
	created := make([]*auth.User, 0, len(records))
	for _, record := range records {
		rec, err := s.createUser(ctx, op, record)
		if err != nil {
			return nil, err
		}
		created = append(created, rec)
	}
	return created, nil
}

func (s *UserService) updateUser(ctx crud.Context, op crud.CrudOperation, record *auth.User) (*auth.User, error) {
	if record == nil {
		return nil, goerrors.New("user payload missing", goerrors.CategoryValidation).WithCode(goerrors.CodeBadRequest)
	}
	if record.ID == uuid.Nil {
		return nil, goerrors.New("user id required", goerrors.CategoryValidation).WithCode(goerrors.CodeBadRequest)
	}
	if s.update == nil {
		return nil, goerrors.New("user update command missing", goerrors.CategoryInternal).WithCode(goerrors.CodeInternal)
	}
	res, err := s.guard.Enforce(crudguard.GuardInput{
		Context:   ctx,
		Operation: op,
		TargetID:  record.ID,
	})
	if err != nil {
		return nil, err
	}
	if err := enforceUserRowAccess(res.Actor, record.ID); err != nil {
		return nil, err
	}
	domain := goauth.UserToDomain(record)
	result := &types.AuthUser{}
	if err := s.update.Execute(ctx.UserContext(), command.UserUpdateInput{
		User:   domain,
		Actor:  res.Actor,
		Scope:  res.Scope,
		Result: result,
	}); err != nil {
		return nil, err
	}
	return applyUserFieldPolicy(sanitizeUser(goauth.UserFromDomain(result)), res.Actor), nil
}

func (s *UserService) updateUsersBatch(ctx crud.Context, op crud.CrudOperation, records []*auth.User) ([]*auth.User, error) {
	updated := make([]*auth.User, 0, len(records))
	for _, record := range records {
		rec, err := s.updateUser(ctx, op, record)
		if err != nil {
			return nil, err
		}
		updated = append(updated, rec)
	}
	return updated, nil
}

func (s *UserService) deleteUser(ctx crud.Context, op crud.CrudOperation, record *auth.User) error {
	if record == nil {
		return goerrors.New("user payload missing", goerrors.CategoryValidation).WithCode(goerrors.CodeBadRequest)
	}
	if record.ID == uuid.Nil {
		return goerrors.New("user id required", goerrors.CategoryValidation).WithCode(goerrors.CodeBadRequest)
	}
	if s.lifecycle == nil {
		return goerrors.New("user lifecycle command missing", goerrors.CategoryInternal).WithCode(goerrors.CodeInternal)
	}
	res, err := s.guard.Enforce(crudguard.GuardInput{
		Context:   ctx,
		Operation: op,
		TargetID:  record.ID,
	})
	if err != nil {
		return err
	}
	if err := enforceUserRowAccess(res.Actor, record.ID); err != nil {
		return err
	}
	return s.lifecycle.Execute(ctx.UserContext(), command.UserLifecycleTransitionInput{
		UserID: record.ID,
		Target: types.LifecycleStateArchived,
		Actor:  res.Actor,
		Scope:  res.Scope,
	})
}

func (s *UserService) deleteUsersBatch(ctx crud.Context, op crud.CrudOperation, records []*auth.User) error {
	if s.bulkLifecycle == nil {
		return goerrors.New("bulk lifecycle command missing", goerrors.CategoryInternal).WithCode(goerrors.CodeInternal)
	}
	ids, err := collectUserIDs(records)
	if err != nil {
		return err
	}
	var res crudguard.GuardResult
	for i, id := range ids {
		guardRes, err := s.guard.Enforce(crudguard.GuardInput{
			Context:   ctx,
			Operation: op,
			TargetID:  id,
		})
		if err != nil {
			return err
		}
		if i == 0 {
			res = guardRes
		}
		if err := enforceUserRowAccess(guardRes.Actor, id); err != nil {
			return err
		}
	}
	return s.bulkLifecycle.Execute(ctx.UserContext(), command.BulkUserTransitionInput{
		UserIDs: ids,
		Target:  types.LifecycleStateArchived,
		Actor:   res.Actor,
		Scope:   res.Scope,
	})
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
	total := page.Total
	if res.Actor.IsSupport() || total == 0 {
		total = len(users)
	}
	records := make([]*auth.User, 0, len(users))
	for _, user := range users {
		record := sanitizeUser(goauth.UserFromDomain(&user))
		records = append(records, applyUserFieldPolicy(record, res.Actor))
	}
	return records, total, nil
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

func resolveUserCreateMode(ctx crud.Context, record *auth.User) (string, map[string]any, error) {
	mode := strings.ToLower(strings.TrimSpace(ctx.Query("mode")))
	metadata := cloneUserMetadata(record.Metadata)
	if mode == "" && len(metadata) > 0 {
		if raw, ok := metadata[userCreateModeMetadataKey]; ok {
			if str, ok := raw.(string); ok {
				mode = strings.ToLower(strings.TrimSpace(str))
			}
		}
	}
	if metadata != nil {
		delete(metadata, userCreateModeMetadataKey)
		if len(metadata) == 0 {
			metadata = nil
		}
	}
	if mode == "" {
		mode = userCreateModeDirect
	}
	switch mode {
	case userCreateModeDirect, userCreateModeInvite, userCreateModeCreateInvite:
		return mode, metadata, nil
	default:
		return "", metadata, goerrors.New("invalid create mode", goerrors.CategoryValidation).WithCode(goerrors.CodeBadRequest)
	}
}

func cloneUserMetadata(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func collectUserIDs(records []*auth.User) ([]uuid.UUID, error) {
	if len(records) == 0 {
		return nil, goerrors.New("user ids required", goerrors.CategoryValidation).WithCode(goerrors.CodeBadRequest)
	}
	ids := make([]uuid.UUID, 0, len(records))
	for _, record := range records {
		if record == nil || record.ID == uuid.Nil {
			return nil, goerrors.New("user id required", goerrors.CategoryValidation).WithCode(goerrors.CodeBadRequest)
		}
		ids = append(ids, record.ID)
	}
	return ids, nil
}
