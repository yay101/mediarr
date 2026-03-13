package server

import (
	"context"
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
	// 1. Try session cookie
	cookie, err := r.Cookie("mediarr_session")
	if err == nil {
		// Use the value to find user in db
		// For now, I'll use a simplified mapping
		database := s.app.DB()
		if database != nil {
			table, _ := database.Users()
			users, _ := table.Filter(func(u db.User) bool {
				return u.Username == cookie.Value // In reality, use a session token mapping
			})
			if len(users) > 0 {
				return &users[0], nil
			}
		}
	}

	// 2. Fallback to default admin if disabled auth (for dev)
	cfg := s.app.Config()
	if !cfg.Auth.OIDC.Enabled {
		return &db.User{
			Username: cfg.Auth.Defaults.Admin.Username,
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
			Role:        db.RoleUser,
		}
		// If it's the first user, make them admin? Or use config.
		id, _ := table.Insert(user)
		user.ID = id
	}

	// Create session cookie
	cookie := &http.Cookie{
		Name:     "mediarr_session",
		Value:    user.Username, // Simple for now
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}

	return true, cookie
}

func currentUser(r *http.Request) *db.User {
	if user, ok := r.Context().Value(userContextKey).(*db.User); ok {
		return user
	}
	return nil
}
