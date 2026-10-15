package activity

import (
	"context"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRoleEventMapperPreservesCatalogAndAssignmentScope(t *testing.T) {
	catalog := types.ScopeFilter{TenantID: uuid.New(), OrgID: uuid.New()}
	for _, event := range []types.RoleEvent{
		{Action: "role.created", Scope: catalog, RoleID: uuid.New()},
		{Action: "role.assigned", Scope: types.ScopeFilter{}.WithLabel("group", uuid.New()).WithLabel("team", uuid.New()), RoleID: uuid.New()},
		{Action: "role.unassigned", Scope: catalog.WithLabel("group", uuid.New()), RoleID: uuid.New()},
	} {
		record, err := (RoleEventMapper{}).Map(context.Background(), event)
		require.NoError(t, err)
		require.Equal(t, event.Scope.TenantID, record.TenantID)
		require.Equal(t, event.Scope.OrgID, record.OrgID)
		require.Equal(t, len(event.Scope.Labels), len(record.Labels))
		for key, id := range event.Scope.Labels {
			require.Equal(t, id, record.Labels[key])
		}
		require.Equal(t, event.Action, record.Verb)
		original := event.Scope.Clone()
		record.Labels["other"] = uuid.New()
		require.Equal(t, original, event.Scope)
	}
}
