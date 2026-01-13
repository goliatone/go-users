package preferences

import (
	"strings"

	"github.com/goliatone/go-repository-cache/cache"
)

// RepositoryOption configures preference repository construction.
type RepositoryOption func(*RepositoryOptions)

// RepositoryOptions captures optional behavior for preference persistence.
type RepositoryOptions struct {
	CacheEnabled          bool
	CacheConfig           *cache.Config
	CacheService          cache.CacheService
	CacheKeySerializer    cache.KeySerializer
	CacheIdentifierFields []string
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

// WithCacheService supplies a preconfigured cache service to use when caching is enabled.
func WithCacheService(service cache.CacheService) RepositoryOption {
	return func(opts *RepositoryOptions) {
		if opts == nil || service == nil {
			return
		}
		opts.CacheService = service
	}
}

// WithCacheKeySerializer supplies a custom key serializer for cache keys.
func WithCacheKeySerializer(serializer cache.KeySerializer) RepositoryOption {
	return func(opts *RepositoryOptions) {
		if opts == nil || serializer == nil {
			return
		}
		opts.CacheKeySerializer = serializer
	}
}

// WithCacheIdentifierFields supplies identifier fields used for cache tags on GetByIdentifier.
func WithCacheIdentifierFields(fields ...string) RepositoryOption {
	return func(opts *RepositoryOptions) {
		if opts == nil {
			return
		}
		opts.CacheIdentifierFields = sanitizeIdentifierFields(fields)
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

func sanitizeIdentifierFields(fields []string) []string {
	if len(fields) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(fields))
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		result = append(result, field)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
