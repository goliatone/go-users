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

func TestDefaultAccessPolicyApplySetsMachineFilterForAdmins(t *testing.T) {
	t.Helper()
	policy := NewDefaultAccessPolicy(WithPolicyFilterOptions(WithMachineActivityEnabled(false)))
	actor := &auth.ActorContext{
		ActorID: uuid.NewString(),
		Role:    types.ActorRoleTenantAdmin,
	}

	filter, err := policy.Apply(actor, "", types.ActivityFilter{})
	require.NoError(t, err)
	require.NotNil(t, filter.MachineActivityEnabled)
	require.False(t, *filter.MachineActivityEnabled)
	require.ElementsMatch(t, DefaultMachineActorTypes(), filter.MachineActorTypes)
	require.ElementsMatch(t, DefaultMachineDataKeys(), filter.MachineDataKeys)
}

func TestDefaultAccessPolicyApplySkipsMachineFilterForSuperadmin(t *testing.T) {
	t.Helper()
	policy := NewDefaultAccessPolicy(WithPolicyFilterOptions(WithMachineActivityEnabled(false)))
	actor := &auth.ActorContext{
		ActorID: uuid.NewString(),
		Role:    types.ActorRoleSystemAdmin,
	}

	filter, err := policy.Apply(actor, "", types.ActivityFilter{})
	require.NoError(t, err)
	require.Nil(t, filter.MachineActivityEnabled)
	require.Empty(t, filter.MachineActorTypes)
	require.Empty(t, filter.MachineDataKeys)
}

func TestDefaultAccessPolicyApplyStatsSelfOnly(t *testing.T) {
	t.Helper()
	actorID := uuid.New()
	tenantID := uuid.New()
	policy := NewDefaultAccessPolicy(WithPolicyStatsSelfOnly(true))
	actor := &auth.ActorContext{
		ActorID:  actorID.String(),
		Role:     types.ActorRoleSupport,
		TenantID: tenantID.String(),
	}

	filter, err := policy.ApplyStats(actor, "", types.ActivityStatsFilter{})
	require.NoError(t, err)
	require.Equal(t, actorID, filter.UserID)
	require.Equal(t, actorID, filter.ActorID)
	require.Equal(t, tenantID, filter.Scope.TenantID)
}

func TestSanitizeRecordMasksDefaultFields(t *testing.T) {
	t.Helper()
	record := types.ActivityRecord{
		Data: map[string]any{
			"password":    "secret-value",
			"token":       "abcd1234",
			"secret":      "shh",
			"actor_email": "admin@example.com",
			"session_id":  "session-12345",
		},
	}
	out := SanitizeRecord(DefaultMasker(), record)
	require.NotEqual(t, "secret-value", out.Data["password"])
	require.NotEqual(t, "abcd1234", out.Data["token"])
	require.NotEqual(t, "shh", out.Data["secret"])
	require.NotEqual(t, "admin@example.com", out.Data["actor_email"])
	require.NotEqual(t, "session-12345", out.Data["session_id"])
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

func TestDefaultAccessPolicySanitizeSupportSanitizedExposure(t *testing.T) {
	t.Helper()
	policy := NewDefaultAccessPolicy(WithMetadataExposure(MetadataExposeSanitized))
	actor := &auth.ActorContext{
		ActorID: uuid.NewString(),
		Role:    types.ActorRoleSupport,
	}

	records := []types.ActivityRecord{
		{ID: uuid.New(), Data: map[string]any{"token": "abcd1234"}},
	}
	out := policy.Sanitize(actor, "", records)
	require.Len(t, out, 1)
	require.NotNil(t, out[0].Data)
	require.NotEqual(t, "abcd1234", out[0].Data["token"])
}

func TestDefaultAccessPolicySanitizeSupportAllExposure(t *testing.T) {
	t.Helper()
	policy := NewDefaultAccessPolicy(WithMetadataExposure(MetadataExposeAll))
	actor := &auth.ActorContext{
		ActorID: uuid.NewString(),
		Role:    types.ActorRoleSupport,
	}

	records := []types.ActivityRecord{
		{ID: uuid.New(), Data: map[string]any{"token": "abcd1234"}},
	}
	out := policy.Sanitize(actor, "", records)
	require.Len(t, out, 1)
	require.NotNil(t, out[0].Data)
	require.Equal(t, "abcd1234", out[0].Data["token"])
}
