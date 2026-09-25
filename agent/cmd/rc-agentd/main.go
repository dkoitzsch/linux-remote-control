// Command rc-agentd is the privileged system daemon of linux-remote-control.
// It runs as root via systemd, serves the HTTPS API and the web portal, and
// talks to rc-helper (running inside the child's graphical session) over a
// Unix domain socket for anything that needs display access: screenshots
// and the on-top notification/shutdown overlays.
package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/dkoitzsch/linux-remote-control/agent/internal/auth"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/ipc"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/netmon"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/procmon"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/server"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/store"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/tlsutil"
	"github.com/dkoitzsch/linux-remote-control/agent/internal/webui"
)

func main() {
	addr := flag.String("addr", ":8443", "HTTPS listen address")
	dataDir := flag.String("data-dir", "/var/lib/rc-agent", "directory for the SQLite DB and generated TLS cert")
	socketPath := flag.String("socket", "/run/rc-agent/helper.sock", "Unix socket rc-helper connects to")
	tokenPath := flag.String("token-file", "/etc/rc-agent/helper.token", "shared secret rc-helper authenticates with")
	socketGroup := flag.String("socket-group", "rc-agent", "group allowed to connect to the helper socket (must match rc-helper's user's group)")
	flag.Parse()

	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		log.Fatalf("data dir: %v", err)
	}

	st, err := store.Open(filepath.Join(*dataDir, "agent.db"))
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if n, err := st.CountUsers(ctx); err == nil && n == 0 {
		log.Printf("=========================================================================")
		log.Printf("Noch kein Account vorhanden. Lege den ersten Vollzugriff-Account an mit:")
		log.Printf("  rc-agentctl -data-dir %s create-user -username <name> -role full", *dataDir)
		log.Printf("=========================================================================")
	}

	token, err := loadOrCreateToken(*tokenPath, *socketGroup)
	if err != nil {
		log.Fatalf("helper token: %v", err)
	}

	hub := ipc.NewHub(token)
	listener, err := listenHelperSocket(*socketPath, *socketGroup)
	if err != nil {
		log.Fatalf("helper socket: %v", err)
	}
	defer os.Remove(*socketPath)
	go func() {
		if err := hub.Serve(ctx, listener); err != nil {
			log.Printf("helper socket serve: %v", err)
		}
	}()

	proc := procmon.New()

	var netMon *netmon.Monitor
	var netForServer server.NetSnapshotter
	if nm, err := netmon.New(); err != nil {
		log.Printf("network monitor unavailable (per-process network stats disabled): %v", err)
	} else {
		netMon = nm
		netForServer = nm
		go netMon.Run(ctx)
		defer netMon.Close()
	}

	certPath := filepath.Join(*dataDir, "certs", "cert.pem")
	keyPath := filepath.Join(*dataDir, "certs", "key.pem")
	cert, err := tlsutil.EnsureCert(certPath, keyPath)
	if err != nil {
		log.Fatalf("tls cert: %v", err)
	}

	static, err := webui.Handler()
	if err != nil {
		log.Printf("embedded web UI unavailable: %v", err)
		static = nil
	}

	a := auth.New(st, true)
	srv := server.New(st, a, hub, proc, netForServer, static)

	httpServer := &http.Server{
		Addr:         *addr,
		Handler:      srv.Routes(),
		TLSConfig:    &tls.Config{Certificates: []tls.Certificate{cert}},
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // MJPEG streams are long-lived
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("rc-agentd listening on %s", *addr)
	if err := httpServer.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
		log.Fatalf("https server: %v", err)
	}
}

func loadOrCreateToken(path, group string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		s := string(b)
		if len(s) > 0 {
			return trimNL(s), nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(token+"\n"), 0640); err != nil {
		return "", err
	}
	chownToGroup(path, group)
	return token, nil
}

func trimNL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func listenHelperSocket(path, group string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	_ = os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0660); err != nil {
		return nil, err
	}
	chownToGroup(path, group)
	return l, nil
}

// chownToGroup best-effort chowns path to root:group so a non-root process
// in that group (rc-helper, running as the child user) can read/connect to
// it. Failures are logged, not fatal: on a fresh install the group may not
// exist yet until install.sh creates it, and misconfiguration here should
// not stop the daemon from serving the rest of the API.
func chownToGroup(path, group string) {
	g, err := user.LookupGroup(group)
	if err != nil {
		log.Printf("chown %s: group %q not found (create it and re-run, or pass -socket-group): %v", path, group, err)
		return
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		log.Printf("chown %s: bad gid for group %q: %v", path, group, err)
		return
	}
	if err := os.Chown(path, 0, gid); err != nil {
		log.Printf("chown %s to group %q: %v", path, group, err)
	}
}
