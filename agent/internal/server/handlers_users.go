package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/dkoitzsch/linux-remote-control/agent/internal/auth"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/model"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/store"
)

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.Store.ListUsers(r.Context())
	if err != nil {
		http.Error(w, "failed to list users", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string     `json:"username"`
		Password string     `json:"password"`
		Role     model.Role `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || len(req.Password) < 8 {
		http.Error(w, "username required, password must be at least 8 characters", http.StatusBadRequest)
		return
	}
	if !req.Role.Valid() {
		http.Error(w, `role must be "full" or "viewer"`, http.StatusBadRequest)
		return
	}

	u, err := s.Store.CreateUser(r.Context(), req.Username, req.Password, req.Role)
	if err != nil {
		if errors.Is(err, store.ErrUsernameTaken) {
			http.Error(w, "username already exists", http.StatusConflict)
			return
		}
		http.Error(w, "failed to create user: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, publicUser(u))
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	self, _ := auth.UserFromContext(r.Context())
	if self != nil && self.ID == id {
		http.Error(w, "cannot delete your own account while logged in as it", http.StatusBadRequest)
		return
	}

	target, err := s.Store.GetUser(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to look up user", http.StatusInternalServerError)
		return
	}
	if target.Role == model.RoleFull {
		if err := s.guardLastFullAccessAccount(r); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	if err := s.Store.DeleteUser(r.Context(), id); err != nil {
		http.Error(w, "failed to delete user", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// guardLastFullAccessAccount prevents deleting the only remaining
// full-access account, which would permanently lock everyone out of control
// (and most admin) functionality.
func (s *Server) guardLastFullAccessAccount(r *http.Request) error {
	users, err := s.Store.ListUsers(r.Context())
	if err != nil {
		return err
	}
	fullCount := 0
	for _, u := range users {
		if u.Role == model.RoleFull {
			fullCount++
		}
	}
	if fullCount <= 1 {
		return errors.New("cannot delete the last remaining full-access account")
	}
	return nil
}
