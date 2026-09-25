// Command rc-helper runs inside the child's KDE/X11 user session (started
// via a systemd --user unit / autostart) and does everything that needs
// display access, which the privileged rc-agentd system service does not
// have: screen capture, enumerating GUI windows, and showing the
// notification/shutdown overlays (delegated to the rc-overlay subprocess).
// It connects out to rc-agentd's Unix socket and reconnects automatically.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dkoitzsch/linux-remote-control/agent/internal/ipc"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/screencap"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/x11win"
)

func main() {
	socketPath := flag.String("socket", "/run/rc-agent/helper.sock", "Unix socket rc-agentd listens on")
	tokenPath := flag.String("token-file", "/etc/rc-agent/helper.token", "shared secret to authenticate with rc-agentd")
	overlayBin := flag.String("overlay-bin", "", "path to the rc-overlay binary (default: alongside this binary, then $PATH)")
	flag.Parse()

	h := &helper{
		socketPath: *socketPath,
		tokenPath:  *tokenPath,
		overlayBin: resolveOverlayBin(*overlayBin),
	}
	h.run()
}

func resolveOverlayBin(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "rc-overlay")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "rc-overlay" // resolved via $PATH
}

type helper struct {
	socketPath string
	tokenPath  string
	overlayBin string
}

func (h *helper) run() {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		token, err := os.ReadFile(h.tokenPath)
		if err != nil {
			log.Printf("rc-helper: read token file %s: %v (retrying)", h.tokenPath, err)
			time.Sleep(backoff)
			continue
		}

		conn, err := net.Dial("unix", h.socketPath)
		if err != nil {
			log.Printf("rc-helper: connect %s: %v (retrying in %s)", h.socketPath, err, backoff)
			time.Sleep(backoff)
			backoff = minDuration(backoff*2, maxBackoff)
			continue
		}

		c := ipc.NewConn(conn)
		if err := c.Send(ipc.Frame{Type: ipc.MsgHello, Payload: ipc.JSON(ipc.HelloPayload{Token: strings.TrimSpace(string(token))})}); err != nil {
			log.Printf("rc-helper: hello: %v", err)
			conn.Close()
			time.Sleep(backoff)
			continue
		}
		f, err := c.Recv()
		if err != nil || f.Type != ipc.MsgHelloAck {
			log.Printf("rc-helper: hello handshake failed: %v", err)
			conn.Close()
			time.Sleep(backoff)
			continue
		}
		var ack ipc.HelloAckPayload
		_ = json.Unmarshal(f.Payload, &ack)
		if !ack.OK {
			log.Printf("rc-helper: rejected by rc-agentd: %s (retrying in %s)", ack.Error, backoff)
			conn.Close()
			time.Sleep(backoff)
			backoff = minDuration(backoff*2, maxBackoff)
			continue
		}

		log.Printf("rc-helper: connected to rc-agentd")
		backoff = time.Second
		h.serveSession(c)
		conn.Close()
		log.Printf("rc-helper: disconnected, will reconnect")
		time.Sleep(time.Second)
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

type session struct {
	h *helper
	c *ipc.Conn

	mu           sync.Mutex
	streamCancel context.CancelFunc
	shutdownCmd  *exec.Cmd
}

// serveSession handles one connected socket session until it errors out
// (rc-agentd restarted, network hiccup, etc), at which point run() reconnects.
func (h *helper) serveSession(c *ipc.Conn) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := &session{h: h, c: c}

	if w, err := x11win.New(); err != nil {
		log.Printf("rc-helper: X11 window watcher unavailable (GUI/service classification will be incomplete): %v", err)
	} else {
		go func() {
			defer w.Close()
			s.windowPIDLoop(ctx, w)
		}()
	}

	for {
		f, err := c.Recv()
		if err != nil {
			return
		}
		switch f.Type {
		case ipc.MsgScreenshotRequest:
			go s.handleScreenshotRequest(f.ID)
		case ipc.MsgStreamStart:
			var p ipc.StreamStartPayload
			_ = json.Unmarshal(f.Payload, &p)
			s.startStream(ctx, p)
		case ipc.MsgStreamStop:
			s.stopStream()
		case ipc.MsgNotify:
			go s.handleNotify(f.ID, f.Payload)
		case ipc.MsgShutdownOverlay:
			s.handleShutdownOverlay(f.Payload)
		case ipc.MsgShutdownCancel:
			s.cancelShutdownOverlay()
		}
	}
}

func (s *session) windowPIDLoop(ctx context.Context, w *x11win.Watcher) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	send := func() {
		pids, err := w.WindowPIDs()
		if err != nil {
			return
		}
		_ = s.c.Send(ipc.Frame{Type: ipc.MsgWindowPIDs, Payload: ipc.JSON(ipc.WindowPIDsPayload{PIDs: pids})})
	}
	send()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			send()
		}
	}
}

func (s *session) handleScreenshotRequest(id string) {
	data, err := screencap.Capture(85)
	if err != nil {
		_ = s.c.Send(ipc.Frame{Type: ipc.MsgError, ID: id, Payload: ipc.JSON(ipc.ErrorPayload{Error: err.Error()})})
		return
	}
	_ = s.c.Send(ipc.Frame{Type: ipc.MsgScreenshotData, ID: id, Payload: data})
}

func (s *session) startStream(ctx context.Context, p ipc.StreamStartPayload) {
	s.stopStream()

	fps := p.FPS
	if fps <= 0 {
		fps = 2
	}
	quality := p.Quality
	if quality <= 0 {
		quality = 60
	}

	streamCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.streamCancel = cancel
	s.mu.Unlock()

	go func() {
		interval := time.Second / time.Duration(fps)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-streamCtx.Done():
				return
			case <-ticker.C:
				data, err := screencap.Capture(quality)
				if err != nil {
					continue
				}
				if err := s.c.Send(ipc.Frame{Type: ipc.MsgStreamFrame, ID: "stream", Payload: data}); err != nil {
					return
				}
			}
		}
	}()
}

func (s *session) stopStream() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.streamCancel != nil {
		s.streamCancel()
		s.streamCancel = nil
	}
}

func (s *session) handleNotify(id string, payload []byte) {
	var p ipc.NotifyPayload
	_ = json.Unmarshal(payload, &p)

	cmd := exec.Command(s.h.overlayBin, "notify", "--message", p.Message)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		_ = s.c.Send(ipc.Frame{Type: ipc.MsgError, ID: id, Payload: ipc.JSON(ipc.ErrorPayload{Error: err.Error()})})
		return
	}
	_ = s.c.Send(ipc.Frame{Type: ipc.MsgNotifyAck, ID: id})
	go func() { _ = cmd.Wait() }()
}

func (s *session) handleShutdownOverlay(payload []byte) {
	var p ipc.ShutdownOverlayPayload
	_ = json.Unmarshal(payload, &p)

	s.cancelShutdownOverlay()

	cmd := exec.Command(s.h.overlayBin, "shutdown",
		"--seconds", strconv.Itoa(p.Seconds),
		"--message", p.Message)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		log.Printf("rc-helper: start shutdown overlay: %v", err)
		return
	}
	s.mu.Lock()
	s.shutdownCmd = cmd
	s.mu.Unlock()
	go func() {
		_ = cmd.Wait()
		s.mu.Lock()
		if s.shutdownCmd == cmd {
			s.shutdownCmd = nil
		}
		s.mu.Unlock()
	}()
}

func (s *session) cancelShutdownOverlay() {
	s.mu.Lock()
	cmd := s.shutdownCmd
	s.shutdownCmd = nil
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
