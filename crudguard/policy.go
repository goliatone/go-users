package crudguard

import (
	"maps"

	"github.com/goliatone/go-crud"
	"github.com/goliatone/go-users/pkg/types"
)

// DefaultPolicyMap maps the standard CRUD verbs to the supplied read/write
// PolicyActions. Create/Update/Delete (and their batch variants) map to the
// write action while list/show map to the read action.
func DefaultPolicyMap(readAction, writeAction types.PolicyAction) map[crud.CrudOperation]types.PolicyAction {
	m := map[crud.CrudOperation]types.PolicyAction{
		crud.OpRead:        readAction,
		crud.OpList:        readAction,
		crud.OpCreate:      writeAction,
		crud.OpCreateBatch: writeAction,
		crud.OpUpdate:      writeAction,
		crud.OpUpdateBatch: writeAction,
		crud.OpDelete:      writeAction,
		crud.OpDeleteBatch: writeAction,
	}
	return m
}

func clonePolicyMap(in map[crud.CrudOperation]types.PolicyAction) map[crud.CrudOperation]types.PolicyAction {
	if len(in) == 0 {
		return nil
	}
	cp := make(map[crud.CrudOperation]types.PolicyAction, len(in))
	maps.Copy(cp, in)
	return cp
}
