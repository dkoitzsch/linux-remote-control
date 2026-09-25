// Package server implements rc-agentd's HTTP(S) API: authentication,
// screen capture, process listing/kill, notifications and shutdown control.
package server

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/dkoitzsch/linux-remote-control/agent/internal/auth"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/ipc"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/model"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/netmon"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/procmon"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/store"
)

// NetSnapshotter is satisfied by *netmon.Monitor; kept as an interface so
// the server still runs (without network stats) when pcap isn't available.
type NetSnapshotter interface {
	Snapshot() map[int32]netmon.Stats
}

type Server struct {
	Store  *store.Store
	Auth   *auth.Auth
	Hub    *ipc.Hub
	Proc   *procmon.Monitor
	Net    NetSnapshotter // may be nil
	Static http.Handler   // embedded frontend, may be nil in API-only/dev mode

	schedMu       sync.Mutex
	shutdown      model.ShutdownState
	shutdownTimer *time.Timer
}

func New(st *store.Store, a *auth.Auth, hub *ipc.Hub, proc *procmon.Monitor, net NetSnapshotter, static http.Handler) *Server {
	s := &Server{Store: st, Auth: a, Hub: hub, Proc: proc, Net: net, Static: static}
	hub.OnHelperConnect = s.onHelperReconnect
	return s
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.Auth.RequireRole(s.handleLogout))
	mux.HandleFunc("GET /api/me", s.Auth.RequireRole(s.handleMe))

	mux.HandleFunc("GET /api/screenshot", s.Auth.RequireRole(s.handleScreenshot))
	mux.HandleFunc("GET /api/stream", s.Auth.RequireRole(s.handleStream))

	mux.HandleFunc("GET /api/processes", s.Auth.RequireRole(s.handleProcessList))
	mux.HandleFunc("POST /api/processes/{pid}/kill", s.Auth.RequireRole(s.handleKillProcess, model.RoleFull))

	mux.HandleFunc("POST /api/notify", s.Auth.RequireRole(s.handleNotify, model.RoleFull))

	mux.HandleFunc("GET /api/shutdown", s.Auth.RequireRole(s.handleShutdownState))
	mux.HandleFunc("POST /api/shutdown", s.Auth.RequireRole(s.handleShutdownSchedule, model.RoleFull))
	mux.HandleFunc("DELETE /api/shutdown", s.Auth.RequireRole(s.handleShutdownCancel, model.RoleFull))

	mux.HandleFunc("GET /api/users", s.Auth.RequireRole(s.handleListUsers, model.RoleFull))
	mux.HandleFunc("POST /api/users", s.Auth.RequireRole(s.handleCreateUser, model.RoleFull))
	mux.HandleFunc("DELETE /api/users/{id}", s.Auth.RequireRole(s.handleDeleteUser, model.RoleFull))

	mux.HandleFunc("GET /api/status", s.Auth.RequireRole(s.handleStatus))

	if s.Static != nil {
		mux.Handle("/", s.Static)
	}

	return withLogging(mux)
}

func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s %s", r.RemoteAddr, r.Method, r.URL.Path, time.Since(start))
	})
}

func (s *Server) onHelperReconnect() {
	s.schedMu.Lock()
	active := s.shutdown.Active
	remaining := int(time.Until(s.shutdown.At).Seconds())
	message := s.shutdown.Message
	s.schedMu.Unlock()
	if active && remaining > 0 {
		_ = s.Hub.ShutdownOverlay(remaining, message)
	}
}
