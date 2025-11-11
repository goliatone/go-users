package crudsvc

import (
	"context"
	"fmt"

	"github.com/goliatone/go-crud"
	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-users/crudguard"
	"github.com/goliatone/go-users/pkg/types"
)

// GuardAdapter captures the subset of crudguard.Adapter we rely on so tests can
// swap in fakes.
type GuardAdapter interface {
	Enforce(in crudguard.GuardInput) (crudguard.GuardResult, error)
}

// ActivityEmitter propagates audit/activity events triggered by CRUD services.
type ActivityEmitter interface {
	Emit(ctx context.Context, record types.ActivityRecord) error
}

// SinkEmitter adapts a types.ActivitySink so it can be used as an emitter.
type SinkEmitter struct {
	Sink types.ActivitySink
}

// Emit satisfies the ActivityEmitter interface.
func (e SinkEmitter) Emit(ctx context.Context, record types.ActivityRecord) error {
	if e.Sink == nil {
		return nil
	}
	return e.Sink.Log(ctx, record)
}

type serviceOptions struct {
	emitter ActivityEmitter
	logger  types.Logger
}

// ServiceOption customizes CRUD service behaviour.
type ServiceOption func(*serviceOptions)

// WithActivityEmitter wires the emitter used to log CRUD side-effects.
func WithActivityEmitter(emitter ActivityEmitter) ServiceOption {
	return func(cfg *serviceOptions) {
		if emitter != nil {
			cfg.emitter = emitter
		}
	}
}

// WithLogger wires a logger for service diagnostics.
func WithLogger(logger types.Logger) ServiceOption {
	return func(cfg *serviceOptions) {
		if logger != nil {
			cfg.logger = logger
		}
	}
}

func applyOptions(opts []ServiceOption) serviceOptions {
	cfg := serviceOptions{
		logger: types.NopLogger{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return cfg
}

func notSupported(op crud.CrudOperation) error {
	return goerrors.New(
		fmt.Sprintf("go-users: crud operation %s disabled for this resource", op),
		goerrors.CategoryValidation,
	).WithCode(goerrors.CodeBadRequest)
}

// WithCommandService mirrors crud.WithService but gives consumers a semantic
// helper to highlight that the controller delegates to the command/query layer.
func WithCommandService[T any](svc crud.Service[T]) crud.Option[T] {
	return crud.WithService(svc)
}
