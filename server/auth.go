package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/yay101/mediarr/db"
	lib "github.com/yay101/oidc"
)

var (
	debugLog *os.File
)

func initDebugLog() {
	var err error
	debugLog, err = os.OpenFile("/tmp/mediarr_debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		slog.Warn("failed to open debug log", "error", err)
	} else {
		slog.Info("debug logging enabled to /tmp/mediarr_debug.log")
	}
}

func debugLogf(format string, args ...interface{}) {
	if debugLog != nil {
		msg := "[DEBUG] " + format + "\n"
		debugLog.WriteString(fmt.Sprintf(msg, args...))
		slog.Debug(fmt.Sprintf(msg, args...))
	}
}

type contextKey string

const (
	userContextKey contextKey = "user"
)

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := s.getCurrentUser(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (s *Server) adminMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := s.getCurrentUser(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if user.Role != db.RoleAdmin {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (s *Server) getCurrentUser(r *http.Request) (*db.User, error) {
	debugLogf("getCurrentUser: path=%s, cookies=%v", r.URL.Path, r.Cookies())

	// 1. Try API Key header
	apiKey := r.Header.Get("X-API-Key")
	if apiKey == "" {
		apiKey = r.URL.Query().Get("api_key")
	}

	if apiKey != "" {
		debugLogf("getCurrentUser: trying API key: %s", apiKey)
		database := s.app.DB()
		if database != nil {
			table, _ := database.Users()
			users, _ := table.Filter(func(u db.User) bool {
				return u.APIKey == apiKey
			})
			if len(users) > 0 {
				debugLogf("getCurrentUser: found user by API key, id=%d, username=%s", users[0].ID, users[0].Username)
				return &users[0], nil
			}
		}
	}

	// 2. Try session cookie
	cookie, err := r.Cookie("mediarr_session")
	if err == nil {
		debugLogf("getCurrentUser: found session cookie, value=%s", cookie.Value)
		database := s.app.DB()
		if database != nil {
			table, _ := database.Users()
			// The cookie value now contains the API Key for persistent sessions
			users, _ := table.Filter(func(u db.User) bool {
				return u.APIKey == cookie.Value
			})
			if len(users) > 0 {
				debugLogf("getCurrentUser: found user by cookie, id=%d, username=%s", users[0].ID, users[0].Username)
				return &users[0], nil
			}
			debugLogf("getCurrentUser: no user found with cookie value")
		}
	}

	// 3. Fallback to default admin if disabled auth (for dev)
	cfg := s.app.Config()
	if !cfg.Auth.OIDC.Enabled {
		return &db.User{
			ID:       1,
			Username: cfg.Auth.Defaults.Admin.Username,
			APIKey:   "dev_admin_key",
			Role:     db.RoleAdmin,
		}, nil
	}

	return nil, http.ErrNoCookie
}

func (s *Server) handleOIDCCallback(accesstoken *string, refreshtoken *string, expiry *int, idtoken lib.IDToken) (bool, *http.Cookie) {
	debugLogf("OIDC Callback: subject=%s, email=%s", idtoken.Subject, idtoken.Email)
	database := s.app.DB()
	if database == nil {
		debugLogf("OIDC Callback: database is nil")
		return false, nil
	}

	table, err := database.Users()
	if err != nil {
		debugLogf("OIDC Callback: failed to get users table: %v", err)
		return false, nil
	}

	// Find or create user
	users, _ := table.Filter(func(u db.User) bool {
		return u.OIDCSubject == idtoken.Subject
	})
	debugLogf("OIDC Callback: found %d existing users with subject", len(users))

	var user *db.User
	if len(users) > 0 {
		user = &users[0]
		debugLogf("OIDC Callback: existing user found, id=%d, username=%s", user.ID, user.Username)
	} else {
		// Create new user
		user = &db.User{
			Username:    idtoken.Email,
			OIDCSubject: idtoken.Subject,
			APIKey:      generateAPIKey(),
			Role:        db.RoleUser,
		}
		debugLogf("OIDC Callback: creating new user, apiKey=%s", user.APIKey)
		// If it's the first user, make them admin? Or use config.
		id, _ := table.Insert(user)
		user.ID = id
		debugLogf("OIDC Callback: new user inserted with id=%d", user.ID)
	}

	// Create session cookie
	cookie := &http.Cookie{
		Name:     "mediarr_session",
		Value:    user.APIKey,
		Path:     "/",
		HttpOnly: true,
		Secure:   false,      // Set to false for local dev compatibility, or use r.TLS != nil
		MaxAge:   86400 * 30, // 30 days
		SameSite: http.SameSiteLaxMode,
	}

	return true, cookie
}

func generateAPIKey() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

func currentUser(r *http.Request) *db.User {
	if user, ok := r.Context().Value(userContextKey).(*db.User); ok {
		return user
	}
	return nil
}
