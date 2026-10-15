package command

import (
	"context"
	"errors"
	"testing"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type failingMutationActivity struct{ err error }

func (s failingMutationActivity) Log(context.Context, types.ActivityRecord) error { return s.err }

type mutationFailureLogger struct {
	types.NopLogger
	err    error
	fields []any
	calls  int
}

func (l *mutationFailureLogger) Error(_ string, err error, fields ...any) {
	l.err = err
	l.fields = fields
	l.calls++
}

func TestLifecycleBestEffortActivityFailureIsObservable(t *testing.T) {
	repo := newFakeAuthRepo()
	user := uuid.New()
	repo.users[user] = &types.AuthUser{ID: user, Status: types.LifecycleStateActive}
	sinkErr := errors.New("audit sink unavailable")
	logger := &mutationFailureLogger{}
	hooks := 0
	cmd := NewUserLifecycleTransitionCommand(LifecycleCommandConfig{Repository: repo, Activity: failingMutationActivity{sinkErr}, Logger: logger, Hooks: types.Hooks{AfterActivity: func(context.Context, types.ActivityRecord) { hooks++ }}})
	output := &UserLifecycleTransitionResult{}
	err := cmd.Execute(context.Background(), UserLifecycleTransitionInput{UserID: user, Actor: types.ActorRef{ID: uuid.New()}, Target: types.LifecycleStateSuspended, Metadata: map[string]any{"password": "must not log"}, Result: output})
	require.NoError(t, err, "legacy best-effort failure must not claim the mutation rolled back")
	require.Equal(t, types.LifecycleStateSuspended, repo.users[user].Status)
	require.Equal(t, types.LifecycleStateSuspended, output.User.Status)
	require.ErrorIs(t, logger.err, sinkErr)
	require.Equal(t, 1, logger.calls)
	require.Equal(t, []any{"verb", "user.lifecycle.transition", "object_type", "user"}, logger.fields)
	require.Equal(t, 1, hooks, "legacy post-write notifications remain compatible")
}
