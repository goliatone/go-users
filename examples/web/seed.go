package main

import (
	"context"
	"fmt"

	auth "github.com/goliatone/go-auth"
	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/activity"
	"github.com/goliatone/go-users/command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

type seedAccount struct {
	Email       string
	Username    string
	FirstName   string
	LastName    string
	Password    string
	TenantID    uuid.UUID
	WorkspaceID uuid.UUID
}

var demoAccounts = []seedAccount{
	{
		Email:       "maya.ops@example.com",
		Username:    "ops.admin",
		FirstName:   "Maya",
		LastName:    "Castillo",
		Password:    "ChangeM3!",
		TenantID:    tenantOpsID,
		WorkspaceID: workspaceOpsID,
	},
	{
		Email:       "leon.commerce@example.com",
		Username:    "commerce.admin",
		FirstName:   "Leon",
		LastName:    "Price",
		Password:    "ChangeM3!",
		TenantID:    tenantCommerceID,
		WorkspaceID: workspaceCommerceID,
	},
}

type seedActivity struct {
	Email      string
	Verb       string
	ObjectType string
	ObjectID   string
	Channel    string
	Data       map[string]any
}

var demoActivity = []seedActivity{
	{
		Email:      "maya.ops@example.com",
		Verb:       "user.lifecycle.transition",
		ObjectType: "user",
		Channel:    "lifecycle",
		Data: map[string]any{
			"from": "pending",
			"to":   "active",
		},
	},
	{
		Email:      "maya.ops@example.com",
		Verb:       "role.assigned",
		ObjectType: "role",
		Channel:    "roles",
		Data: map[string]any{
			"role": "ops.editors",
		},
	},
	{
		Email:      "leon.commerce@example.com",
		Verb:       "preferences.updated",
		ObjectType: "preference",
		Channel:    "preferences",
		Data: map[string]any{
			"key": "dashboard.theme",
		},
	},
}

func seedDemoUsers(ctx context.Context, app *App) error {
	if app.repo == nil || app.workspaceDir == nil {
		return nil
	}

	usersRepo := app.repo.Users()
	for _, acct := range demoAccounts {
		record, err := usersRepo.GetByIdentifier(ctx, acct.Email)
		if err != nil && !repository.IsRecordNotFound(err) {
			return err
		}

		if err == nil && record != nil {
			app.workspaceDir.Ensure(record.ID, acct.TenantID, acct.WorkspaceID)
			app.demoUsers[acct.Email] = record.ID
			app.demoScopes[acct.Email] = scopeFor(acct.TenantID, acct.WorkspaceID)
			continue
		}

		passwordHash, err := auth.HashPassword(acct.Password)
		if err != nil {
			return err
		}

		user := &auth.User{
			ID:             uuid.New(),
			Role:           auth.RoleAdmin,
			Status:         auth.UserStatusActive,
			FirstName:      acct.FirstName,
			LastName:       acct.LastName,
			Username:       acct.Username,
			Email:          acct.Email,
			PasswordHash:   passwordHash,
			EmailValidated: true,
			ExternalID:     acct.Email,
			ExternalIDProvider: "seed",
		}

		user.AddMetadata(tenantMetadataKey, acct.TenantID.String())
		user.AddMetadata(workspaceMetadataKey, acct.WorkspaceID.String())

		created, err := usersRepo.Create(ctx, user)
		if err != nil {
			return err
		}

		app.workspaceDir.Bind(created.ID, acct.TenantID, acct.WorkspaceID)
		app.demoUsers[acct.Email] = created.ID
		app.demoScopes[acct.Email] = scopeFor(acct.TenantID, acct.WorkspaceID)
	}

	return nil
}

func seedActivityData(ctx context.Context, app *App) error {
	if app.users == nil || app.bunDB == nil {
		return nil
	}

	count, err := app.bunDB.NewSelect().
		Model((*activity.LogEntry)(nil)).
		Count(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	for _, entry := range demoActivity {
		actorID := app.demoUsers[entry.Email]
		if actorID == uuid.Nil {
			continue
		}
		scope := app.demoScopes[entry.Email]
		record := types.ActivityRecord{
			UserID:     actorID,
			ActorID:    actorID,
			Verb:       entry.Verb,
			ObjectType: entry.ObjectType,
			ObjectID:   chooseObjectID(entry.ObjectID, actorID),
			Channel:    entry.Channel,
			Data:       entry.Data,
			TenantID:   scope.TenantID,
			OrgID:      scope.OrgID,
		}
		if err := app.users.Commands().LogActivity.Execute(ctx, command.ActivityLogInput{
			Record: record,
		}); err != nil {
			return fmt.Errorf("seed activity failed for %s: %w", entry.Email, err)
		}
	}
	return nil
}

func scopeFor(tenantID, workspaceID uuid.UUID) types.ScopeFilter {
	scope := types.ScopeFilter{
		TenantID: tenantID,
		OrgID:    workspaceID,
	}
	return scope.WithLabel(workspaceScopeLabel, workspaceID)
}

func chooseObjectID(provided string, fallback uuid.UUID) string {
	if provided != "" {
		return provided
	}
	return fallback.String()
}
