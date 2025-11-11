# Command & Query Examples

This guide walks through the Phase 2 command/query surfaces and mirrors the runnable sample under `examples/commands`. The goal is to demonstrate how go-users can be wired without any HTTP transports by relying on the shared `users.Service` entry point.

## Service Construction

```go
repo := newMemoryAuthRepo() // implements types.AuthRepository + types.UserInventoryRepository
svc := users.New(users.Config{
    AuthRepository:      repo,
    InventoryRepository: repo,
    RoleRegistry:        noopRoleRegistry{},
    ActivitySink:        stdoutActivitySink{},
    Hooks: types.Hooks{
        AfterLifecycle: func(ctx context.Context, evt types.LifecycleEvent) {
            log.Printf("[hook] lifecycle %s -> %s", evt.FromState, evt.ToState)
        },
    },
    ScopeResolver: types.ScopeResolverFunc(func(_ context.Context, actor types.ActorRef, scope types.ScopeFilter) (types.ScopeFilter, error) {
        if scope.TenantID == uuid.Nil {
            scope.TenantID = lookupTenantFromActor(actor)
        }
        return scope, nil
    }),
    AuthorizationPolicy: types.AuthorizationPolicyFunc(func(_ context.Context, check types.PolicyCheck) error {
        if expected := lookupTenantFromActor(check.Actor); expected != uuid.Nil && check.Scope.TenantID != expected {
            return types.ErrUnauthorizedScope
        }
        return nil
    }),
})
```

The service validates that the auth repository, role registry, activity sink, and user-inventory repository are present during `HealthCheck`. Hooks, clock, logger, transition policy, and ID generator default to no-op/system implementations if omitted.

`lookupTenantFromActor` represents whatever helper your host app uses to map authenticated actors to tenant/org scopes (JWT claims, session data, etc.).

## Lifecycle & Invite Commands

```go
actor := types.ActorRef{ID: uuid.New(), Type: "admin"}
invite := &command.UserInviteResult{}
if err := svc.Commands().UserInvite.Execute(ctx, command.UserInviteInput{
    Email: "sample@example.com",
    Actor: actor,
    Result: invite,
}); err != nil {
    log.Fatal(err)
}

if err := svc.Commands().UserLifecycleTransition.Execute(ctx, command.UserLifecycleTransitionInput{
    UserID: invite.User.ID,
    Target: types.LifecycleStateActive,
    Actor:  actor,
    Scope:  types.ScopeFilter{TenantID: tenantID},
    Reason: "activate from CLI",
}); err != nil {
    log.Fatal(err)
}
```

- `UserInvite` creates a pending user, generates a deterministic token, stores invite metadata, and logs the event to the activity sink/hook.
- `UserLifecycleTransition` enforces the configured transition policy, calls the upstream auth repository, and emits lifecycle + activity hooks. `BulkUserTransition` reuses the same handler for batches.
- `UserPasswordReset` wraps `AuthRepository.ResetPassword`, guards required inputs, and logs a `user.password.reset` activity record.

## Inventory Query

```go
page, err := svc.Queries().UserInventory.Query(ctx, types.UserInventoryFilter{
    Actor: actor,
    Scope: types.ScopeFilter{TenantID: tenantID},
    Statuses: []types.LifecycleState{types.LifecycleStateActive},
    Pagination: types.Pagination{Limit: 25},
    Keyword: "sample",
})
if err != nil {
    log.Fatal(err)
}
fmt.Printf("found %d/%d users\n", len(page.Users), page.Total)
```

`UserInventoryQuery` clamps pagination values (default limit=50, max=200) and forwards the normalized `types.UserInventoryFilter` to whichever repository was provided. The repository contract supports tenant/org scope, lifecycle status filters, role filters, keyword search, and bulk ID selection for admin-facing dashboards.

## Role Registry & Assignments

```go
role := &types.RoleDefinition{}
if err := svc.Commands().CreateRole.Execute(ctx, command.CreateRoleInput{
    Name:  "Editors",
    Actor: actor,
    Result: role,
}); err != nil {
    log.Fatal(err)
}

if err := svc.Commands().AssignRole.Execute(ctx, command.AssignRoleInput{
    UserID: invite.User.ID,
    RoleID: role.ID,
    Actor:  actor,
}); err != nil {
    log.Fatal(err)
}

assignments, _ := svc.Queries().RoleAssignments.Query(ctx, types.RoleAssignmentFilter{
    Actor: actor,
    UserID: invite.User.ID,
})
fmt.Printf("%d assignments loaded\n", len(assignments))
```

The Bun-backed registry emits `role.*` hook events whenever roles are created, updated, deleted, or assigned. Hosts can swap the registry via `users.Config.RoleRegistry` if they store roles elsewhere, as long as the replacement implementation satisfies `types.RoleRegistry`.

## Activity Feed Query

```go
feed, err := svc.Queries().ActivityFeed.Query(ctx, types.ActivityFilter{
    Actor:      actor,
    Scope:      types.ScopeFilter{TenantID: tenantID},
    Verbs:      []string{"user.lifecycle.transition"},
    Pagination: types.Pagination{Limit: 20},
})
if err != nil {
    log.Fatal(err)
}
for _, record := range feed.Records {
    fmt.Printf("%s -> %s (%s)\n", record.Verb, record.ObjectID, record.Channel)
}
```

The default Bun activity repository implements both `types.ActivitySink` and `types.ActivityRepository`, so you can inject it once into `users.Config` and reuse it for command logging and queries. Custom storage engines simply need to satisfy the same interfaces to become drop-in replacements.

Run the end-to-end sample via:

```bash
/Users/goliatone/.g/go/bin/go run ./examples/commands
```

The console output highlights invite creation, lifecycle activation, hook ordering, and the inventory query counts.

## Admin Integration Example

`examples/admin` simulates the go-admin/go-cms wiring described in
`go-admin-architecture-updated.md`. It uses the same in-memory repositories as
the commands sample but adds:

- Multi-tenant scope guards (resolver + authorization policy) bound to actors
- `cmsBridge` that forwards go-users hooks to a fake CMS widget bus
- Dashboard widgets that call `ActivityStats`, `ActivityFeed`, and `Preferences`
  queries to render admin panels per tenant
- Role creation/assignment, profile management, and preference orchestration
  for each tenant’s admin actor

Execute the scenario with:

```bash
/Users/goliatone/.g/go/bin/go run ./examples/admin
```

The output shows widget payloads for the ops and commerce tenants plus the CMS
event bus entries triggered by hooks.

## Web Demo & schema polling

`examples/web` is the full HTTP transport that wires go-crud controllers,
`crudguard.Adapter`, `pkg/schema.Registry`, and the validation listener together.
Two routes are especially useful when validating integrations:

- `/users/:id/password-reset` showcases how to bolt custom admin actions onto
  the CRUD surface by invoking `UserPasswordReset` directly after the guard
  runs.
- `/admin/schema-demo` polls `/admin/schemas`, renders the latest OpenAPI
  snapshot, and lists recent registry events so front-end teams can confirm
  menu/action metadata before consuming it from go-cms/go-admin.
