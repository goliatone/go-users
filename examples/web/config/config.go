package config

import (
	"time"

	"github.com/goliatone/go-auth"
	"github.com/goliatone/go-persistence-bun"
	"github.com/goliatone/go-router"
)

// BaseConfig holds all configuration for the web example
type BaseConfig struct {
	Server      ServerConfig            `json:"server"`
	Auth        AuthConfig              `json:"auth"`
	Persistence PersistenceConfig       `json:"persistence"`
	Views       router.SimpleViewConfig `json:"views"`
}

// ServerConfig holds HTTP server settings
type ServerConfig struct {
	Port string `json:"port" env:"SERVER_PORT" default:"8978"`
	Host string `json:"host" env:"SERVER_HOST" default:"localhost"`
}

// AuthConfig implements auth.Config interface
type AuthConfig struct {
	SigningKey            string   `json:"signing_key" env:"AUTH_SIGNING_KEY" default:"changeme-secret-key"`
	SigningMethod         string   `json:"signing_method" default:"HS256"`
	ContextKey            string   `json:"context_key" default:"auth_token"`
	TokenExpiration       int      `json:"token_expiration" default:"3600"`
	ExtendedTokenDuration int      `json:"extended_token_duration" default:"86400"`
	TokenLookup           string   `json:"token_lookup" default:"cookie:auth_token"`
	AuthScheme            string   `json:"auth_scheme" default:"Bearer"`
	Issuer                string   `json:"issuer" default:"go-users-web"`
	Audience              []string `json:"audience"`
	RejectedRouteKey      string   `json:"rejected_route_key" default:"rejected_route"`
	RejectedRouteDefault  string   `json:"rejected_route_default" default:"/auth/login"`
}

func (c AuthConfig) GetSigningKey() string           { return c.SigningKey }
func (c AuthConfig) GetSigningMethod() string        { return c.SigningMethod }
func (c AuthConfig) GetContextKey() string           { return c.ContextKey }
func (c AuthConfig) GetTokenExpiration() int         { return c.TokenExpiration }
func (c AuthConfig) GetExtendedTokenDuration() int   { return c.ExtendedTokenDuration }
func (c AuthConfig) GetTokenLookup() string          { return c.TokenLookup }
func (c AuthConfig) GetAuthScheme() string           { return c.AuthScheme }
func (c AuthConfig) GetIssuer() string               { return c.Issuer }
func (c AuthConfig) GetAudience() []string           { return c.Audience }
func (c AuthConfig) GetRejectedRouteKey() string     { return c.RejectedRouteKey }
func (c AuthConfig) GetRejectedRouteDefault() string { return c.RejectedRouteDefault }

// PersistenceConfig implements persistence.Config interface
type PersistenceConfig struct {
	Debug          bool          `json:"debug" default:"true"`
	Driver         string        `json:"driver" default:"sqlite"`
	Server         string        `json:"server" env:"DB_SERVER" default:"file:test.db?_journal_mode=WAL&cache=shared&_fk=1"`
	PingTimeout    time.Duration `json:"ping_timeout" default:"5s"`
	OtelIdentifier string        `json:"otel_identifier" default:"go-users-web"`
}

func (c PersistenceConfig) GetDebug() bool                { return c.Debug }
func (c PersistenceConfig) GetDriver() string             { return c.Driver }
func (c PersistenceConfig) GetServer() string             { return c.Server }
func (c PersistenceConfig) GetPingTimeout() time.Duration { return c.PingTimeout }
func (c PersistenceConfig) GetOtelIdentifier() string     { return c.OtelIdentifier }

// GetAuth returns auth config
func (c *BaseConfig) GetAuth() auth.Config {
	return c.Auth
}

// GetPersistence returns persistence config
func (c *BaseConfig) GetPersistence() persistence.Config {
	return c.Persistence
}

// GetViews returns view engine config
func (c *BaseConfig) GetViews() *router.SimpleViewConfig {
	return &c.Views
}

// GetServer returns server config
func (c *BaseConfig) GetServer() ServerConfig {
	return c.Server
}

// Validate implements config.Validable interface
func (c *BaseConfig) Validate() error {
	return nil
}
