package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/dkoitzsch/linux-remote-control/agent/internal/ipc"
)

func (s *Server) handleNotify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	req.Message = strings.TrimSpace(req.Message)
	if req.Message == "" {
		http.Error(w, "message must not be empty", http.StatusBadRequest)
		return
	}
	if len(req.Message) > 2000 {
		http.Error(w, "message too long", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := s.Hub.Notify(ctx, req.Message); err != nil {
		if errors.Is(err, ipc.ErrHelperNotConnected) {
			http.Error(w, "rc-helper is not connected (is the child logged in?)", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "notify failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
