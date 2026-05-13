package migrations

import persistence "github.com/goliatone/go-persistence-bun"

// StableOrderedSources converts profile descriptors into source-stable ordered
// migration sources. SourceKey and Order are durable migration ABI once used in
// a released database.
func StableOrderedSources(sources []ProfileSource) []persistence.OrderedMigrationSource {
	ordered := make([]persistence.OrderedMigrationSource, 0, len(sources))
	for _, source := range sources {
		dialectOptions := []persistence.DialectMigrationOption{
			persistence.WithDialectSourceLabel(source.SourceLabel),
		}
		if len(source.ValidationTargets) > 0 {
			dialectOptions = append(dialectOptions, persistence.WithValidationTargets(source.ValidationTargets...))
		}

		options := []persistence.OrderedMigrationSourceOption{
			persistence.WithOrderedMigrationDialectOptions(dialectOptions...),
		}
		if len(source.DependsOn) > 0 {
			options = append(options, persistence.WithOrderedMigrationDependencies(source.DependsOn...))
		}

		ordered = append(ordered, persistence.NewStableOrderedMigrationSource(
			source.Name,
			source.Filesystem,
			source.SourceKey,
			source.Order,
			options...,
		))
	}
	return ordered
}

// StableOrderedProfileSources resolves a migration profile and returns v0.16
// source-stable ordered sources. Register the returned sources together; do not
// mix source-stable and legacy positional ordered sources in one registration set.
func StableOrderedProfileSources(profile MigrationProfile, opts ...ProfileOption) ([]persistence.OrderedMigrationSource, error) {
	sources, err := ProfileSources(profile, opts...)
	if err != nil {
		return nil, err
	}
	return StableOrderedSources(sources), nil
}
