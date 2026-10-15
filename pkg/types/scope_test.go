package types

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestScopeLabelsCanonicalKeysAndDetachedCopies(t *testing.T) {
	id, other := uuid.New(), uuid.New()
	original := ScopeFilter{TenantID: uuid.New(), OrgID: uuid.New()}.WithLabel(" Group ", id)
	require.Equal(t, id, original.Label("group"))
	require.Equal(t, id, original.Label(" GROUP "))
	require.Equal(t, map[string]uuid.UUID{"group": id}, original.Labels)
	clone := original.Clone()
	clone.Labels["group"] = other
	require.Equal(t, id, original.Label("group"))
	updated := original.WithLabel("Group", other)
	require.Equal(t, other, updated.Label("group"))
	require.Equal(t, id, original.Label("group"))
	require.Equal(t, original, original.WithLabel("  ", other))
	require.Equal(t, original, original.WithLabel("group", uuid.Nil))
	require.Equal(t, uuid.Nil, original.Label("absent"))
}

func TestScopeNormalizeLabels(t *testing.T) {
	id := uuid.New()
	for _, labels := range []map[string]uuid.UUID{
		{"": id}, {"  ": id}, {"group": uuid.Nil},
		{" Group ": id, "group": uuid.New()},
	} {
		_, err := (ScopeFilter{Labels: labels}).NormalizeLabels()
		require.ErrorIs(t, err, ErrInvalidScopeLabels)
	}
	original := ScopeFilter{TenantID: uuid.New(), OrgID: uuid.New(), Labels: map[string]uuid.UUID{" Group ": id, "group": id}}
	normalized, err := original.NormalizeLabels()
	require.NoError(t, err)
	require.Equal(t, original.TenantID, normalized.TenantID)
	require.Equal(t, original.OrgID, normalized.OrgID)
	require.Equal(t, map[string]uuid.UUID{"group": id}, normalized.Labels)
	normalized.Labels["group"] = uuid.New()
	require.Equal(t, id, original.Labels["group"])
	empty, err := (ScopeFilter{}).NormalizeLabels()
	require.NoError(t, err)
	require.Empty(t, empty.Labels)
}
