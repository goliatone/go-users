package registry

import (
	"context"
	"errors"

	repository "github.com/goliatone/go-repository-bun"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/uptrace/bun"
)

// WithTx returns an independent registry whose catalog and assignment operations
// use the caller's transaction. It neither commits nor rolls back. The caller
// must lock authoritative authorization state before checks, and must use this
// same transaction for policy reads, version advancement and required audit.
// The transaction must belong to the repositories' database. Transaction-scoped
// registries must not escape the callback or be used concurrently.
//
// Hooks are suppressed: a database write is not yet a committed event. Persist
// required publication in an outbox in the same transaction, then deliver after
// commit. The original registry and its hooks remain unchanged.
func (r *RoleRegistry) WithTx(tx bun.Tx) (*RoleRegistry, error) {
	if r == nil || tx.Tx == nil {
		return nil, errors.New("bun role registry: a live transaction is required")
	}
	bound := *r
	bound.roles = transactionRepository[*CustomRole]{Repository: r.roles, tx: tx}
	bound.assignments = transactionRepository[*RoleAssignment]{Repository: r.assignments, tx: tx}
	bound.hooks = types.Hooks{}
	bound.transactional = true
	return &bound, nil
}

// Only the persistence methods used by RoleRegistry are exposed through its
// private stores; forwarding retains repository scope/error handling.
type transactionRepository[T any] struct {
	repository.Repository[T]
	tx bun.IDB
}

func (r transactionRepository[T]) Create(ctx context.Context, record T, criteria ...repository.InsertCriteria) (T, error) {
	return r.CreateTx(ctx, r.tx, record, criteria...)
}
func (r transactionRepository[T]) GetByID(ctx context.Context, id string, criteria ...repository.SelectCriteria) (T, error) {
	return r.GetByIDTx(ctx, r.tx, id, criteria...)
}
func (r transactionRepository[T]) List(ctx context.Context, criteria ...repository.SelectCriteria) ([]T, int, error) {
	return r.ListTx(ctx, r.tx, criteria...)
}
func (r transactionRepository[T]) Update(ctx context.Context, record T, criteria ...repository.UpdateCriteria) (T, error) {
	return r.UpdateTx(ctx, r.tx, record, criteria...)
}
func (r transactionRepository[T]) Delete(ctx context.Context, record T) error {
	return r.DeleteTx(ctx, r.tx, record)
}
func (r transactionRepository[T]) DeleteWhere(ctx context.Context, criteria ...repository.DeleteCriteria) error {
	return r.DeleteWhereTx(ctx, r.tx, criteria...)
}

// BindTx is the native Group adapter for WithTx. It returns nil for an invalid
// receiver/transaction; callers needing a configuration error should use WithTx.
// It retains WithTx's transaction ordering and suppressed-hook contract.
func (r *RoleRegistry) BindTx(tx bun.Tx) *RoleRegistry {
	bound, err := r.WithTx(tx)
	if err != nil {
		return nil
	}
	return bound
}
