package activity

import (
	"testing"
	"time"

	"github.com/goliatone/go-auth"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestBuildRecordFromActorPopulatesFields(t *testing.T) {
	actorID := uuid.New()
	tenantID := uuid.New()
	orgID := uuid.New()
	meta := map[string]any{"from": "draft"}

	record, err := BuildRecordFromActor(&auth.ActorContext{
		ActorID:        actorID.String(),
		Role:           "admin",
		TenantID:       tenantID.String(),
		OrganizationID: orgID.String(),
	}, "settings.updated", "settings", "global", meta, WithChannel("admin"))
	require.NoError(t, err)

	require.Equal(t, actorID, record.ActorID)
	require.Equal(t, tenantID, record.TenantID)
	require.Equal(t, orgID, record.OrgID)
	require.Equal(t, "settings.updated", record.Verb)
	require.Equal(t, "settings", record.ObjectType)
	require.Equal(t, "global", record.ObjectID)
	require.Equal(t, "admin", record.Channel)
	require.Equal(t, "draft", record.Data["from"])

	// Ensure metadata was defensively copied.
	meta["from"] = "mutated"
	require.Equal(t, "draft", record.Data["from"])
}

func TestBuildRecordFromActorHandlesNilMetadata(t *testing.T) {
	record, err := BuildRecordFromActor(&auth.ActorContext{
		ActorID:  uuid.NewString(),
		TenantID: uuid.NewString(),
	}, "media.uploaded", "asset", "asset-1", nil)
	require.NoError(t, err)
	require.NotNil(t, record.Data)
	require.Len(t, record.Data, 0)
	require.Empty(t, record.Channel)
}

func TestBuildRecordFromActorValidatesActor(t *testing.T) {
	_, err := BuildRecordFromActor(nil, "export.requested", "export", "exp-1", nil)
	require.Error(t, err)

	_, err = BuildRecordFromActor(&auth.ActorContext{ActorID: "not-a-uuid"}, "export.completed", "export", "exp-1", nil)
	require.Error(t, err)
}

func TestBuildRecordFromUUIDPopulatesFields(t *testing.T) {
	actorID := uuid.New()
	meta := map[string]any{"state": "draft"}

	record, err := BuildRecordFromUUID(actorID, "  settings.updated  ", " settings ", " global ", meta)
	require.NoError(t, err)

	require.Equal(t, actorID, record.ActorID)
	require.Equal(t, "settings.updated", record.Verb)
	require.Equal(t, "settings", record.ObjectType)
	require.Equal(t, "global", record.ObjectID)
	require.Empty(t, record.Channel)
	require.NotNil(t, record.Data)
	require.Equal(t, "draft", record.Data["state"])
	require.Equal(t, time.UTC, record.OccurredAt.Location())
	require.WithinDuration(t, time.Now().UTC(), record.OccurredAt, 2*time.Second)

	meta["state"] = "mutated"
	require.Equal(t, "draft", record.Data["state"])
}

func TestBuildRecordFromUUIDOptionsOverride(t *testing.T) {
	actorID := uuid.New()
	tenantID := uuid.New()
	orgID := uuid.New()
	occurredAt := time.Date(2023, 3, 15, 10, 5, 0, 0, time.UTC)

	record, err := BuildRecordFromUUID(actorID, "role.assigned", "role", "role-1", nil,
		WithChannel("audit"),
		WithTenant(tenantID),
		WithOrg(orgID),
		WithOccurredAt(occurredAt),
	)
	require.NoError(t, err)

	require.Equal(t, "audit", record.Channel)
	require.Equal(t, tenantID, record.TenantID)
	require.Equal(t, orgID, record.OrgID)
	require.Equal(t, occurredAt, record.OccurredAt)
}

func TestBuildRecordFromUUIDValidatesRequiredFields(t *testing.T) {
	_, err := BuildRecordFromUUID(uuid.New(), "   ", "object", "obj-1", nil)
	require.Error(t, err)

	_, err = BuildRecordFromUUID(uuid.New(), "action", "   ", "obj-1", nil)
	require.Error(t, err)
}

func TestBuildRecordFromUUIDClonesMetadata(t *testing.T) {
	meta := map[string]any{"from": "source"}

	record, err := BuildRecordFromUUID(uuid.New(), "user.invited", "user", "user-1", meta)
	require.NoError(t, err)

	meta["from"] = "mutated"
	require.Equal(t, "source", record.Data["from"])
	require.NotNil(t, record.Data)
}

func TestBuildRecordFromUUIDAllowsZeroActor(t *testing.T) {
	record, err := BuildRecordFromUUID(uuid.Nil, "user.viewed", "user", "user-1", nil)
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, record.ActorID)
}
