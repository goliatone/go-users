package goauth

import (
	"context"
	"testing"
	"time"

	auth "github.com/goliatone/go-auth"
	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestToAuthUser(t *testing.T) {
	now := time.Now()
	user := &auth.User{
		ID:        uuid.New(),
		Role:      auth.UserRole("admin"),
		Status:    auth.UserStatus("active"),
		Email:     "test@example.com",
		Username:  "tester",
		FirstName: "Test",
		LastName:  "Er",
		Metadata:  map[string]any{"foo": "bar"},
		CreatedAt: &now,
		UpdatedAt: &now,
	}

	result := toAuthUser(user)
	require.NotNil(t, result)
	require.Equal(t, user.Email, result.Email)
	require.Equal(t, user.Username, result.Username)
	require.Equal(t, types.LifecycleState(user.Status), result.Status)
	require.Equal(t, "bar", result.Metadata["foo"])
	require.Same(t, user, result.Raw)
}

func TestBuildGoAuthOptions(t *testing.T) {
	opts := buildGoAuthOptions(types.TransitionConfig{
		Reason: "maintenance",
		Metadata: map[string]any{
			"foo": "bar",
		},
		Force: true,
	})
	require.Len(t, opts, 3)
}

func TestMergeAuthUserUpdatePreservesAuthManagedFields(t *testing.T) {
	userID := uuid.New()
	now := time.Now()
	current := &auth.User{
		ID:                 userID,
		Email:              "before@example.com",
		ExternalID:         "external-1",
		ExternalIDProvider: "auth0",
		Phone:              "+15551234567",
		PasswordHash:       "existing-hash",
		EmailValidated:     true,
		LoginAttempts:      2,
		LoginAttemptAt:     &now,
		LoggedInAt:         &now,
		ResetedAt:          &now,
	}
	input := &types.AuthUser{
		ID:       userID,
		Email:    "after@example.com",
		Username: "after",
		Raw: &auth.User{
			ID:           userID,
			Email:        "after@example.com",
			PasswordHash: "",
		},
	}

	record := mergeAuthUserUpdate(input, current)

	require.NotNil(t, record)
	require.Equal(t, "after@example.com", record.Email)
	require.Equal(t, "after", record.Username)
	require.Equal(t, "external-1", record.ExternalID)
	require.Equal(t, "auth0", record.ExternalIDProvider)
	require.Equal(t, "+15551234567", record.Phone)
	require.Equal(t, "existing-hash", record.PasswordHash)
	require.True(t, record.EmailValidated)
	require.Equal(t, 2, record.LoginAttempts)
	require.Same(t, current.LoginAttemptAt, record.LoginAttemptAt)
	require.Same(t, current.LoggedInAt, record.LoggedInAt)
	require.Same(t, current.ResetedAt, record.ResetedAt)
}

func TestUsersAdapterResetPasswordAndClearTemporaryPasswordFallsBackForNonTemporaryUser(t *testing.T) {
	userID := uuid.New()
	repo := &legacyGoAuthUsersRepo{
		user: &auth.User{
			ID:    userID,
			Email: "user@example.com",
		},
	}
	adapter := NewUsersAdapter(repo)

	err := adapter.ResetPasswordAndClearTemporaryPassword(context.Background(), userID, "new-hash")

	require.NoError(t, err)
	require.True(t, repo.resetPasswordCalled)
	require.Equal(t, userID, repo.lastResetID)
	require.Equal(t, "new-hash", repo.lastResetHash)
}

func TestUsersAdapterResetPasswordAndClearTemporaryPasswordRejectsTemporaryUserWithoutAtomicCleanup(t *testing.T) {
	userID := uuid.New()
	issuedAt := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)
	repo := &legacyGoAuthUsersRepo{
		user: &auth.User{
			ID:       userID,
			Email:    "user@example.com",
			Metadata: types.MarkTemporaryPasswordMetadata(nil, issuedAt, issuedAt.Add(time.Hour)),
		},
	}
	adapter := NewUsersAdapter(repo)

	err := adapter.ResetPasswordAndClearTemporaryPassword(context.Background(), userID, "new-hash")

	require.ErrorIs(t, err, types.ErrTemporaryPasswordResetUnsupported)
	require.False(t, repo.resetPasswordCalled)
}

type legacyGoAuthUsersRepo struct {
	auth.Users
	user                *auth.User
	resetPasswordCalled bool
	lastResetID         uuid.UUID
	lastResetHash       string
}

func (r *legacyGoAuthUsersRepo) GetByID(_ context.Context, id string, _ ...repository.SelectCriteria) (*auth.User, error) {
	if r.user == nil || r.user.ID.String() != id {
		return nil, repository.NewRecordNotFound()
	}
	return r.user, nil
}

func (r *legacyGoAuthUsersRepo) ResetPassword(_ context.Context, id uuid.UUID, passwordHash string) error {
	r.resetPasswordCalled = true
	r.lastResetID = id
	r.lastResetHash = passwordHash
	if r.user == nil || r.user.ID != id {
		return repository.NewRecordNotFound()
	}
	r.user.PasswordHash = passwordHash
	return nil
}
