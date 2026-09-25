package server

import (
	"net/http"
	"strconv"
	"syscall"

	"github.com/dkoitzsch/linux-remote-control/agent/internal/model"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/netmon"
)

func (s *Server) handleProcessList(w http.ResponseWriter, r *http.Request) {
	procs, err := s.Proc.List(r.Context())
	if err != nil {
		http.Error(w, "failed to list processes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	windowPIDs := s.Hub.WindowPIDs()
	windowSet := make(map[int32]bool, len(windowPIDs))
	for _, pid := range windowPIDs {
		windowSet[pid] = true
	}

	var snap map[int32]netmon.Stats
	if s.Net != nil {
		snap = s.Net.Snapshot()
	}

	out := make([]model.Process, len(procs))
	for i, p := range procs {
		p.HasWindow = windowSet[p.PID]
		if ns, ok := snap[p.PID]; ok {
			p.NetBytesSent = ns.BytesSent
			p.NetBytesRecv = ns.BytesRecv
			if !ns.LastActive.IsZero() {
				t := ns.LastActive
				p.NetLastActive = &t
			}
		}
		out[i] = p
	}

	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleKillProcess(w http.ResponseWriter, r *http.Request) {
	pidStr := r.PathValue("pid")
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 1 {
		http.Error(w, "invalid pid", http.StatusBadRequest)
		return
	}

	sig := syscall.SIGTERM
	if r.URL.Query().Get("force") == "1" {
		sig = syscall.SIGKILL
	}
	if err := syscall.Kill(pid, sig); err != nil {
		http.Error(w, "kill failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
