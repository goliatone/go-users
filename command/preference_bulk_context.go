package command

import "github.com/goliatone/go-users/pkg/types"

type preferenceBulkContext struct {
	level types.PreferenceLevel
	mode  types.PreferenceBulkMode
	scope types.ScopeFilter
	keys  []string
}
