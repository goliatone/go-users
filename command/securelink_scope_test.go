package command

import (
	"context"
	"testing"
	"time"

	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/scope"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestSecureLinkIssuanceRejectsLabelsBeforeSideEffects(t *testing.T) {
	for _, resolved := range []bool{false, true} {
		t.Run(map[bool]string{false: "requested", true: "resolved"}[resolved], func(t *testing.T) {
			repo, tokens, resets := newFakeAuthRepo(), newMemoryTokenRepo(), newMemoryResetRepo()
			manager := &stubSecureLinkManager{}
			labeled := types.ScopeFilter{}.WithLabel("group_id", uuid.New())
			requested := labeled
			var guard scope.Guard
			if resolved {
				requested = types.ScopeFilter{}
				guard = scope.NewGuard(types.ScopeResolverFunc(func(context.Context, types.ActorRef, types.ScopeFilter) (types.ScopeFilter, error) {
					return labeled, nil
				}), nil)
			}
			actor := types.ActorRef{ID: uuid.New()}
			invite := NewUserInviteCommand(InviteCommandConfig{Repository: repo, TokenRepository: tokens, SecureLinks: manager, ScopeGuard: guard})
			require.ErrorIs(t, invite.Execute(context.Background(), UserInviteInput{Email: "a@example.com", Scope: requested, Actor: actor}), types.ErrUnsupportedScopeLabels)
			registration := NewUserRegistrationRequestCommand(RegistrationRequestConfig{Repository: repo, TokenRepository: tokens, SecureLinks: manager, ScopeGuard: guard})
			require.ErrorIs(t, registration.Execute(context.Background(), UserRegistrationRequestInput{Email: "a@example.com", Scope: requested, Actor: actor}), types.ErrUnsupportedScopeLabels)
			reset := NewUserPasswordResetRequestCommand(PasswordResetRequestConfig{Repository: repo, ResetRepository: resets, SecureLinks: manager})
			require.ErrorIs(t, reset.Execute(context.Background(), UserPasswordResetRequestInput{UserID: uuid.New(), Scope: labeled}), types.ErrUnsupportedScopeLabels)
			require.Nil(t, repo.lastCreated)
			require.Empty(t, tokens.tokens)
			require.Empty(t, resets.resets)
			require.Empty(t, manager.lastPayloads)
		})
	}
}

func TestSecureLinkValidationRejectsLabelRestoration(t *testing.T) {
	ctx := context.Background()
	for _, payloadLabels := range []bool{false, true} {
		tokens := newMemoryTokenRepo()
		manager := &stubSecureLinkManager{validatePayload: types.SecureLinkPayload{"jti": "missing"}}
		requested := types.ScopeFilter{}.WithLabel("group_id", uuid.New())
		if payloadLabels {
			requested = types.ScopeFilter{}
			manager.validatePayload["scope_labels"] = map[string]any{"group_id": uuid.New().String()}
		}
		validate := NewUserTokenValidateCommand(TokenValidateConfig{TokenRepository: tokens, SecureLinks: manager})
		require.ErrorIs(t, validate.Execute(ctx, UserTokenValidateInput{Token: "signed", TokenType: types.UserTokenInvite, Scope: requested}), types.ErrUnsupportedScopeLabels)
		consume := NewUserTokenConsumeCommand(TokenConsumeConfig{TokenRepository: tokens, SecureLinks: manager})
		require.ErrorIs(t, consume.Execute(ctx, UserTokenConsumeInput{Token: "signed", TokenType: types.UserTokenInvite, Scope: requested}), types.ErrUnsupportedScopeLabels)
		confirm := NewUserPasswordResetConfirmCommand(PasswordResetConfirmConfig{ResetRepository: newMemoryResetRepo(), SecureLinks: manager, ResetCommand: &UserPasswordResetCommand{}})
		require.ErrorIs(t, confirm.Execute(ctx, UserPasswordResetConfirmInput{Token: "signed", NewPasswordHash: "hash", Scope: requested}), types.ErrUnsupportedScopeLabels)
	}
	_, err := buildSecureLinkPayload(SecureLinkActionInvite, nil, types.ScopeFilter{}.WithLabel("group_id", uuid.New()), "id", time.Time{}, time.Time{}, "")
	require.ErrorIs(t, err, types.ErrUnsupportedScopeLabels)
}
