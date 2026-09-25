// Package x11win enumerates top-level X11 windows and the PIDs that own
// them, using the EWMH _NET_CLIENT_LIST / _NET_WM_PID properties that KWin
// (and every other modern Linux window manager) maintains on the root
// window. This is how rc-helper tells rc-agentd which processes are
// "programs with a UI" versus everything else.
//
// Pure Go (jezek/xgb speaks the X11 wire protocol directly), no Cgo/Xlib
// needed for this part.
package x11win

import (
	"fmt"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

type Watcher struct {
	conn *xgb.Conn
	root xproto.Window

	atomClientList xproto.Atom
	atomWMPid      xproto.Atom
}

func New() (*Watcher, error) {
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("x11win: connect: %w", err)
	}
	setup := xproto.Setup(conn)
	if setup == nil || len(setup.Roots) == 0 {
		conn.Close()
		return nil, fmt.Errorf("x11win: no X11 screens")
	}
	root := setup.Roots[0].Root

	clientList, err := internAtom(conn, "_NET_CLIENT_LIST")
	if err != nil {
		conn.Close()
		return nil, err
	}
	wmPid, err := internAtom(conn, "_NET_WM_PID")
	if err != nil {
		conn.Close()
		return nil, err
	}

	return &Watcher{conn: conn, root: root, atomClientList: clientList, atomWMPid: wmPid}, nil
}

func (w *Watcher) Close() {
	w.conn.Close()
}

func internAtom(conn *xgb.Conn, name string) (xproto.Atom, error) {
	reply, err := xproto.InternAtom(conn, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, fmt.Errorf("x11win: intern atom %s: %w", name, err)
	}
	if reply == nil {
		return 0, fmt.Errorf("x11win: intern atom %s: no reply", name)
	}
	return reply.Atom, nil
}

// WindowPIDs returns the deduplicated set of PIDs owning a top-level window
// according to the window manager's _NET_CLIENT_LIST.
func (w *Watcher) WindowPIDs() ([]int32, error) {
	windows, err := w.clientList()
	if err != nil {
		return nil, err
	}

	seen := make(map[int32]bool, len(windows))
	out := make([]int32, 0, len(windows))
	for _, win := range windows {
		pid, ok := w.windowPID(win)
		if !ok || pid <= 0 {
			continue
		}
		if !seen[pid] {
			seen[pid] = true
			out = append(out, pid)
		}
	}
	return out, nil
}

func (w *Watcher) clientList() ([]xproto.Window, error) {
	reply, err := xproto.GetProperty(w.conn, false, w.root, w.atomClientList, xproto.AtomWindow, 0, ^uint32(0)).Reply()
	if err != nil {
		return nil, fmt.Errorf("x11win: get _NET_CLIENT_LIST: %w", err)
	}
	if reply == nil || reply.Format != 32 {
		return nil, nil
	}
	n := len(reply.Value) / 4
	out := make([]xproto.Window, 0, n)
	for i := 0; i < n; i++ {
		v := uint32(reply.Value[i*4]) | uint32(reply.Value[i*4+1])<<8 | uint32(reply.Value[i*4+2])<<16 | uint32(reply.Value[i*4+3])<<24
		out = append(out, xproto.Window(v))
	}
	return out, nil
}

func (w *Watcher) windowPID(win xproto.Window) (int32, bool) {
	reply, err := xproto.GetProperty(w.conn, false, win, w.atomWMPid, xproto.AtomCardinal, 0, 1).Reply()
	if err != nil || reply == nil || reply.Format != 32 || len(reply.Value) < 4 {
		return 0, false
	}
	v := uint32(reply.Value[0]) | uint32(reply.Value[1])<<8 | uint32(reply.Value[2])<<16 | uint32(reply.Value[3])<<24
	return int32(v), true
}
