package users

import "github.com/goliatone/go-users/service"

// Re-export the service package entry point so consumers can do `users.New(...)`
// without importing internal wiring helpers.
type (
	Service            = service.Service
	Config             = service.Config
	Commands           = service.Commands
	Queries            = service.Queries
	PreferenceResolver = service.PreferenceResolver
)

// New constructs the go-users runtime using the provided configuration.
func New(cfg Config) *Service {
	return service.New(cfg)
}
