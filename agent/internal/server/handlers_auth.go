package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/dkoitzsch/linux-remote-control/agent/internal/auth"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/store"
	"golang.org/x/time/rate"
)

// perIPLoginLimiter throttles login attempts to slow down brute forcing of
// account passwords from within the LAN.
var (
	loginLimiters   = map[string]*rate.Limiter{}
	loginLimitersMu sync.Mutex
)

func loginLimiterFor(ip string) *rate.Limiter {
	loginLimitersMu.Lock()
	defer loginLimitersMu.Unlock()
	l, ok := loginLimiters[ip]
	if !ok {
		l = rate.NewLimiter(rate.Every(6*time.Second), 5) // ~5 burst, then 1 per 6s
		loginLimiters[ip] = l
	}
	return l
}

func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if h, _, err := splitHostPort(host); err == nil {
		return h
	}
	return host
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !loginLimiterFor(clientIP(r)).Allow() {
		http.Error(w, "too many login attempts, slow down", http.StatusTooManyRequests)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	u, err := s.Auth.Login(w, r, req.Username, req.Password)
	if err != nil {
		if errors.Is(err, store.ErrInvalidCredentials) {
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		http.Error(w, "login failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, publicUser(u))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.Auth.Logout(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFromContext(r.Context())
	writeJSON(w, http.StatusOK, publicUser(u))
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"helperConnected":       s.Hub.Connected(),
		"networkStatsAvailable": s.Net != nil,
	})
}
