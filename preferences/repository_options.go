package preferences

import "github.com/goliatone/go-repository-cache/cache"

// RepositoryOption configures preference repository construction.
type RepositoryOption func(*RepositoryOptions)

// RepositoryOptions captures optional behavior for preference persistence.
type RepositoryOptions struct {
	CacheEnabled bool
	CacheConfig  *cache.Config
}

// WithCache toggles the repository cache decorator.
func WithCache(enabled bool) RepositoryOption {
	return func(opts *RepositoryOptions) {
		if opts == nil {
			return
		}
		opts.CacheEnabled = enabled
	}
}

// WithCacheConfig supplies the cache configuration to use when caching is enabled.
func WithCacheConfig(cfg cache.Config) RepositoryOption {
	return func(opts *RepositoryOptions) {
		if opts == nil {
			return
		}
		opts.CacheConfig = &cfg
	}
}

func applyRepositoryOptions(options []RepositoryOption) RepositoryOptions {
	var opts RepositoryOptions
	for _, opt := range options {
		if opt == nil {
			continue
		}
		opt(&opts)
	}
	return opts
}
