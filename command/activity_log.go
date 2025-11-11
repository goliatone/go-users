package command

import (
	"context"
	"strings"

	gocommand "github.com/goliatone/go-command"
	"github.com/goliatone/go-users/pkg/types"
)

// ActivityLogInput wraps a record to persist through the ActivitySink.
type ActivityLogInput struct {
	Record types.ActivityRecord
}

// Type implements gocommand.Message.
func (ActivityLogInput) Type() string {
	return "command.activity.log"
}

// Validate implements gocommand.Message.
func (input ActivityLogInput) Validate() error {
	if strings.TrimSpace(input.Record.Verb) == "" {
		return ErrActivityVerbRequired
	}
	return nil
}

// ActivityLogCommand logs arbitrary activity records.
type ActivityLogCommand struct {
	sink  types.ActivitySink
	hooks types.Hooks
	clock types.Clock
}

// ActivityLogConfig wires dependencies for the log command.
type ActivityLogConfig struct {
	Sink  types.ActivitySink
	Hooks types.Hooks
	Clock types.Clock
}

// NewActivityLogCommand constructs the logging command handler.
func NewActivityLogCommand(cfg ActivityLogConfig) *ActivityLogCommand {
	return &ActivityLogCommand{
		sink:  cfg.Sink,
		hooks: cfg.Hooks,
		clock: safeClock(cfg.Clock),
	}
}

var _ gocommand.Commander[ActivityLogInput] = (*ActivityLogCommand)(nil)

// Execute validates and persists the supplied record.
func (c *ActivityLogCommand) Execute(ctx context.Context, input ActivityLogInput) error {
	if c.sink == nil {
		return types.ErrMissingActivitySink
	}
	if err := input.Validate(); err != nil {
		return err
	}
	record := input.Record
	if record.OccurredAt.IsZero() {
		record.OccurredAt = now(c.clock)
	}
	if err := c.sink.Log(ctx, record); err != nil {
		return err
	}
	emitActivityHook(ctx, c.hooks, record)
	return nil
}
