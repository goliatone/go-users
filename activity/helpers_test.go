package activity

import (
	"testing"

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
