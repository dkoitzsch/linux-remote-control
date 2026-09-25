// Package model holds data types shared across the agent's HTTP API,
// process monitor, network monitor and the React frontend's JSON contract.
package model

import "time"

// Role is an account's permission level.
type Role string

const (
	// RoleFull grants view AND control (kill processes, send notifications, shutdown).
	RoleFull Role = "full"
	// RoleViewer grants read-only access (screen + process view only).
	RoleViewer Role = "viewer"
)

func (r Role) Valid() bool {
	return r == RoleFull || r == RoleViewer
}

type User struct {
	ID           int64     `json:"id"`
	Username     string    `json:"username"`
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
	PasswordHash string    `json:"-"`
}

// Process describes one running process as reported to the frontend.
type Process struct {
	PID           int32      `json:"pid"`
	PPID          int32      `json:"ppid"`
	Name          string     `json:"name"`
	Exe           string     `json:"exe,omitempty"`
	Cmdline       string     `json:"cmdline,omitempty"`
	Username      string     `json:"username,omitempty"`
	StartTime     time.Time  `json:"startTime"`
	UptimeSec     int64      `json:"uptimeSeconds"`
	RSSBytes      uint64     `json:"rssBytes"`
	CPUPercent    float64    `json:"cpuPercent"`
	IsService     bool       `json:"isService"` // background/system service (systemd system.slice)
	HasWindow     bool       `json:"hasWindow"` // has a mapped X11 top-level window (GUI app)
	NetBytesSent  uint64     `json:"netBytesSent"`
	NetBytesRecv  uint64     `json:"netBytesRecv"`
	NetLastActive *time.Time `json:"netLastActive,omitempty"`
}

// ShutdownState reflects an in-progress scheduled shutdown, exposed via the API
// so a freshly loaded frontend can show the correct countdown after a page reload.
type ShutdownState struct {
	Active      bool      `json:"active"`
	At          time.Time `json:"at,omitempty"`
	Message     string    `json:"message,omitempty"`
	RequestedBy string    `json:"requestedBy,omitempty"`
}
