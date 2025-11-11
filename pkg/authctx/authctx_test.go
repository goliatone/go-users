package authctx

import (
	"context"
	"testing"
	"time"

	auth "github.com/goliatone/go-auth"
	"github.com/goliatone/go-errors"
	"github.com/google/uuid"
)

func TestResolveActorContextPrefersStoredActor(t *testing.T) {
	ctx := context.Background()
	expected := &auth.ActorContext{
		ActorID:        uuid.NewString(),
		Role:           "admin",
		TenantID:       uuid.NewString(),
		OrganizationID: uuid.NewString(),
	}
	ctx = auth.WithActorContext(ctx, expected)

	actual, err := ResolveActorContext(ctx)
	if err != nil {
		t.Fatalf("ResolveActorContext returned error: %v", err)
	}
	if actual.ActorID != expected.ActorID {
		t.Fatalf("expected actor %s, got %s", expected.ActorID, actual.ActorID)
	}
}

func TestResolveActorContextFallsBackToClaims(t *testing.T) {
	ctx := context.Background()
	actorID := uuid.New().String()
	tenantID := uuid.New().String()
	claims := &stubClaims{
		subject:  actorID,
		uid:      actorID,
		role:     "owner",
		metadata: map[string]any{"tenant_id": tenantID},
	}
	ctx = auth.WithClaimsContext(ctx, claims)

	actual, err := ResolveActorContext(ctx)
	if err != nil {
		t.Fatalf("expected fallback to claims, got error: %v", err)
	}
	if actual.ActorID != actorID {
		t.Fatalf("expected actor %s, got %s", actorID, actual.ActorID)
	}
	if actual.TenantID != tenantID {
		t.Fatalf("expected tenant %s, got %s", tenantID, actual.TenantID)
	}
}

func TestResolveActorContextMissingReturnsRichError(t *testing.T) {
	_, err := ResolveActorContext(context.Background())
	if err == nil {
		t.Fatal("expected error when context lacks auth metadata")
	}
	var richErr *errors.Error
	if !errors.As(err, &richErr) {
		t.Fatalf("expected go-errors.Error, got %T", err)
	}
	if richErr.TextCode != textCodeActorMissing {
		t.Fatalf("expected text code %s, got %s", textCodeActorMissing, richErr.TextCode)
	}
}

func TestActorRefFromActorContext(t *testing.T) {
	id := uuid.New()
	ref, err := ActorRefFromActorContext(&auth.ActorContext{
		ActorID: id.String(),
		Role:    "admin",
	})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if ref.ID != id {
		t.Fatalf("expected id %s, got %s", id, ref.ID)
	}
	if ref.Type != "admin" {
		t.Fatalf("expected type admin, got %s", ref.Type)
	}
}

func TestActorRefFromActorContextInvalidID(t *testing.T) {
	_, err := ActorRefFromActorContext(&auth.ActorContext{
		ActorID: "not-a-uuid",
	})
	if err == nil {
		t.Fatal("expected error for invalid actor id")
	}
	var richErr *errors.Error
	if !errors.As(err, &richErr) {
		t.Fatalf("expected go-errors.Error, got %T", err)
	}
	if richErr.TextCode != textCodeActorInvalid {
		t.Fatalf("expected text code %s, got %s", textCodeActorInvalid, richErr.TextCode)
	}
}

func TestScopeFromActorContextParsesUUIDs(t *testing.T) {
	tenant := uuid.New()
	org := uuid.New()
	scope := ScopeFromActorContext(&auth.ActorContext{
		TenantID:       tenant.String(),
		OrganizationID: org.String(),
	})
	if scope.TenantID != tenant {
		t.Fatalf("expected tenant %s, got %s", tenant, scope.TenantID)
	}
	if scope.OrgID != org {
		t.Fatalf("expected org %s, got %s", org, scope.OrgID)
	}
}

type stubClaims struct {
	subject  string
	uid      string
	role     string
	metadata map[string]any
	res      map[string]string
}

func (s *stubClaims) Subject() string                  { return s.subject }
func (s *stubClaims) UserID() string                   { return s.uid }
func (s *stubClaims) Role() string                     { return s.role }
func (s *stubClaims) CanRead(string) bool              { return true }
func (s *stubClaims) CanEdit(string) bool              { return true }
func (s *stubClaims) CanCreate(string) bool            { return true }
func (s *stubClaims) CanDelete(string) bool            { return true }
func (s *stubClaims) HasRole(role string) bool         { return s.role == role }
func (s *stubClaims) IsAtLeast(string) bool            { return true }
func (s *stubClaims) Expires() time.Time               { return time.Time{} }
func (s *stubClaims) IssuedAt() time.Time              { return time.Time{} }
func (s *stubClaims) ResourceRoles() map[string]string { return s.res }
func (s *stubClaims) ClaimsMetadata() map[string]any   { return s.metadata }
