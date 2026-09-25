// Package auth provides session-cookie based authentication middleware on
// top of internal/store, plus role-gating helpers used by the HTTP API.
package auth

import (
	"context"
	"net/http"
	"time"

	"github.com/dkoitzsch/linux-remote-control/agent/internal/model"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/store"
)

const CookieName = "rc_session"

type ctxKey int

const userCtxKey ctxKey = 0

// UserFromContext returns the authenticated user for the current request, if any.
func UserFromContext(ctx context.Context) (*model.User, bool) {
	u, ok := ctx.Value(userCtxKey).(*model.User)
	return u, ok
}

type Auth struct {
	store *store.Store
	// Secure controls the Cookie's Secure flag; true whenever served over TLS (always, in this project).
	Secure bool
}

func New(s *store.Store, secure bool) *Auth {
	return &Auth{store: s, Secure: secure}
}

// Login authenticates credentials and, on success, sets the session cookie.
func (a *Auth) Login(w http.ResponseWriter, r *http.Request, username, password string) (*model.User, error) {
	u, err := a.store.Authenticate(r.Context(), username, password)
	if err != nil {
		return nil, err
	}
	token, expiresAt, err := a.store.CreateSession(r.Context(), u.ID)
	if err != nil {
		return nil, err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   a.Secure,
		SameSite: http.SameSiteStrictMode,
	})
	return u, nil
}

func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(CookieName); err == nil {
		_ = a.store.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   a.Secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// currentUser resolves the session cookie on r to a user, or nil.
func (a *Auth) currentUser(r *http.Request) *model.User {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	u, err := a.store.SessionUser(r.Context(), c.Value)
	if err != nil {
		return nil
	}
	return u
}

// RequireRole wraps a handler so it 401s with no valid session, and 403s if
// the session's role isn't in allowed. Pass no roles to just require login.
func (a *Auth) RequireRole(next http.HandlerFunc, allowed ...model.Role) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := a.currentUser(r)
		if u == nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if len(allowed) > 0 {
			ok := false
			for _, role := range allowed {
				if u.Role == role {
					ok = true
					break
				}
			}
			if !ok {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		ctx := context.WithValue(r.Context(), userCtxKey, u)
		next(w, r.WithContext(ctx))
	}
}
