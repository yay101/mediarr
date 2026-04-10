package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/yay101/mediarr/internal/db"
	lib "github.com/yay101/oidc"
)

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
	// 1. Try API Key header
	apiKey := r.Header.Get("X-API-Key")
	if apiKey == "" {
		apiKey = r.URL.Query().Get("api_key")
	}

	if apiKey != "" {
		database := s.app.DB()
		if database != nil {
			table, _ := database.Users()
			users, _ := table.Filter(func(u db.User) bool {
				return u.APIKey == apiKey
			})
			if len(users) > 0 {
				return &users[0], nil
			}
		}
	}

	// 2. Try session cookie
	cookie, err := r.Cookie("mediarr_session")
	if err == nil {
		database := s.app.DB()
		if database != nil {
			table, _ := database.Users()
			// The cookie value now contains the API Key for persistent sessions
			users, _ := table.Filter(func(u db.User) bool {
				return u.APIKey == cookie.Value
			})
			if len(users) > 0 {
				return &users[0], nil
			}
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
	database := s.app.DB()
	if database == nil {
		return false, nil
	}

	table, err := database.Users()
	if err != nil {
		return false, nil
	}

	// Find or create user
	users, _ := table.Filter(func(u db.User) bool {
		return u.OIDCSubject == idtoken.Subject
	})

	var user *db.User
	if len(users) > 0 {
		user = &users[0]
	} else {
		// Create new user
		user = &db.User{
			Username:    idtoken.Email,
			OIDCSubject: idtoken.Subject,
			APIKey:      generateAPIKey(),
			Role:        db.RoleUser,
		}
		// If it's the first user, make them admin? Or use config.
		id, _ := table.Insert(user)
		user.ID = id
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
