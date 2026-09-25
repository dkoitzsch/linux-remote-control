package server

import (
	"encoding/json"
	"net"
	"net/http"

	"github.com/dkoitzsch/linux-remote-control/agent/internal/model"
)

func splitHostPort(hostport string) (string, string, error) {
	return net.SplitHostPort(hostport)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// publicUser strips the password hash before sending a user to the client.
// Safe to call with nil (renders as null).
func publicUser(u *model.User) *model.User {
	if u == nil {
		return nil
	}
	cp := *u
	cp.PasswordHash = ""
	return &cp
}
