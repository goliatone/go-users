package activity

import (
	"testing"

	"github.com/goliatone/go-auth"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestBuildFilterFromActorRoleAliases(t *testing.T) {
	t.Helper()
	actorID := uuid.New()
	tenantID := uuid.New()
	requested := uuid.New()
	actor := &auth.ActorContext{
		ActorID:  actorID.String(),
		Role:     "manager",
		TenantID: tenantID.String(),
	}

	filter, err := BuildFilterFromActor(actor, "", types.ActivityFilter{
		UserID:  requested,
		ActorID: requested,
	}, WithRoleAliases([]string{"manager"}, nil))
	require.NoError(t, err)
	require.Equal(t, requested, filter.UserID)
	require.Equal(t, requested, filter.ActorID)
	require.Equal(t, tenantID, filter.Scope.TenantID)
}

func TestBuildFilterFromActorChannelAllowDeny(t *testing.T) {
	t.Helper()
	actor := &auth.ActorContext{
		ActorID: uuid.NewString(),
		Role:    types.ActorRoleTenantAdmin,
	}

	filter, err := BuildFilterFromActor(actor, "", types.ActivityFilter{
		Channels:        []string{"settings", "bulk"},
		ChannelDenylist: []string{"bulk"},
	}, WithChannelAllowlist("settings", "bulk"), WithChannelDenylist("audit"))
	require.NoError(t, err)
	require.Equal(t, []string{"settings"}, filter.Channels)
	require.Empty(t, filter.Channel)
	require.ElementsMatch(t, []string{"audit", "bulk"}, filter.ChannelDenylist)
}

func TestDefaultAccessPolicyFiltersMachineActivity(t *testing.T) {
	t.Helper()
	policy := NewDefaultAccessPolicy(WithPolicyFilterOptions(WithMachineActivityEnabled(false)))
	actor := &auth.ActorContext{
		ActorID: uuid.NewString(),
		Role:    types.ActorRoleTenantAdmin,
	}

	records := []types.ActivityRecord{
		{ID: uuid.New(), Data: map[string]any{"system": true}},
		{ID: uuid.New(), Data: map[string]any{"actor_type": "job"}},
		{ID: uuid.New(), Data: map[string]any{"event": "user.login"}},
	}
	out := policy.Sanitize(actor, "", records)
	require.Len(t, out, 1)
	require.Equal(t, records[2].ID, out[0].ID)
}

func TestDefaultAccessPolicyMachineActivityAllowsSuperadmin(t *testing.T) {
	t.Helper()
	policy := NewDefaultAccessPolicy(WithPolicyFilterOptions(WithMachineActivityEnabled(false)))
	actor := &auth.ActorContext{
		ActorID: uuid.NewString(),
		Role:    types.ActorRoleSystemAdmin,
	}

	records := []types.ActivityRecord{
		{ID: uuid.New(), Data: map[string]any{"system": true}},
		{ID: uuid.New(), Data: map[string]any{"actor_type": "task"}},
		{ID: uuid.New(), Data: map[string]any{"event": "user.login"}},
	}
	out := policy.Sanitize(actor, "", records)
	require.Len(t, out, len(records))
}

func TestSanitizeRecordMasksDefaultFields(t *testing.T) {
	t.Helper()
	record := types.ActivityRecord{
		Data: map[string]any{
			"password": "secret-value",
			"token":    "abcd1234",
			"secret":   "shh",
		},
	}
	out := SanitizeRecord(DefaultMasker(), record)
	require.NotEqual(t, "secret-value", out.Data["password"])
	require.NotEqual(t, "abcd1234", out.Data["token"])
	require.NotEqual(t, "shh", out.Data["secret"])
}

func TestDefaultAccessPolicySanitizeRedactsAndMasks(t *testing.T) {
	t.Helper()
	policy := NewDefaultAccessPolicy()
	actor := &auth.ActorContext{
		ActorID: uuid.NewString(),
		Role:    types.ActorRoleTenantAdmin,
	}

	records := []types.ActivityRecord{
		{ID: uuid.New(), IP: "10.10.10.10", Data: map[string]any{"token": "abcd1234"}},
	}
	out := policy.Sanitize(actor, "", records)
	require.Len(t, out, 1)
	require.Empty(t, out[0].IP)
	require.NotEqual(t, "abcd1234", out[0].Data["token"])
}

func TestDefaultAccessPolicySanitizeStripsSupportData(t *testing.T) {
	t.Helper()
	policy := NewDefaultAccessPolicy()
	actor := &auth.ActorContext{
		ActorID: uuid.NewString(),
		Role:    types.ActorRoleSupport,
	}

	records := []types.ActivityRecord{
		{ID: uuid.New(), Data: map[string]any{"token": "abcd1234"}},
	}
	out := policy.Sanitize(actor, "", records)
	require.Len(t, out, 1)
	require.Nil(t, out[0].Data)
}
