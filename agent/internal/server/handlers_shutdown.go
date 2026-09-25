package server

import (
	"encoding/json"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/dkoitzsch/linux-remote-control/agent/internal/auth"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/model"
)

const (
	minShutdownDelay = 5 * time.Second
	maxShutdownDelay = 24 * time.Hour
)

func (s *Server) handleShutdownState(w http.ResponseWriter, r *http.Request) {
	s.schedMu.Lock()
	st := s.shutdown
	s.schedMu.Unlock()
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleShutdownSchedule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DelaySeconds int    `json:"delaySeconds"`
		Message      string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	delay := time.Duration(req.DelaySeconds) * time.Second
	if delay < minShutdownDelay || delay > maxShutdownDelay {
		http.Error(w, "delaySeconds must be between 5 and 86400", http.StatusBadRequest)
		return
	}
	message := strings.TrimSpace(req.Message)
	if message == "" {
		message = "Dieser Computer wird in Kürze heruntergefahren."
	}
	if len(message) > 2000 {
		http.Error(w, "message too long", http.StatusBadRequest)
		return
	}

	u, _ := auth.UserFromContext(r.Context())
	requestedBy := ""
	if u != nil {
		requestedBy = u.Username
	}

	at := time.Now().Add(delay)

	s.schedMu.Lock()
	if s.shutdownTimer != nil {
		s.shutdownTimer.Stop()
	}
	s.shutdown = model.ShutdownState{Active: true, At: at, Message: message, RequestedBy: requestedBy}
	s.shutdownTimer = time.AfterFunc(delay, s.executeShutdown)
	s.schedMu.Unlock()

	if err := s.Hub.ShutdownOverlay(req.DelaySeconds, message); err != nil {
		log.Printf("shutdown: failed to notify helper: %v", err)
	}

	writeJSON(w, http.StatusOK, s.shutdown)
}

func (s *Server) handleShutdownCancel(w http.ResponseWriter, r *http.Request) {
	s.schedMu.Lock()
	if s.shutdownTimer != nil {
		s.shutdownTimer.Stop()
		s.shutdownTimer = nil
	}
	s.shutdown = model.ShutdownState{}
	s.schedMu.Unlock()

	if err := s.Hub.ShutdownCancel(); err != nil {
		log.Printf("shutdown: failed to notify helper of cancel: %v", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) executeShutdown() {
	s.schedMu.Lock()
	active := s.shutdown.Active
	s.schedMu.Unlock()
	if !active {
		return // cancelled after the timer already fired but before this ran
	}
	log.Printf("shutdown: executing scheduled shutdown now")
	if err := exec.Command("systemctl", "poweroff").Run(); err != nil {
		log.Printf("shutdown: systemctl poweroff failed: %v", err)
	}
}
