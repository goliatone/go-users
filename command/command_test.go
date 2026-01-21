package command

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	featuregate "github.com/goliatone/go-featuregate/gate"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestUserLifecycleTransitionCommand_PolicyViolation(t *testing.T) {
	userID := uuid.New()
	repo := newFakeAuthRepo()
	repo.users[userID] = &types.AuthUser{
		ID:     userID,
		Status: types.LifecycleStateActive,
	}
	cmd := NewUserLifecycleTransitionCommand(LifecycleCommandConfig{
		Repository: repo,
		Policy:     types.DefaultTransitionPolicy(),
	})

	err := cmd.Execute(context.Background(), UserLifecycleTransitionInput{
		UserID: userID,
		Target: types.LifecycleStatePending,
		Actor: types.ActorRef{
			ID:   uuid.New(),
			Type: "admin",
		},
	})

	require.ErrorIs(t, err, types.ErrTransitionNotAllowed)
	require.False(t, repo.transitionCalled, "repo should not receive UpdateStatus when policy rejects")
}

func TestUserLifecycleTransitionCommand_MetadataAndHookOrdering(t *testing.T) {
	userID := uuid.New()
	repo := newFakeAuthRepo()
	repo.users[userID] = &types.AuthUser{
		ID:     userID,
		Status: types.LifecycleStateActive,
	}

	order := make([]string, 0, 2)
	sink := &recordingActivitySink{
		onLog: func(types.ActivityRecord) {
			order = append(order, "sink")
		},
	}
	hooks := types.Hooks{
		AfterLifecycle: func(context.Context, types.LifecycleEvent) {
			order = append(order, "hook")
		},
	}

	cmd := NewUserLifecycleTransitionCommand(LifecycleCommandConfig{
		Repository: repo,
		Policy:     types.DefaultTransitionPolicy(),
		Activity:   sink,
		Hooks:      hooks,
	})

	result := &UserLifecycleTransitionResult{}
	err := cmd.Execute(context.Background(), UserLifecycleTransitionInput{
		UserID: userID,
		Target: types.LifecycleStateSuspended,
		Actor: types.ActorRef{
			ID: uuid.New(),
		},
		Reason: "test cleanup",
		Metadata: map[string]any{
			"key": "value",
		},
		Result: result,
	})

	require.NoError(t, err)
	require.True(t, repo.transitionCalled)
	require.Equal(t, "test cleanup", repo.lastTransitionReason)
	require.Equal(t, "value", repo.lastTransitionMetadata["key"])
	require.Equal(t, []string{"sink", "hook"}, order, "activity sink must run before hook")
	require.NotNil(t, result.User)
	require.Equal(t, types.LifecycleStateSuspended, result.User.Status)
}

func TestUserPasswordResetCommand_LogsActivity(t *testing.T) {
	userID := uuid.New()
	repo := newFakeAuthRepo()
	repo.users[userID] = &types.AuthUser{
		ID:    userID,
		Email: "user@example.com",
	}

	var recorded types.ActivityRecord
	sink := &recordingActivitySink{
		onLog: func(r types.ActivityRecord) {
			recorded = r
		},
	}

	cmd := NewUserPasswordResetCommand(PasswordResetCommandConfig{
		Repository: repo,
		Activity:   sink,
	})

	expiresAt := time.Date(2024, 2, 2, 3, 4, 5, 0, time.UTC)
	err := cmd.Execute(context.Background(), UserPasswordResetInput{
		UserID:          userID,
		NewPasswordHash: "hashed-secret",
		TokenJTI:        "reset-jti",
		TokenExpiresAt:  expiresAt,
		Actor: types.ActorRef{
			ID: uuid.New(),
		},
	})

	require.NoError(t, err)
	require.Equal(t, userID, repo.lastResetUserID)
	require.Equal(t, "hashed-secret", repo.lastResetHash)
	require.Equal(t, "user.password.reset", recorded.Verb)
	require.Equal(t, userID, recorded.UserID)
	require.Equal(t, "reset-jti", recorded.Data["jti"])
	require.Equal(t, expiresAt, recorded.Data["expires_at"])
}

func TestUserInviteCommand_GeneratesTokenAndActivity(t *testing.T) {
	repo := newFakeAuthRepo()
	tokenRepo := newMemoryTokenRepo()
	manager := &stubSecureLinkManager{
		token:      "secure-link",
		expiration: time.Hour,
	}
	expectedToken := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	fixedTime := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	scope := types.ScopeFilter{
		TenantID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-000000000001"),
		OrgID:    uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-000000000002"),
	}
	var recorded types.ActivityRecord
	sink := &recordingActivitySink{
		onLog: func(r types.ActivityRecord) {
			recorded = r
		},
	}

	cmd := NewUserInviteCommand(InviteCommandConfig{
		Repository:      repo,
		TokenRepository: tokenRepo,
		SecureLinks:     manager,
		Clock:           fixedClock{t: fixedTime},
		IDGen:           fixedIDGenerator{id: expectedToken},
		Activity:        sink,
		TokenTTL:        time.Hour,
	})

	result := &UserInviteResult{}
	err := cmd.Execute(context.Background(), UserInviteInput{
		Email:    "new@example.com",
		Username: "new-user",
		Actor: types.ActorRef{
			ID: uuid.New(),
		},
		Scope:  scope,
		Result: result,
	})

	require.NoError(t, err)
	require.Equal(t, "secure-link", result.Token)
	require.NotNil(t, result.User)
	require.Equal(t, "new-user", result.User.Username)
	require.Equal(t, types.LifecycleStatePending, result.User.Status)
	require.Equal(t, SecureLinkRouteInviteAccept, manager.lastRoute)

	require.NotNil(t, tokenRepo.lastCreated)
	require.Equal(t, types.UserTokenInvite, tokenRepo.lastCreated.Type)
	require.Equal(t, expectedToken.String(), tokenRepo.lastCreated.JTI)
	require.Equal(t, fixedTime.Add(time.Hour), tokenRepo.lastCreated.ExpiresAt)

	require.Len(t, manager.lastPayloads, 1)
	payload := manager.lastPayloads[0]
	require.Equal(t, SecureLinkActionInvite, payload["action"])
	require.Equal(t, expectedToken.String(), payload["jti"])
	require.Equal(t, result.User.ID.String(), payload["user_id"])
	require.Equal(t, result.User.Email, payload["email"])
	require.Equal(t, scope.TenantID.String(), payload["tenant_id"])
	require.Equal(t, scope.OrgID.String(), payload["org_id"])
	require.Equal(t, fixedTime.Format(time.RFC3339Nano), payload["issued_at"])
	require.Equal(t, fixedTime.Add(time.Hour).Format(time.RFC3339Nano), payload["expires_at"])

	require.Equal(t, "user.invite", recorded.Verb)
	require.Equal(t, expectedToken.String(), recorded.Data["jti"])
	_, hasToken := recorded.Data["token"]
	require.False(t, hasToken)
}

func TestUserInviteCommand_FeatureGateDisabled(t *testing.T) {
	repo := newFakeAuthRepo()
	tokenRepo := newMemoryTokenRepo()
	manager := &stubSecureLinkManager{}
	gate := &stubFeatureGate{enabled: false}

	cmd := NewUserInviteCommand(InviteCommandConfig{
		Repository:      repo,
		TokenRepository: tokenRepo,
		SecureLinks:     manager,
		FeatureGate:     gate,
	})

	err := cmd.Execute(context.Background(), UserInviteInput{
		Email: "new@example.com",
		Actor: types.ActorRef{
			ID: uuid.New(),
		},
	})

	require.ErrorIs(t, err, ErrInviteDisabled)
	require.Nil(t, repo.lastCreated)
	require.Nil(t, tokenRepo.lastCreated)
	require.Equal(t, []string{featureUsersInvite}, gate.keys)
}

func TestUserPasswordResetRequestCommand_IssuesTokenAndLogsActivity(t *testing.T) {
	userID := uuid.New()
	repo := newFakeAuthRepo()
	repo.users[userID] = &types.AuthUser{
		ID:       userID,
		Email:    "reset@example.com",
		Username: "reset-user",
	}
	resetRepo := newMemoryResetRepo()
	manager := &stubSecureLinkManager{
		token:      "reset-link",
		expiration: time.Hour,
	}
	fixedTime := time.Date(2024, 6, 7, 8, 9, 10, 0, time.UTC)
	expectedToken := uuid.MustParse("bbbbbbbb-cccc-dddd-eeee-ffffffffffff")
	scope := types.ScopeFilter{
		TenantID: uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-000000000003"),
		OrgID:    uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-000000000004"),
	}
	var recorded types.ActivityRecord
	sink := &recordingActivitySink{
		onLog: func(r types.ActivityRecord) {
			recorded = r
		},
	}

	cmd := NewUserPasswordResetRequestCommand(PasswordResetRequestConfig{
		Repository:      repo,
		ResetRepository: resetRepo,
		SecureLinks:     manager,
		Clock:           fixedClock{t: fixedTime},
		IDGen:           fixedIDGenerator{id: expectedToken},
		Activity:        sink,
		TokenTTL:        time.Hour,
	})

	result := &UserPasswordResetRequestResult{}
	err := cmd.Execute(context.Background(), UserPasswordResetRequestInput{
		Identifier: "reset@example.com",
		Scope:      scope,
		Result:     result,
	})

	require.NoError(t, err)
	require.Equal(t, "reset-link", result.Token)
	require.NotNil(t, result.User)
	require.Equal(t, userID, result.User.ID)

	require.Len(t, manager.lastPayloads, 1)
	payload := manager.lastPayloads[0]
	require.Equal(t, SecureLinkActionPasswordReset, payload["action"])
	require.Equal(t, expectedToken.String(), payload["jti"])
	require.Equal(t, userID.String(), payload["user_id"])
	require.Equal(t, "reset@example.com", payload["email"])
	require.Equal(t, scope.TenantID.String(), payload["tenant_id"])
	require.Equal(t, scope.OrgID.String(), payload["org_id"])
	require.Equal(t, fixedTime.Format(time.RFC3339Nano), payload["issued_at"])
	require.Equal(t, fixedTime.Add(time.Hour).Format(time.RFC3339Nano), payload["expires_at"])

	resetRecord := resetRepo.resets[expectedToken.String()]
	require.NotNil(t, resetRecord)
	require.Equal(t, types.PasswordResetStatusRequested, resetRecord.Status)
	require.Equal(t, fixedTime, resetRecord.IssuedAt)
	require.Equal(t, fixedTime.Add(time.Hour), resetRecord.ExpiresAt)

	require.Equal(t, "user.password.reset.requested", recorded.Verb)
	require.Equal(t, expectedToken.String(), recorded.Data["jti"])
	_, hasToken := recorded.Data["token"]
	require.False(t, hasToken)
}

func TestUserPasswordResetRequestCommand_FeatureGateDisabled(t *testing.T) {
	userID := uuid.New()
	repo := newFakeAuthRepo()
	repo.users[userID] = &types.AuthUser{
		ID:    userID,
		Email: "reset@example.com",
	}
	resetRepo := newMemoryResetRepo()
	manager := &stubSecureLinkManager{}
	gate := &stubFeatureGate{enabled: false}

	cmd := NewUserPasswordResetRequestCommand(PasswordResetRequestConfig{
		Repository:      repo,
		ResetRepository: resetRepo,
		SecureLinks:     manager,
		FeatureGate:     gate,
	})

	err := cmd.Execute(context.Background(), UserPasswordResetRequestInput{
		Identifier: "reset@example.com",
	})

	require.ErrorIs(t, err, ErrPasswordResetDisabled)
	require.Len(t, resetRepo.resets, 0)
	require.Equal(t, []string{featureUsersPasswordReset}, gate.keys)
}

func TestUserRegistrationRequestCommand_FeatureGateDisabled(t *testing.T) {
	repo := newFakeAuthRepo()
	tokenRepo := newMemoryTokenRepo()
	manager := &stubSecureLinkManager{}
	gate := &stubFeatureGate{enabled: false}

	cmd := NewUserRegistrationRequestCommand(RegistrationRequestConfig{
		Repository:      repo,
		TokenRepository: tokenRepo,
		SecureLinks:     manager,
		FeatureGate:     gate,
	})

	err := cmd.Execute(context.Background(), UserRegistrationRequestInput{
		Email: "register@example.com",
	})

	require.ErrorIs(t, err, ErrSignupDisabled)
	require.Nil(t, repo.lastCreated)
	require.Nil(t, tokenRepo.lastCreated)
	require.Equal(t, []string{featuregate.FeatureUsersSignup}, gate.keys)
}

func TestUserTokenConsumeCommand_PreventsReplayAndLogsActivity(t *testing.T) {
	userID := uuid.New()
	issuedAt := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)
	tokenRepo := newMemoryTokenRepo()
	_, err := tokenRepo.CreateToken(context.Background(), types.UserToken{
		UserID:    userID,
		Type:      types.UserTokenInvite,
		JTI:       "token-jti",
		Status:    types.UserTokenStatusIssued,
		IssuedAt:  issuedAt,
		ExpiresAt: issuedAt.Add(time.Hour),
	})
	require.NoError(t, err)

	manager := &stubSecureLinkManager{
		validatePayload: types.SecureLinkPayload{
			"jti":        "token-jti",
			"user_id":    userID.String(),
			"expires_at": issuedAt.Add(time.Hour).Format(time.RFC3339Nano),
			"email":      "invitee@example.com",
		},
	}
	var recorded types.ActivityRecord
	sink := &recordingActivitySink{
		onLog: func(r types.ActivityRecord) {
			recorded = r
		},
	}

	cmd := NewUserTokenConsumeCommand(TokenConsumeConfig{
		TokenRepository: tokenRepo,
		SecureLinks:     manager,
		Clock:           fixedClock{t: issuedAt},
		Activity:        sink,
	})

	err = cmd.Execute(context.Background(), UserTokenConsumeInput{
		Token:     "secure-token",
		TokenType: types.UserTokenInvite,
	})
	require.NoError(t, err)
	require.Equal(t, types.UserTokenStatusUsed, tokenRepo.tokens[tokenKey(types.UserTokenInvite, "token-jti")].Status)
	require.Equal(t, "user.invite.consumed", recorded.Verb)
	_, hasToken := recorded.Data["token"]
	require.False(t, hasToken)

	err = cmd.Execute(context.Background(), UserTokenConsumeInput{
		Token:     "secure-token",
		TokenType: types.UserTokenInvite,
	})
	require.ErrorIs(t, err, ErrTokenAlreadyUsed)
}

func TestUserPasswordResetConfirmCommand_ConsumesToken(t *testing.T) {
	userID := uuid.New()
	repo := newFakeAuthRepo()
	repo.users[userID] = &types.AuthUser{
		ID:    userID,
		Email: "user@example.com",
	}
	resetRepo := newMemoryResetRepo()
	issuedAt := time.Date(2024, 4, 5, 6, 7, 8, 0, time.UTC)
	_, err := resetRepo.CreateReset(context.Background(), types.PasswordResetRecord{
		UserID:    userID,
		Email:     "user@example.com",
		Status:    types.PasswordResetStatusRequested,
		JTI:       "reset-jti",
		IssuedAt:  issuedAt,
		ExpiresAt: issuedAt.Add(time.Hour),
	})
	require.NoError(t, err)

	manager := &stubSecureLinkManager{
		validatePayload: types.SecureLinkPayload{
			"jti":        "reset-jti",
			"user_id":    userID.String(),
			"expires_at": issuedAt.Add(time.Hour).Format(time.RFC3339Nano),
		},
	}
	resetCmd := NewUserPasswordResetCommand(PasswordResetCommandConfig{
		Repository: repo,
	})
	confirmCmd := NewUserPasswordResetConfirmCommand(PasswordResetConfirmConfig{
		ResetRepository: resetRepo,
		SecureLinks:     manager,
		ResetCommand:    resetCmd,
		Clock:           fixedClock{t: issuedAt},
	})

	err = confirmCmd.Execute(context.Background(), UserPasswordResetConfirmInput{
		Token:           "reset-token",
		NewPasswordHash: "hashed-reset",
	})
	require.NoError(t, err)
	require.Equal(t, types.PasswordResetStatusChanged, resetRepo.resets["reset-jti"].Status)
	require.Equal(t, userID, repo.lastResetUserID)
}

func TestUserCreateCommand_DefaultsStatusAndLogsActivity(t *testing.T) {
	repo := newFakeAuthRepo()
	var recorded types.ActivityRecord
	sink := &recordingActivitySink{
		onLog: func(r types.ActivityRecord) {
			recorded = r
		},
	}

	cmd := NewUserCreateCommand(UserCreateCommandConfig{
		Repository: repo,
		Activity:   sink,
	})

	result := &types.AuthUser{}
	err := cmd.Execute(context.Background(), UserCreateInput{
		User: &types.AuthUser{
			Email: "create@example.com",
			Role:  "member",
		},
		Actor: types.ActorRef{
			ID: uuid.New(),
		},
		Result: result,
	})

	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, result.ID)
	require.Equal(t, types.LifecycleStateActive, result.Status)
	require.Equal(t, "user.created", recorded.Verb)
	require.Equal(t, result.ID, recorded.UserID)
}

func TestUserUpdateCommand_LogsActivity(t *testing.T) {
	userID := uuid.New()
	repo := newFakeAuthRepo()
	repo.users[userID] = &types.AuthUser{
		ID:    userID,
		Email: "before@example.com",
	}
	var recorded types.ActivityRecord
	sink := &recordingActivitySink{
		onLog: func(r types.ActivityRecord) {
			recorded = r
		},
	}

	cmd := NewUserUpdateCommand(UserUpdateCommandConfig{
		Repository: repo,
		Activity:   sink,
	})

	result := &types.AuthUser{}
	err := cmd.Execute(context.Background(), UserUpdateInput{
		User: &types.AuthUser{
			ID:    userID,
			Email: "after@example.com",
		},
		Actor: types.ActorRef{
			ID: uuid.New(),
		},
		Result: result,
	})

	require.NoError(t, err)
	require.Equal(t, "after@example.com", result.Email)
	require.Equal(t, "user.updated", recorded.Verb)
	require.Equal(t, userID, recorded.UserID)
}

func TestUserUpdateCommand_EnforcesLifecyclePolicy(t *testing.T) {
	userID := uuid.New()
	repo := newFakeAuthRepo()
	repo.users[userID] = &types.AuthUser{
		ID:     userID,
		Email:  "pending@example.com",
		Status: types.LifecycleStatePending,
	}

	cmd := NewUserUpdateCommand(UserUpdateCommandConfig{
		Repository: repo,
		Policy:     types.DefaultTransitionPolicy(),
	})

	err := cmd.Execute(context.Background(), UserUpdateInput{
		User: &types.AuthUser{
			ID:     userID,
			Email:  "pending@example.com",
			Status: types.LifecycleStateArchived,
		},
		Actor: types.ActorRef{
			ID: uuid.New(),
		},
	})

	require.ErrorIs(t, err, types.ErrTransitionNotAllowed)
	require.Nil(t, repo.lastUpdated)
}

func TestActivityLogCommand_LogsRecord(t *testing.T) {
	sink := &recordingActivitySink{}
	cmd := NewActivityLogCommand(ActivityLogConfig{
		Sink: sink,
	})

	err := cmd.Execute(context.Background(), ActivityLogInput{
		Record: types.ActivityRecord{
			Verb: "demo.event",
		},
	})

	require.NoError(t, err)
	require.Len(t, sink.records, 1)
	require.Equal(t, "demo.event", sink.records[0].Verb)
}

func TestActivityLogCommand_RequiresVerb(t *testing.T) {
	sink := &recordingActivitySink{}
	cmd := NewActivityLogCommand(ActivityLogConfig{Sink: sink})

	err := cmd.Execute(context.Background(), ActivityLogInput{
		Record: types.ActivityRecord{},
	})

	require.ErrorIs(t, err, ErrActivityVerbRequired)
}

func TestProfileUpsertCommand_EmitsHook(t *testing.T) {
	repo := &fakeProfileRepo{}
	var event types.ProfileEvent
	cmd := NewProfileUpsertCommand(ProfileCommandConfig{
		Repository: repo,
		Hooks: types.Hooks{
			AfterProfileChange: func(_ context.Context, e types.ProfileEvent) {
				event = e
			},
		},
	})

	display := "New Name"
	err := cmd.Execute(context.Background(), ProfileUpsertInput{
		UserID: uuid.New(),
		Patch: types.ProfilePatch{
			DisplayName: &display,
		},
		Actor: types.ActorRef{ID: uuid.New()},
	})

	require.NoError(t, err)
	require.NotNil(t, repo.stored)
	require.Equal(t, "New Name", repo.stored.DisplayName)
	require.Equal(t, "New Name", event.Profile.DisplayName)
}

func TestPreferenceCommands_LogEvents(t *testing.T) {
	repo := &fakePreferenceRepo{}
	var events []types.PreferenceEvent
	hooks := types.Hooks{
		AfterPreferenceChange: func(_ context.Context, e types.PreferenceEvent) {
			events = append(events, e)
		},
	}

	upsert := NewPreferenceUpsertCommand(PreferenceCommandConfig{
		Repository: repo,
		Hooks:      hooks,
	})
	key := "notifications.email"
	err := upsert.Execute(context.Background(), PreferenceUpsertInput{
		UserID: uuid.New(),
		Scope: types.ScopeFilter{
			TenantID: uuid.New(),
		},
		Level: types.PreferenceLevelUser,
		Key:   key,
		Value: map[string]any{"enabled": true},
		Actor: types.ActorRef{ID: uuid.New()},
	})
	require.NoError(t, err)
	require.Len(t, repo.upserts, 1)

	del := NewPreferenceDeleteCommand(PreferenceCommandConfig{
		Repository: repo,
		Hooks:      hooks,
	})
	err = del.Execute(context.Background(), PreferenceDeleteInput{
		UserID: repo.upserts[0].UserID,
		Scope:  repo.upserts[0].Scope,
		Level:  types.PreferenceLevelUser,
		Key:    key,
		Actor:  types.ActorRef{ID: uuid.New()},
	})
	require.NoError(t, err)
	require.Len(t, repo.deleted, 1)
	require.Len(t, events, 2)
	require.Equal(t, "preference.delete", events[len(events)-1].Action)
}

// --- Test helpers ---

type fakeAuthRepo struct {
	users                  map[uuid.UUID]*types.AuthUser
	transitionCalled       bool
	lastTransitionReason   string
	lastTransitionMetadata map[string]any
	lastResetUserID        uuid.UUID
	lastResetHash          string
	lastCreated            *types.AuthUser
	lastUpdated            *types.AuthUser
}

func newFakeAuthRepo() *fakeAuthRepo {
	return &fakeAuthRepo{
		users: make(map[uuid.UUID]*types.AuthUser),
	}
}

func (f *fakeAuthRepo) GetByID(_ context.Context, id uuid.UUID) (*types.AuthUser, error) {
	user, ok := f.users[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return user, nil
}

func (f *fakeAuthRepo) GetByIdentifier(_ context.Context, identifier string) (*types.AuthUser, error) {
	needle := strings.TrimSpace(identifier)
	if needle == "" {
		return nil, errors.New("not found")
	}
	for _, user := range f.users {
		if strings.EqualFold(strings.TrimSpace(user.Email), needle) ||
			strings.EqualFold(strings.TrimSpace(user.Username), needle) {
			return user, nil
		}
	}
	return nil, errors.New("not found")
}

func (f *fakeAuthRepo) Create(_ context.Context, input *types.AuthUser) (*types.AuthUser, error) {
	if input.ID == uuid.Nil {
		input.ID = uuid.New()
	}
	f.lastCreated = input
	f.users[input.ID] = input
	return input, nil
}

func (f *fakeAuthRepo) Update(_ context.Context, input *types.AuthUser) (*types.AuthUser, error) {
	f.lastUpdated = input
	f.users[input.ID] = input
	return input, nil
}

func (f *fakeAuthRepo) UpdateStatus(_ context.Context, actor types.ActorRef, id uuid.UUID, next types.LifecycleState, opts ...types.TransitionOption) (*types.AuthUser, error) {
	f.transitionCalled = true
	cfg := extractTransitionConfig(opts...)
	f.lastTransitionReason = cfg.Reason
	f.lastTransitionMetadata = cfg.Metadata

	user, ok := f.users[id]
	if !ok {
		return nil, errors.New("not found")
	}
	user.Status = next
	_ = actor
	return user, nil
}

func (f *fakeAuthRepo) AllowedTransitions(context.Context, uuid.UUID) ([]types.LifecycleTransition, error) {
	return nil, nil
}

func (f *fakeAuthRepo) ResetPassword(_ context.Context, id uuid.UUID, hash string) error {
	f.lastResetUserID = id
	f.lastResetHash = hash
	return nil
}

type recordingActivitySink struct {
	onLog   func(types.ActivityRecord)
	records []types.ActivityRecord
}

func (r *recordingActivitySink) Log(_ context.Context, record types.ActivityRecord) error {
	r.records = append(r.records, record)
	if r.onLog != nil {
		r.onLog(record)
	}
	return nil
}

func extractTransitionConfig(opts ...types.TransitionOption) types.TransitionConfig {
	cfg := types.TransitionConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

type fakeProfileRepo struct {
	stored *types.UserProfile
}

func (f *fakeProfileRepo) GetProfile(context.Context, uuid.UUID, types.ScopeFilter) (*types.UserProfile, error) {
	if f.stored == nil {
		return nil, nil
	}
	profile := *f.stored
	return &profile, nil
}

func (f *fakeProfileRepo) UpsertProfile(_ context.Context, profile types.UserProfile) (*types.UserProfile, error) {
	copy := profile
	f.stored = &copy
	return &copy, nil
}

type fakePreferenceRepo struct {
	upserts []types.PreferenceRecord
	deleted []struct {
		level types.PreferenceLevel
		key   string
	}
}

func (f *fakePreferenceRepo) ListPreferences(context.Context, types.PreferenceFilter) ([]types.PreferenceRecord, error) {
	return nil, nil
}

func (f *fakePreferenceRepo) UpsertPreference(_ context.Context, record types.PreferenceRecord) (*types.PreferenceRecord, error) {
	copy := record
	copy.ID = uuid.New()
	f.upserts = append(f.upserts, copy)
	return &copy, nil
}

func (f *fakePreferenceRepo) DeletePreference(_ context.Context, _ uuid.UUID, _ types.ScopeFilter, level types.PreferenceLevel, key string) error {
	f.deleted = append(f.deleted, struct {
		level types.PreferenceLevel
		key   string
	}{level: level, key: key})
	return nil
}

type fixedIDGenerator struct {
	id uuid.UUID
}

func (f fixedIDGenerator) UUID() uuid.UUID {
	return f.id
}

type fixedClock struct {
	t time.Time
}

func (f fixedClock) Now() time.Time {
	return f.t
}

type stubSecureLinkManager struct {
	token           string
	expiration      time.Duration
	lastRoute       string
	lastPayloads    []types.SecureLinkPayload
	validatePayload types.SecureLinkPayload
}

func (s *stubSecureLinkManager) Generate(route string, payloads ...types.SecureLinkPayload) (string, error) {
	s.lastRoute = route
	s.lastPayloads = payloads
	if s.token == "" {
		return "token", nil
	}
	return s.token, nil
}

func (s *stubSecureLinkManager) Validate(string) (map[string]any, error) {
	if s.validatePayload == nil {
		return map[string]any{}, nil
	}
	return map[string]any(s.validatePayload), nil
}

func (s *stubSecureLinkManager) GetAndValidate(fn func(string) string) (types.SecureLinkPayload, error) {
	return s.validatePayload, nil
}

func (s *stubSecureLinkManager) GetExpiration() time.Duration {
	return s.expiration
}

type stubFeatureGate struct {
	enabled bool
	err     error
	keys    []string
}

func (s *stubFeatureGate) Enabled(_ context.Context, key string, _ ...featuregate.ResolveOption) (bool, error) {
	s.keys = append(s.keys, key)
	if s.err != nil {
		return false, s.err
	}
	return s.enabled, nil
}

type memoryTokenRepo struct {
	tokens      map[string]*types.UserToken
	lastCreated *types.UserToken
}

func newMemoryTokenRepo() *memoryTokenRepo {
	return &memoryTokenRepo{tokens: map[string]*types.UserToken{}}
}

func (m *memoryTokenRepo) CreateToken(_ context.Context, token types.UserToken) (*types.UserToken, error) {
	copy := token
	if copy.ID == uuid.Nil {
		copy.ID = uuid.New()
	}
	m.tokens[tokenKey(copy.Type, copy.JTI)] = &copy
	m.lastCreated = &copy
	return &copy, nil
}

func (m *memoryTokenRepo) GetTokenByJTI(_ context.Context, tokenType types.UserTokenType, jti string) (*types.UserToken, error) {
	if token, ok := m.tokens[tokenKey(tokenType, jti)]; ok {
		return token, nil
	}
	return nil, errors.New("not found")
}

func (m *memoryTokenRepo) UpdateTokenStatus(_ context.Context, tokenType types.UserTokenType, jti string, status types.UserTokenStatus, usedAt time.Time) error {
	token, ok := m.tokens[tokenKey(tokenType, jti)]
	if !ok {
		return errors.New("not found")
	}
	token.Status = status
	if !usedAt.IsZero() {
		token.UsedAt = usedAt
	}
	return nil
}

func tokenKey(tokenType types.UserTokenType, jti string) string {
	return string(tokenType) + ":" + jti
}

type memoryResetRepo struct {
	resets map[string]*types.PasswordResetRecord
}

func newMemoryResetRepo() *memoryResetRepo {
	return &memoryResetRepo{resets: map[string]*types.PasswordResetRecord{}}
}

func (m *memoryResetRepo) CreateReset(_ context.Context, record types.PasswordResetRecord) (*types.PasswordResetRecord, error) {
	copy := record
	if copy.ID == uuid.Nil {
		copy.ID = uuid.New()
	}
	m.resets[copy.JTI] = &copy
	return &copy, nil
}

func (m *memoryResetRepo) GetResetByJTI(_ context.Context, jti string) (*types.PasswordResetRecord, error) {
	if rec, ok := m.resets[jti]; ok {
		return rec, nil
	}
	return nil, errors.New("not found")
}

func (m *memoryResetRepo) ConsumeReset(_ context.Context, jti string, usedAt time.Time) error {
	rec, ok := m.resets[jti]
	if !ok {
		return errors.New("not found")
	}
	if rec.Status == types.PasswordResetStatusExpired || rec.Status == types.PasswordResetStatusChanged || !rec.UsedAt.IsZero() {
		return errors.New("already used")
	}
	if usedAt.IsZero() {
		usedAt = time.Now().UTC()
	}
	rec.UsedAt = usedAt
	return nil
}

func (m *memoryResetRepo) UpdateResetStatus(_ context.Context, jti string, status types.PasswordResetStatus, usedAt time.Time) error {
	rec, ok := m.resets[jti]
	if !ok {
		return errors.New("not found")
	}
	rec.Status = status
	if !usedAt.IsZero() {
		rec.UsedAt = usedAt
		rec.ResetAt = usedAt
	}
	return nil
}
