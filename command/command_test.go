package command

import (
	"context"
	"errors"
	"testing"
	"time"

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

	err := cmd.Execute(context.Background(), UserPasswordResetInput{
		UserID:          userID,
		NewPasswordHash: "hashed-secret",
		Actor: types.ActorRef{
			ID: uuid.New(),
		},
	})

	require.NoError(t, err)
	require.Equal(t, userID, repo.lastResetUserID)
	require.Equal(t, "hashed-secret", repo.lastResetHash)
	require.Equal(t, "user.password.reset", recorded.Verb)
	require.Equal(t, userID, recorded.UserID)
}

func TestUserInviteCommand_GeneratesTokenAndActivity(t *testing.T) {
	repo := newFakeAuthRepo()
	expectedToken := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	fixedTime := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	var recorded types.ActivityRecord
	sink := &recordingActivitySink{
		onLog: func(r types.ActivityRecord) {
			recorded = r
		},
	}

	cmd := NewUserInviteCommand(InviteCommandConfig{
		Repository: repo,
		Clock:      fixedClock{t: fixedTime},
		IDGen:      fixedIDGenerator{id: expectedToken},
		Activity:   sink,
		TokenTTL:   time.Hour,
	})

	result := &UserInviteResult{}
	err := cmd.Execute(context.Background(), UserInviteInput{
		Email:    "new@example.com",
		Username: "new-user",
		Actor: types.ActorRef{
			ID: uuid.New(),
		},
		Result: result,
	})

	require.NoError(t, err)
	require.Equal(t, expectedToken.String(), result.Token)
	require.Equal(t, "new@example.com", recorded.Data["email"])
	require.Equal(t, "user.invite", recorded.Verb)
	require.NotNil(t, result.User)
	require.Equal(t, "new-user", result.User.Username)
	require.Equal(t, types.LifecycleStatePending, result.User.Status)
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

func (f *fakeAuthRepo) GetByIdentifier(context.Context, string) (*types.AuthUser, error) {
	return nil, errors.New("not implemented")
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
