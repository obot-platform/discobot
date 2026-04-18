package middleware

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/service"
	"github.com/obot-platform/discobot/server/internal/store"
)

type contextKey string

const (
	UserKey      contextKey = "user"
	UserIDKey    contextKey = "userID"
	UserEmailKey contextKey = "userEmail"
)

const sessionCookieName = "discobot_session"
const desktopSecretCookieName = "discobot_secret"

// DesktopShellAuth middleware validates the desktop shell secret from cookie or
// query string.
// Only active when cfg.DesktopMode is true.
// Rejects requests without valid secret with 401 Unauthorized.
// Checks both cookie and ?token= query parameter for flexibility with WebSocket/SSE.
//
// Two paths are exempt from auth because they are called without a desktop shell
// session:
//   - MCP OAuth callbacks (/sessions/.../mcp/.../callback) — browser redirects from
//     external OAuth servers that cannot carry the desktop shell session cookie.
//   - MCP token persistence (/api/projects/.../credentials/mcp) — POST from agent
//     containers that have no access to the desktop shell session cookie.
func DesktopShellAuth(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip if not in desktop shell mode
			if !cfg.DesktopMode {
				next.ServeHTTP(w, r)
				return
			}

			// MCP OAuth callbacks arrive from external browsers that cannot carry
			// the desktop shell secret cookie. Pattern: /sessions/{id}/mcp/{name}/callback
			if strings.HasPrefix(r.URL.Path, "/sessions/") && strings.HasSuffix(r.URL.Path, "/callback") {
				next.ServeHTTP(w, r)
				return
			}

			// MCP token POST from agent containers (no desktop shell cookie available in-container).
			// Pattern: POST /api/projects/{id}/credentials/mcp
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/credentials/mcp") {
				next.ServeHTTP(w, r)
				return
			}

			var secret string

			// First check query parameter (for WebSocket/SSE URLs)
			if token := r.URL.Query().Get("token"); token != "" {
				secret = token
			} else if cookie, err := r.Cookie(desktopSecretCookieName); err == nil {
				// Fall back to cookie
				secret = cookie.Value
			}

			if secret == "" {
				http.Error(w, `{"error":"Desktop shell authentication required"}`, http.StatusUnauthorized)
				return
			}

			// Constant-time comparison to prevent timing attacks
			if subtle.ConstantTimeCompare([]byte(secret), []byte(cfg.DesktopSecret)) != 1 {
				http.Error(w, `{"error":"Invalid desktop shell secret"}`, http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// TauriAuth is a backward-compatible alias for DesktopShellAuth.
func TauriAuth(cfg *config.Config) func(http.Handler) http.Handler {
	return DesktopShellAuth(cfg)
}

// Auth middleware validates user authentication.
// If auth is disabled (cfg.AuthEnabled == false), it uses the anonymous user.
func Auth(s *store.Store, cfg *config.Config) func(http.Handler) http.Handler {
	authService := service.NewAuthService(s, cfg)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// If auth is disabled, use anonymous user
			if !cfg.AuthEnabled {
				anonUser := &service.User{
					ID:    model.AnonymousUserID,
					Email: model.AnonymousUserEmail,
					Name:  model.AnonymousUserName,
				}
				ctx := context.WithValue(r.Context(), UserKey, anonUser)
				ctx = context.WithValue(ctx, UserIDKey, anonUser.ID)
				ctx = context.WithValue(ctx, UserEmailKey, anonUser.Email)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Get session cookie
			cookie, err := r.Cookie(sessionCookieName)
			if err != nil {
				http.Error(w, `{"error":"Authentication required"}`, http.StatusUnauthorized)
				return
			}

			// Validate session
			user, err := authService.ValidateSession(r.Context(), cookie.Value)
			if err != nil {
				// Clear invalid cookie
				http.SetCookie(w, &http.Cookie{
					Name:     sessionCookieName,
					Value:    "",
					Path:     "/",
					HttpOnly: true,
					MaxAge:   -1,
				})
				http.Error(w, `{"error":"Session expired"}`, http.StatusUnauthorized)
				return
			}

			// Add user info to context
			ctx := context.WithValue(r.Context(), UserKey, user)
			ctx = context.WithValue(ctx, UserIDKey, user.ID)
			ctx = context.WithValue(ctx, UserEmailKey, user.Email)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUser extracts user from context
func GetUser(ctx context.Context) *service.User {
	if user, ok := ctx.Value(UserKey).(*service.User); ok {
		return user
	}
	return nil
}

// GetUserID extracts user ID from context
func GetUserID(ctx context.Context) string {
	if id, ok := ctx.Value(UserIDKey).(string); ok {
		return id
	}
	return ""
}

// GetUserEmail extracts user email from context
func GetUserEmail(ctx context.Context) string {
	if email, ok := ctx.Value(UserEmailKey).(string); ok {
		return email
	}
	return ""
}

// OptionalAuth middleware allows unauthenticated requests but adds user info if authenticated
func OptionalAuth(s *store.Store, cfg *config.Config) func(http.Handler) http.Handler {
	authService := service.NewAuthService(s, cfg)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get session cookie
			cookie, err := r.Cookie(sessionCookieName)
			if err != nil {
				// No cookie, continue without auth
				next.ServeHTTP(w, r)
				return
			}

			// Validate session
			user, err := authService.ValidateSession(r.Context(), cookie.Value)
			if err != nil {
				// Invalid session, continue without auth
				next.ServeHTTP(w, r)
				return
			}

			// Add user info to context
			ctx := context.WithValue(r.Context(), UserKey, user)
			ctx = context.WithValue(ctx, UserIDKey, user.ID)
			ctx = context.WithValue(ctx, UserEmailKey, user.Email)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
