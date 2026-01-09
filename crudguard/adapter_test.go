package crudguard

import (
	"context"
	"testing"
	"time"

	auth "github.com/goliatone/go-auth"
	"github.com/goliatone/go-crud"
	goerrors "github.com/goliatone/go-errors"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
)

func TestAdapterEnforceRunsGuard(t *testing.T) {
	guard := &stubGuard{
		result:    types.ScopeFilter{TenantID: uuid.New()},
		useResult: true,
	}
	adapter := newTestAdapter(t, guard)

	actorCtx := &auth.ActorContext{
		ActorID:  uuid.NewString(),
		Role:     "admin",
		TenantID: uuid.NewString(),
	}
	ctx := newStubCrudContext(auth.WithActorContext(context.Background(), actorCtx))
	result, err := adapter.Enforce(GuardInput{
		Context:   ctx,
		Operation: crud.OpList,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !guard.called {
		t.Fatal("expected guard to be called")
	}
	if guard.lastAction != types.PolicyActionUsersRead {
		t.Fatalf("expected action %s, got %s", types.PolicyActionUsersRead, guard.lastAction)
	}
	if result.Scope.TenantID != guard.result.TenantID {
		t.Fatalf("expected resolved scope to match guard result")
	}
}

func TestAdapterEnforceBypassSkipsGuard(t *testing.T) {
	guard := &stubGuard{}
	adapter := newTestAdapter(t, guard)
	actorCtx := &auth.ActorContext{ActorID: uuid.NewString(), Role: "admin"}
	ctx := newStubCrudContext(auth.WithActorContext(context.Background(), actorCtx))

	result, err := adapter.Enforce(GuardInput{
		Context:   ctx,
		Operation: crud.OpRead,
		Bypass: &BypassConfig{
			Enabled: true,
			Reason:  "schema export",
		},
	})
	if err != nil {
		t.Fatalf("expected bypass to succeed, got %v", err)
	}
	if guard.called {
		t.Fatal("expected guard not to be called when bypass active")
	}
	if !result.Bypassed {
		t.Fatal("expected bypass flag in result")
	}
	if result.BypassReason != "schema export" {
		t.Fatalf("expected bypass reason to propagate, got %s", result.BypassReason)
	}
}

func TestAdapterMissingActorReturnsError(t *testing.T) {
	guard := &stubGuard{}
	adapter := newTestAdapter(t, guard)
	_, err := adapter.Enforce(GuardInput{
		Context:   newStubCrudContext(context.Background()),
		Operation: crud.OpRead,
	})
	if err == nil {
		t.Fatal("expected error when actor context missing")
	}
	var richErr *goerrors.Error
	if !goerrors.As(err, &richErr) {
		t.Fatalf("expected go-errors.Error, got %T", err)
	}
	if richErr.TextCode != "ACTOR_CONTEXT_MISSING" {
		t.Fatalf("expected text code ACTOR_CONTEXT_MISSING, got %s", richErr.TextCode)
	}
}

func TestAdapterFallsBackToClaims(t *testing.T) {
	guard := &stubGuard{}
	adapter := newTestAdapter(t, guard)

	actorID := uuid.New()
	claims := &testClaims{
		subject:  actorID.String(),
		uid:      actorID.String(),
		role:     "user",
		metadata: map[string]any{"tenant_id": uuid.New().String()},
	}
	ctx := auth.WithClaimsContext(context.Background(), claims)

	_, err := adapter.Enforce(GuardInput{
		Context:   newStubCrudContext(ctx),
		Operation: crud.OpRead,
	})
	if err != nil {
		t.Fatalf("expected fallback to claims, got %v", err)
	}
	if !guard.called {
		t.Fatal("expected guard to run")
	}
}

func TestAdapterWrapsUnauthorizedScope(t *testing.T) {
	guard := &stubGuard{
		err: types.ErrUnauthorizedScope,
	}
	adapter := newTestAdapter(t, guard)
	actorCtx := &auth.ActorContext{ActorID: uuid.NewString(), Role: "admin"}
	ctx := newStubCrudContext(auth.WithActorContext(context.Background(), actorCtx))

	_, err := adapter.Enforce(GuardInput{
		Context:   ctx,
		Operation: crud.OpDelete,
	})
	if err == nil {
		t.Fatal("expected scope enforcement failure")
	}
	var richErr *goerrors.Error
	if !goerrors.As(err, &richErr) {
		t.Fatalf("expected go-errors.Error, got %T", err)
	}
	if richErr.TextCode != textCodeScopeDenied {
		t.Fatalf("expected text code %s, got %s", textCodeScopeDenied, richErr.TextCode)
	}
}

// helpers

type stubGuard struct {
	result        types.ScopeFilter
	err           error
	called        bool
	lastAction    types.PolicyAction
	lastRequested types.ScopeFilter
	useResult     bool
}

func (s *stubGuard) Enforce(ctx context.Context, actor types.ActorRef, requested types.ScopeFilter, action types.PolicyAction, target uuid.UUID) (types.ScopeFilter, error) {
	s.called = true
	s.lastAction = action
	s.lastRequested = requested
	if s.err != nil {
		return types.ScopeFilter{}, s.err
	}
	if s.useResult {
		return s.result.Clone(), nil
	}
	return requested, nil
}

func newTestAdapter(t *testing.T, guard scope.Guard) *Adapter {
	t.Helper()
	policyMap := DefaultPolicyMap(types.PolicyActionUsersRead, types.PolicyActionUsersWrite)
	adapter, err := NewAdapter(Config{
		Guard:          guard,
		Logger:         types.NopLogger{},
		PolicyMap:      policyMap,
		ScopeExtractor: DefaultScopeExtractor,
	})
	if err != nil {
		t.Fatalf("unexpected adapter construction error: %v", err)
	}
	return adapter
}

type stubCrudContext struct {
	ctx     context.Context
	status  int
	body    []byte
	queries map[string]string
}

func newStubCrudContext(ctx context.Context) *stubCrudContext {
	return &stubCrudContext{
		ctx:     ctx,
		queries: map[string]string{},
	}
}

func (s *stubCrudContext) UserContext() context.Context {
	return s.ctx
}

func (s *stubCrudContext) Params(key string, defaultValue ...string) string {
	return ""
}

func (s *stubCrudContext) BodyParser(out any) error {
	return nil
}

func (s *stubCrudContext) Query(key string, defaultValue ...string) string {
	if v, ok := s.queries[key]; ok {
		return v
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

func (s *stubCrudContext) QueryValues(key string) []string {
	if v, ok := s.queries[key]; ok {
		return []string{v}
	}
	return nil
}

func (s *stubCrudContext) QueryInt(key string, defaultValue ...int) int {
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return 0
}

func (s *stubCrudContext) Queries() map[string]string {
	return s.queries
}

func (s *stubCrudContext) Body() []byte {
	return s.body
}

func (s *stubCrudContext) Status(status int) crud.Response {
	s.status = status
	return s
}

func (s *stubCrudContext) JSON(data any, ctype ...string) error {
	return nil
}

func (s *stubCrudContext) SendStatus(status int) error {
	s.status = status
	return nil
}

type testClaims struct {
	subject  string
	uid      string
	role     string
	metadata map[string]any
	res      map[string]string
}

func (t *testClaims) Subject() string                  { return t.subject }
func (t *testClaims) UserID() string                   { return t.uid }
func (t *testClaims) Role() string                     { return t.role }
func (t *testClaims) CanRead(string) bool              { return true }
func (t *testClaims) CanEdit(string) bool              { return true }
func (t *testClaims) CanCreate(string) bool            { return true }
func (t *testClaims) CanDelete(string) bool            { return true }
func (t *testClaims) HasRole(role string) bool         { return t.role == role }
func (t *testClaims) IsAtLeast(string) bool            { return true }
func (t *testClaims) Expires() time.Time               { return time.Time{} }
func (t *testClaims) IssuedAt() time.Time              { return time.Time{} }
func (t *testClaims) ResourceRoles() map[string]string { return t.res }
func (t *testClaims) ClaimsMetadata() map[string]any   { return t.metadata }
