# Auth Provider Integration

go-users relies on an upstream authentication service (go-auth today) that
implements the `types.AuthRepository` interface. This document explains how to
wire the repository, what adapters exist, and how to test integrations.

## Requirements

The auth repository must expose:

- CRUD operations for user records (`GetByID`, `GetByIdentifier`, `Create`, `Update`)
- Lifecycle transitions with reason/metadata hooks (`UpdateStatus`)
- A way to query allowed transitions for the current lifecycle state

go-users ships `types.AuthRepository` and helper types (`AuthUser`,
`LifecycleTransition`, `ActorRef`, `TransitionOption`) that define this contract.

## go-auth Adapter

If you are already using `github.com/goliatone/go-auth`, reuse the adapter in
`adapter/goauth`:

```go
import (
    auth "github.com/goliatone/go-auth"
    "github.com/goliatone/go-users/adapter/goauth"
    "github.com/goliatone/go-users/service"
)

usersRepo := auth.NewUsersRepository(bunDB)
authProvider := goauth.NewUsersAdapter(usersRepo)

svc := users.New(users.Config{
    AuthRepository: authProvider,
    // other dependencies…
})
```

The adapter:

- Wraps `auth.Users`
- Converts go-auth `UserStatus` values into go-users `LifecycleState`
- Proxies lifecycle transitions through go-auth’s `UserStateMachine`
- Applies the default transition policy; override it via `goauth.WithPolicy(...)`

## Custom Providers

To integrate a different auth store:

1. Implement `types.AuthRepository`
2. Map your user struct to `types.AuthUser`
3. Enforce lifecycle policies (you can reuse `types.DefaultTransitionPolicy()`)

## Testing

- Use the migration smoke test (`task migrate`) to verify DB connectivity
- Mock `types.AuthRepository` when testing go-users commands
- For go-auth setups, rely on its existing repository tests and only unit-test
  the adapter if you customize policies
