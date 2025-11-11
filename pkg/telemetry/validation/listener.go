package validation

import (
	"context"

	"github.com/goliatone/go-auth/middleware/jwtware"
	"github.com/goliatone/go-router"
	"github.com/goliatone/go-users/pkg/authctx"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/google/uuid"
)

// SchemaNotifier receives callbacks whenever an authenticated actor is
// validated so schema exporters can refresh caches.
type SchemaNotifier interface {
	Notify(ctx context.Context, actorID uuid.UUID, metadata map[string]any)
}

// ListenerOptions customize the validation listener behaviour.
type ListenerOptions struct {
	ActivitySink   types.ActivitySink
	Logger         types.Logger
	SchemaNotifier SchemaNotifier
}

// NewListener returns a jwtware.ValidationListener that emits audit records and
// notifies schema observers whenever a token is validated.
func NewListener(opts ListenerOptions) jwtware.ValidationListener {
	logger := opts.Logger
	if logger == nil {
		logger = types.NopLogger{}
	}
	return func(ctx router.Context, claims jwtware.AuthClaims) error {
		actorCtx, err := authctx.ResolveActorContextFromRouter(ctx)
		if err != nil {
			logger.Error("validation listener failed to resolve actor", err)
			return nil
		}
		if opts.ActivitySink != nil {
			record := types.ActivityRecord{
				ActorID:    parseUUID(actorCtx.ActorID),
				Verb:       "auth.validated",
				ObjectType: "auth",
				ObjectID:   claims.Subject(),
				Channel:    "auth",
				TenantID:   parseUUID(actorCtx.TenantID),
				OrgID:      parseUUID(actorCtx.OrganizationID),
				Data: map[string]any{
					"role": actorCtx.Role,
				},
			}
			if err := opts.ActivitySink.Log(ctx.Context(), record); err != nil {
				logger.Error("validation activity sink failed", err)
			}
		}
		if opts.SchemaNotifier != nil {
			opts.SchemaNotifier.Notify(ctx.Context(), parseUUID(actorCtx.ActorID), actorCtx.Metadata)
		}
		return nil
	}
}

func parseUUID(value string) uuid.UUID {
	if value == "" {
		return uuid.Nil
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil
	}
	return id
}
