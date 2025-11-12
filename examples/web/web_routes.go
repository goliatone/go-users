package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/goliatone/go-auth"
	"github.com/goliatone/go-router"
	"github.com/goliatone/go-router/flash"
	"github.com/goliatone/go-users/command"
	"github.com/goliatone/go-users/pkg/types"
	"github.com/goliatone/go-users/query"
	"github.com/google/uuid"
)

type preferenceView map[string]any
type inviteView map[string]any

// RegisterWebRoutes mounts all HTML web handlers
func RegisterWebRoutes(app *App) {
	cfg := app.Config().GetAuth()
	protected := app.auther.ProtectedRoute(cfg, app.auther.MakeClientRouteAuthErrorHandler(false))

	// User Inventory routes
	users := app.srv.Router().Group("/users")
	users.Get("/", renderUserInventory(app), protected)
	users.Get("/:id", renderUserDetail(app), protected)
	users.Post("/:id/transition", handleUserTransition(app), protected)
	users.Post("/:id/password-reset", handleUserPasswordReset(app), protected)

	// Activity Feed routes
	app.srv.Router().Get("/activity", renderActivityFeed(app), protected)
	app.srv.Router().Get("/activity/stats", serveActivityStats(app), protected)

	// Invites routes
	invites := app.srv.Router().Group("/invites")
	invites.Get("/", renderInvitesList(app), protected)
	invites.Get("/new", renderInviteForm(app), protected)
	invites.Post("/", handleCreateInvite(app), protected)

	// Preferences routes
	prefs := app.srv.Router().Group("/preferences")
	prefs.Get("/", renderPreferences(app), protected)
	prefs.Post("/", handleUpsertPreference(app), protected)
	prefs.Post("/:id/delete", handleDeletePreference(app), protected)

	// Profile routes
	app.srv.Router().Get("/profile", renderProfile(app), protected)
	app.srv.Router().Post("/profile", handleUpdateProfile(app), protected)

	// Role Management routes
	roles := app.srv.Router().Group("/roles")
	roles.Get("/", renderRolesList(app), protected)
	roles.Get("/:id", renderRoleDetail(app), protected)
	roles.Post("/:id/assign", handleAssignRole(app), protected)
	roles.Post("/:id/unassign", handleUnassignRole(app), protected)

	admin := app.srv.Router().Group("/admin")
	admin.Get("/schema-demo", renderSchemaDemo(app), protected)
	admin.Get("/schema-demo/feed", serveSchemaSnapshots(app), protected)

	app.GetLogger("web").Info("Web routes registered")
}

func renderHome(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		data := router.ViewContext{
			"title":   "go-users Web Demo",
			"message": "User management system demonstration",
		}
		if app.users != nil {
			if actor, err := actorFromSession(c, app); err == nil && actor.ID != uuid.Nil {
				stats, err := app.users.Queries().ActivityStats.Query(c.Context(), types.ActivityStatsFilter{
					Actor: actor,
				})
				if err == nil {
					data["stats"] = stats
					data["verb_stats"] = summarizeVerbStats(stats)
				}
			}
		}
		return renderWithGlobals(c, "index", data)
	}
}

// User Inventory handlers
func renderUserInventory(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		actor, err := actorFromSession(c, app)
		if err != nil {
			return c.Status(http.StatusInternalServerError).SendString("Invalid user ID")
		}

		page, err := app.users.Queries().UserInventory.Query(c.Context(), types.UserInventoryFilter{
			Actor:      actor,
			Pagination: types.Pagination{Limit: 25, Offset: 0},
		})
		if err != nil {
			return renderWithGlobals(c, "errors/500", router.ViewContext{
				"message": err.Error(),
			})
		}

		return renderWithGlobals(c, "users/index", router.ViewContext{
			"users": page.Users,
			"total": page.Total,
		})
	}
}

func renderUserDetail(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		id := c.Param("id", "")
		userID, err := uuid.Parse(id)
		if err != nil {
			return renderWithGlobals(c, "errors/404", router.ViewContext{
				"message": "Invalid user ID",
			})
		}

		user, err := app.repo.Users().GetByID(c.Context(), userID.String())
		if err != nil {
			return renderWithGlobals(c, "errors/404", router.ViewContext{
				"message": "User not found",
			})
		}

		return renderWithGlobals(c, "users/detail", router.ViewContext{
			"user": user,
		})
	}
}

func handleUserTransition(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		id := c.Param("id", "")
		userID, err := uuid.Parse(id)
		if err != nil {
			return flash.Redirect(c, "/users", router.ViewContext{
				"error":         true,
				"error_message": "Invalid user ID",
			})
		}

		targetState := c.FormValue("target_state")
		reason := c.FormValue("reason")

		actor, err := actorFromSession(c, app)
		if err != nil {
			return flash.Redirect(c, fmt.Sprintf("/users/%s", id), router.ViewContext{
				"error":         true,
				"error_message": "Missing session",
			})
		}

		err = app.users.Commands().UserLifecycleTransition.Execute(c.Context(), command.UserLifecycleTransitionInput{
			UserID: userID,
			Target: types.LifecycleState(targetState),
			Actor:  actor,
			Reason: reason,
		})
		if err != nil {
			return flash.Redirect(c, fmt.Sprintf("/users/%s", id), router.ViewContext{
				"error":         true,
				"error_message": fmt.Sprintf("Transition failed: %v", err),
			})
		}

		return flash.Redirect(c, fmt.Sprintf("/users/%s", id), router.ViewContext{
			"success":         true,
			"success_message": fmt.Sprintf("User transitioned to %s", targetState),
		})
	}
}

// Activity Feed handlers
func renderActivityFeed(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		actor, err := actorFromSession(c, app)
		if err != nil {
			return c.Status(http.StatusInternalServerError).SendString("Session error")
		}

		feed, err := app.users.Queries().ActivityFeed.Query(c.Context(), types.ActivityFilter{
			Actor:      actor,
			Pagination: types.Pagination{Limit: 50, Offset: 0},
		})
		if err != nil {
			return renderWithGlobals(c, "errors/500", router.ViewContext{
				"message": err.Error(),
			})
		}

		stats, err := app.users.Queries().ActivityStats.Query(c.Context(), types.ActivityStatsFilter{
			Actor: actor,
		})
		if err != nil {
			return renderWithGlobals(c, "errors/500", router.ViewContext{
				"message": err.Error(),
			})
		}

		return renderWithGlobals(c, "activity/feed", router.ViewContext{
			"activities": feed.Records,
			"total":      feed.Total,
			"stats":      stats,
			"verb_stats": summarizeVerbStats(stats),
		})
	}
}

func serveActivityStats(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		actor, err := actorFromSession(c, app)
		if err != nil {
			return c.Status(http.StatusUnauthorized).SendString("Session required")
		}
		stats, err := app.users.Queries().ActivityStats.Query(c.Context(), types.ActivityStatsFilter{
			Actor: actor,
		})
		if err != nil {
			return c.Status(http.StatusInternalServerError).SendString(err.Error())
		}
		payload := map[string]any{
			"total":    stats.Total,
			"by_verb":  stats.ByVerb,
			"verb_set": summarizeVerbStats(stats),
		}
		return writeJSON(c, payload)
	}
}

// Invites handlers
func renderInvitesList(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		actor, err := actorFromSession(c, app)
		if err != nil {
			return c.Status(http.StatusUnauthorized).SendString("Session required")
		}

		invites, err := listInvites(c, app)
		if err != nil {
			app.GetLogger("invites").Error("failed to list invites", "error", err)
			return renderWithGlobals(c, "invites/index", router.ViewContext{
				"error": "Failed to load invites",
			})
		}

		app.GetLogger("invites").Debug("invites listed", "count", len(invites), "actor", actor.ID)

		return renderWithGlobals(c, "invites/index", router.ViewContext{
			"invites": invites,
		})
	}
}

func listInvites(c router.Context, app *App) ([]inviteView, error) {
	users, _, err := app.repo.Users().List(c.Context())
	if err != nil {
		return nil, err
	}
	invites := make([]inviteView, 0)
	for _, user := range users {
		meta := user.Metadata
		inviteMeta, ok := meta["invite"].(map[string]any)
		if !ok {
			continue
		}
		expires := ""
		if str, ok := inviteMeta["expires_at"].(string); ok {
			expires = str
		}
		invites = append(invites, inviteView{
			"email":      user.Email,
			"status":     string(user.Status),
			"sent_at":    formatTimePtr(user.CreatedAt),
			"expires_at": expires,
		})
	}
	return invites, nil
}

func renderInviteForm(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		return renderWithGlobals(c, "invites/form", router.ViewContext{})
	}
}

func handleCreateInvite(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		email := c.FormValue("email")
		if email == "" {
			return renderWithGlobals(c, "invites/form", router.ViewContext{
				"error": "Email is required",
			})
		}

		actor, err := actorFromSession(c, app)
		if err != nil {
			return renderWithGlobals(c, "invites/form", router.ViewContext{
				"error": "Authentication required",
				"email": email,
			})
		}

		result := &command.UserInviteResult{}
		err = app.users.Commands().UserInvite.Execute(c.Context(), command.UserInviteInput{
			Email:  email,
			Actor:  actor,
			Result: result,
		})
		if err != nil {
			return renderWithGlobals(c, "invites/form", router.ViewContext{
				"error": fmt.Sprintf("Failed to create invite: %v", err),
				"email": email,
			})
		}

		return flash.Redirect(c, "/invites", router.ViewContext{
			"success":         true,
			"success_message": fmt.Sprintf("Invite sent to %s", email),
		})
	}
}

// Preferences handlers
func renderPreferences(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		actor, err := actorFromSession(c, app)
		if err != nil {
			return c.Status(http.StatusInternalServerError).SendString("Session error")
		}

		input := query.PreferenceQueryInput{
			Actor:  actor,
			UserID: actor.ID,
		}

		snapshot, err := app.users.Queries().Preferences.Query(c.Context(), input)
		if err != nil {
			return renderWithGlobals(c, "errors/500", router.ViewContext{
				"message": err.Error(),
			})
		}

		prefs := buildPreferenceViews(snapshot)

		return renderWithGlobals(c, "preferences/index", router.ViewContext{
			"preferences": prefs,
		})
	}
}

func buildPreferenceViews(snapshot types.PreferenceSnapshot) []preferenceView {
	prefs := make([]preferenceView, 0, len(snapshot.Effective))
	for key, val := range snapshot.Effective {
		prefs = append(prefs, preferenceView{
			"key":         key,
			"scope_level": preferenceScopeLevel(snapshot.Traces, key),
			"value":       val,
		})
	}
	sort.Slice(prefs, func(i, j int) bool {
		return prefs[i]["key"].(string) < prefs[j]["key"].(string)
	})
	return prefs
}

func preferenceScopeLevel(traces []types.PreferenceTrace, key string) string {
	for _, trace := range traces {
		if trace.Key != key {
			continue
		}
		for _, layer := range trace.Layers {
			if layer.Found {
				return string(layer.Level)
			}
		}
		break
	}
	return ""
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

func handleUpsertPreference(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		key := c.FormValue("key")
		value := c.FormValue("value")

		actor, err := actorFromSession(c, app)
		if err != nil {
			return flash.Redirect(c, "/preferences", router.ViewContext{
				"error":         true,
				"error_message": "Session required",
			})
		}

		err = app.users.Commands().PreferenceUpsert.Execute(c.Context(), command.PreferenceUpsertInput{
			UserID: actor.ID,
			Key:    key,
			Value:  map[string]any{"data": value},
			Actor:  actor,
		})
		if err != nil {
			return flash.Redirect(c, "/preferences", router.ViewContext{
				"error":         true,
				"error_message": fmt.Sprintf("Failed to save preference: %v", err),
			})
		}

		return flash.Redirect(c, "/preferences", router.ViewContext{
			"success":         true,
			"success_message": "Preference saved",
		})
	}
}

func handleDeletePreference(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		key := c.Param("id", "") // This is actually the preference key

		actor, err := actorFromSession(c, app)
		if err != nil {
			return flash.Redirect(c, "/preferences", router.ViewContext{
				"error":         true,
				"error_message": "Session required",
			})
		}

		err = app.users.Commands().PreferenceDelete.Execute(c.Context(), command.PreferenceDeleteInput{
			UserID: actor.ID,
			Key:    key,
			Actor:  actor,
		})
		if err != nil {
			return flash.Redirect(c, "/preferences", router.ViewContext{
				"error":         true,
				"error_message": fmt.Sprintf("Failed to delete preference: %v", err),
			})
		}

		return flash.Redirect(c, "/preferences", router.ViewContext{
			"success":         true,
			"success_message": "Preference deleted",
		})
	}
}

// Profile handlers
func renderProfile(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		actor, err := actorFromSession(c, app)
		if err != nil {
			return c.Status(http.StatusInternalServerError).SendString("Session error")
		}

		input := query.ProfileQueryInput{
			Actor:  actor,
			UserID: actor.ID,
		}

		profileData, err := app.users.Queries().ProfileDetail.Query(c.Context(), input)
		if err != nil {
			return renderWithGlobals(c, "errors/500", router.ViewContext{
				"message": err.Error(),
			})
		}

		return renderWithGlobals(c, "profile/detail", router.ViewContext{
			"profile": profileData,
		})
	}
}

func handleUpdateProfile(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		displayName := c.FormValue("display_name")
		avatarURL := c.FormValue("avatar_url")
		bio := c.FormValue("bio")

		actor, err := actorFromSession(c, app)
		if err != nil {
			return flash.Redirect(c, "/profile", router.ViewContext{
				"error":         true,
				"error_message": "Session required",
			})
		}

		err = app.users.Commands().ProfileUpsert.Execute(c.Context(), command.ProfileUpsertInput{
			UserID: actor.ID,
			Patch: types.ProfilePatch{
				DisplayName: &displayName,
				AvatarURL:   &avatarURL,
				Bio:         &bio,
			},
			Actor: actor,
		})
		if err != nil {
			return flash.Redirect(c, "/profile", router.ViewContext{
				"error":         true,
				"error_message": fmt.Sprintf("Failed to update profile: %v", err),
			})
		}

		return flash.Redirect(c, "/profile", router.ViewContext{
			"success":         true,
			"success_message": "Profile updated",
		})
	}
}

// Role Management handlers
func renderRolesList(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		actor, err := actorFromSession(c, app)
		if err != nil {
			return c.Status(http.StatusInternalServerError).SendString("Session error")
		}

		rolePage, err := app.users.Queries().RoleList.Query(c.Context(), types.RoleFilter{
			Actor: actor,
		})
		if err != nil {
			return renderWithGlobals(c, "errors/500", router.ViewContext{
				"message": err.Error(),
			})
		}

		return renderWithGlobals(c, "roles/index", router.ViewContext{
			"roles": rolePage.Roles,
			"total": rolePage.Total,
		})
	}
}

func renderRoleDetail(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		id := c.Param("id", "")
		roleID, err := uuid.Parse(id)
		if err != nil {
			return renderWithGlobals(c, "errors/404", router.ViewContext{
				"message": "Invalid role ID",
			})
		}

		actor, err := actorFromSession(c, app)
		if err != nil {
			return renderWithGlobals(c, "errors/404", router.ViewContext{
				"message": "Session required",
			})
		}

		input := query.RoleDetailInput{
			Actor:  actor,
			RoleID: roleID,
		}

		roleData, err := app.users.Queries().RoleDetail.Query(c.Context(), input)
		if err != nil {
			return renderWithGlobals(c, "errors/404", router.ViewContext{
				"message": "Role not found",
			})
		}

		return renderWithGlobals(c, "roles/detail", router.ViewContext{
			"role": roleData,
		})
	}
}

func handleAssignRole(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		roleID := c.Param("id", "")
		userIDStr := c.FormValue("user_id")

		rid, _ := uuid.Parse(roleID)
		uid, err := uuid.Parse(userIDStr)
		if err != nil {
			return flash.Redirect(c, fmt.Sprintf("/roles/%s", roleID), router.ViewContext{
				"error":         true,
				"error_message": "Invalid user ID",
			})
		}

		actor, err := actorFromSession(c, app)
		if err != nil {
			return flash.Redirect(c, fmt.Sprintf("/roles/%s", roleID), router.ViewContext{
				"error":         true,
				"error_message": "Session required",
			})
		}

		err = app.users.Commands().AssignRole.Execute(c.Context(), command.AssignRoleInput{
			UserID: uid,
			RoleID: rid,
			Actor:  actor,
		})
		if err != nil {
			return flash.Redirect(c, fmt.Sprintf("/roles/%s", roleID), router.ViewContext{
				"error":         true,
				"error_message": fmt.Sprintf("Failed to assign role: %v", err),
			})
		}

		return flash.Redirect(c, fmt.Sprintf("/roles/%s", roleID), router.ViewContext{
			"success":         true,
			"success_message": "Role assigned",
		})
	}
}

func handleUnassignRole(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		roleID := c.Param("id", "")
		userIDStr := c.FormValue("user_id")

		rid, _ := uuid.Parse(roleID)
		uid, err := uuid.Parse(userIDStr)
		if err != nil {
			return flash.Redirect(c, fmt.Sprintf("/roles/%s", roleID), router.ViewContext{
				"error":         true,
				"error_message": "Invalid user ID",
			})
		}

		actor, err := actorFromSession(c, app)
		if err != nil {
			return flash.Redirect(c, fmt.Sprintf("/roles/%s", roleID), router.ViewContext{
				"error":         true,
				"error_message": "Session required",
			})
		}

		err = app.users.Commands().UnassignRole.Execute(c.Context(), command.UnassignRoleInput{
			UserID: uid,
			RoleID: rid,
			Actor:  actor,
		})
		if err != nil {
			return flash.Redirect(c, fmt.Sprintf("/roles/%s", roleID), router.ViewContext{
				"error":         true,
				"error_message": fmt.Sprintf("Failed to unassign role: %v", err),
			})
		}

		return flash.Redirect(c, fmt.Sprintf("/roles/%s", roleID), router.ViewContext{
			"success":         true,
			"success_message": "Role unassigned",
		})
	}
}

func handleUserPasswordReset(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		id := c.Param("id", "")
		userID, err := uuid.Parse(id)
		if err != nil {
			return flash.Redirect(c, "/users", router.ViewContext{
				"error":         true,
				"error_message": "Invalid user ID",
			})
		}

		actor, err := actorFromSession(c, app)
		if err != nil {
			return flash.Redirect(c, fmt.Sprintf("/users/%s", id), router.ViewContext{
				"error":         true,
				"error_message": "Missing session",
			})
		}

		err = app.users.Commands().UserPasswordReset.Execute(c.Context(), command.UserPasswordResetInput{
			UserID: userID,
			Actor:  actor,
		})
		if err != nil {
			return flash.Redirect(c, fmt.Sprintf("/users/%s", id), router.ViewContext{
				"error":         true,
				"error_message": fmt.Sprintf("Password reset failed: %v", err),
			})
		}

		return flash.Redirect(c, fmt.Sprintf("/users/%s", id), router.ViewContext{
			"success":         true,
			"success_message": "Password reset requested",
		})
	}
}

func renderSchemaDemo(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		if app.schemaFeed == nil {
			return renderWithGlobals(c, "admin/schema_demo", router.ViewContext{
				"has_schema": false,
			})
		}

		latest, ok := app.schemaFeed.Latest()
		history := app.schemaFeed.History()
		latestJSON := ""
		if ok {
			latestJSON = prettyJSON(latest.Document)
		}

		return renderWithGlobals(c, "admin/schema_demo", router.ViewContext{
			"has_schema":  ok,
			"latest":      latest,
			"latest_json": latestJSON,
			"history":     history,
		})
	}
}

func serveSchemaSnapshots(app *App) router.HandlerFunc {
	return func(c router.Context) error {
		if app.schemaFeed == nil {
			return c.NoContent(http.StatusNoContent)
		}
		history := app.schemaFeed.History()
		dto := make([]map[string]any, 0, len(history))
		for _, snap := range history {
			dto = append(dto, map[string]any{
				"generated_at":   snap.GeneratedAt,
				"resource_names": snap.ResourceNames,
			})
		}
		return writeJSON(c, dto)
	}
}

func prettyJSON(doc map[string]any) string {
	if len(doc) == 0 {
		return ""
	}
	payload, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return ""
	}
	return string(payload)
}

func writeJSON(ctx router.Context, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return ctx.Status(http.StatusInternalServerError).SendString("failed to marshal JSON")
	}
	ctx.SetHeader("Content-Type", "application/json")
	return ctx.Status(http.StatusOK).Send(data)
}

type verbStat struct {
	Verb  string
	Count int
}

func summarizeVerbStats(stats types.ActivityStats) []verbStat {
	out := make([]verbStat, 0, len(stats.ByVerb))
	for verb, count := range stats.ByVerb {
		out = append(out, verbStat{Verb: verb, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Verb < out[j].Verb
		}
		return out[i].Count > out[j].Count
	})
	return out
}

func actorFromSession(c router.Context, app *App) (types.ActorRef, error) {
	session, err := auth.GetRouterSession(c, app.Config().GetAuth().GetContextKey())
	if err != nil {
		return types.ActorRef{}, err
	}
	actorID, err := uuid.Parse(session.GetUserID())
	if err != nil {
		return types.ActorRef{}, err
	}
	return types.ActorRef{ID: actorID, Type: "user"}, nil
}
