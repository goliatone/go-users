package goauth

import (
	"testing"
	"time"

	auth "github.com/goliatone/go-auth"
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

func TestMergeAuthUserUpdatePreservesCurrentPasswordHash(t *testing.T) {
	userID := uuid.New()
	current := &auth.User{
		ID:           userID,
		Email:        "before@example.com",
		PasswordHash: "existing-hash",
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
	require.Equal(t, "existing-hash", record.PasswordHash)
}
