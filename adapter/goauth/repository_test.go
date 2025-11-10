package goauth

import (
	"testing"
	"time"

	auth "github.com/goliatone/go-auth"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
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
	if result == nil {
		t.Fatalf("expected user to be converted")
	}
	if result.Email != user.Email || result.Username != user.Username {
		t.Fatalf("expected email/username to be copied")
	}
	if result.Status != types.LifecycleState(user.Status) {
		t.Fatalf("expected status to match")
	}
	if result.Metadata["foo"] != "bar" {
		t.Fatalf("expected metadata to be copied")
	}
	if result.Raw != user {
		t.Fatalf("expected raw pointer to be preserved")
	}
}

func TestBuildGoAuthOptions(t *testing.T) {
	opts := buildGoAuthOptions(types.TransitionConfig{
		Reason: "maintenance",
		Metadata: map[string]any{
			"foo": "bar",
		},
		Force: true,
	})
	if len(opts) != 3 {
		t.Fatalf("expected 3 transition options, got %d", len(opts))
	}
}
