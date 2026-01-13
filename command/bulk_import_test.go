package command

import (
	"context"
	"errors"
	"testing"

	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestBulkUserImportCommand_RequiresUsers(t *testing.T) {
	repo := newFakeAuthRepo()
	create := NewUserCreateCommand(UserCreateCommandConfig{Repository: repo})
	cmd := NewBulkUserImportCommand(create)

	err := cmd.Execute(context.Background(), BulkUserImportInput{
		Actor: types.ActorRef{ID: uuid.New()},
	})

	require.ErrorIs(t, err, ErrUsersRequired)
}

func TestBulkUserImportCommand_RequiresActor(t *testing.T) {
	repo := newFakeAuthRepo()
	create := NewUserCreateCommand(UserCreateCommandConfig{Repository: repo})
	cmd := NewBulkUserImportCommand(create)

	err := cmd.Execute(context.Background(), BulkUserImportInput{
		Users: []*types.AuthUser{
			{Email: "user@example.com"},
		},
	})

	require.ErrorIs(t, err, ErrActorRequired)
}

func TestBulkUserImportCommand_CreatesMultipleUsersAndRespectsDefaultStatus(t *testing.T) {
	repo := newFakeAuthRepo()
	create := NewUserCreateCommand(UserCreateCommandConfig{Repository: repo})
	cmd := NewBulkUserImportCommand(create)

	results := []BulkUserImportResult{}
	err := cmd.Execute(context.Background(), BulkUserImportInput{
		Users: []*types.AuthUser{
			{Email: "default@example.com"},
			{Email: "explicit@example.com", Status: types.LifecycleStateDisabled},
		},
		DefaultStatus: types.LifecycleStateSuspended,
		Actor:         types.ActorRef{ID: uuid.New()},
		Results:       &results,
	})

	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, types.LifecycleStateSuspended, results[0].Status)
	require.Equal(t, types.LifecycleStateDisabled, results[1].Status)
	require.Len(t, repo.users, 2)
	require.NotEqual(t, uuid.Nil, results[0].UserID)
	require.NotEqual(t, uuid.Nil, results[1].UserID)
}

func TestBulkUserImportCommand_StopsOnErrorByDefault(t *testing.T) {
	repo := newFakeAuthRepo()
	create := NewUserCreateCommand(UserCreateCommandConfig{Repository: repo})
	cmd := NewBulkUserImportCommand(create)

	results := []BulkUserImportResult{}
	err := cmd.Execute(context.Background(), BulkUserImportInput{
		Users: []*types.AuthUser{
			{Email: ""},
			{Email: "valid@example.com"},
		},
		Actor:   types.ActorRef{ID: uuid.New()},
		Results: &results,
	})

	require.Error(t, err)
	require.Len(t, results, 1)
	require.Len(t, repo.users, 0)
	require.NotNil(t, results[0].Err)
	var richErr *goerrors.Error
	require.True(t, errors.As(results[0].Err, &richErr))
	require.Equal(t, 0, richErr.Metadata["index"])
}

func TestBulkUserImportCommand_ContinuesOnErrorWhenEnabled(t *testing.T) {
	repo := newFakeAuthRepo()
	create := NewUserCreateCommand(UserCreateCommandConfig{Repository: repo})
	cmd := NewBulkUserImportCommand(create)

	results := []BulkUserImportResult{}
	err := cmd.Execute(context.Background(), BulkUserImportInput{
		Users: []*types.AuthUser{
			{Email: ""},
			{Email: "valid@example.com"},
		},
		Actor:           types.ActorRef{ID: uuid.New()},
		ContinueOnError: true,
		Results:         &results,
	})

	require.Error(t, err)
	require.Len(t, results, 2)
	require.Len(t, repo.users, 1)
	require.NotNil(t, results[0].Err)
	require.Equal(t, "valid@example.com", results[1].Email)
}

func TestBulkUserImportCommand_DryRunEmitsActivity(t *testing.T) {
	repo := newFakeAuthRepo()
	sink := &recordingActivitySink{}
	create := NewUserCreateCommand(UserCreateCommandConfig{
		Repository: repo,
		Activity:   sink,
	})
	cmd := NewBulkUserImportCommand(create)

	results := []BulkUserImportResult{}
	err := cmd.Execute(context.Background(), BulkUserImportInput{
		Users: []*types.AuthUser{
			{Email: "dry@example.com"},
		},
		Actor:   types.ActorRef{ID: uuid.New()},
		DryRun:  true,
		Results: &results,
	})

	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, uuid.Nil, results[0].UserID)
	require.Len(t, repo.users, 0)
	require.Len(t, sink.records, 1)
	require.Equal(t, "user.created", sink.records[0].Verb)
	require.Equal(t, true, sink.records[0].Data["dry_run"])
}

func TestBulkUserImportCommand_PolicyDenied(t *testing.T) {
	repo := newFakeAuthRepo()
	denyPolicy := types.AuthorizationPolicyFunc(func(context.Context, types.PolicyCheck) error {
		return types.ErrUnauthorizedScope
	})
	guard := scope.NewGuard(nil, denyPolicy)
	create := NewUserCreateCommand(UserCreateCommandConfig{
		Repository: repo,
		ScopeGuard: guard,
	})
	cmd := NewBulkUserImportCommand(create)

	results := []BulkUserImportResult{}
	err := cmd.Execute(context.Background(), BulkUserImportInput{
		Users: []*types.AuthUser{
			{Email: "denied@example.com"},
		},
		Actor:   types.ActorRef{ID: uuid.New()},
		Results: &results,
	})

	require.Error(t, err)
	require.Len(t, results, 1)
	require.Len(t, repo.users, 0)
	var richErr *goerrors.Error
	require.True(t, errors.As(results[0].Err, &richErr))
	require.Equal(t, goerrors.CategoryAuthz, richErr.Category)
}
