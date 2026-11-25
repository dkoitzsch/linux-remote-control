# linux-remote-control

A self-hosted parental control portal for a Fedora/KDE machine on your own
home network: screen view, filterable process list, process termination, a
"stays on top until acknowledged" message, and a scheduled shutdown with a
countdown window — all from a browser, with role-based login (Full
Access / Viewer).

## Architecture

```
┌──────────────────────┐        HTTPS (LAN, port 8443)        ┌───────────────────────────────────────┐
│  Browser (parent PC)  │ ───────────────────────────────────▶ │  rc-agentd (root, systemd system svc)   │
└──────────────────────┘                                       │  - Auth/sessions (SQLite)               │
                                                                 │  - Process list (gopsutil) + per-       │
                                                                 │    process network monitor (libpcap)    │
                                                                 │  - HTTP API + embedded web portal       │
                                                                 └───────────────┬─────────────────────────┘
                                                                                 │ Unix socket (IPC, token-
                                                                                 │ authenticated)
                                                                                 ▼
                                                                 ┌───────────────────────────────────────┐
                                                                 │  rc-helper (child's user, KDE autostart)│
                                                                 │  - Screenshot/stream via X11             │
                                                                 │  - Window→PID mapping (EWMH)             │
                                                                 │  - launches rc-overlay for popups        │
                                                                 └───────────────┬─────────────────────────┘
                                                                                 │ subprocess
                                                                                 ▼
                                                                 ┌───────────────────────────────────────┐
                                                                 │  rc-overlay (Fyne, fullscreen, non-     │
                                                                 │  dismissable): "Acknowledged" message   │
                                                                 │  or shutdown countdown                   │
                                                                 └───────────────────────────────────────┘
```

**Why two processes on the child's PC?** `rc-agentd` runs as a root system
service and therefore has *no* access to the child's graphical session (no
`DISPLAY`, no X11 permissions). Anything that needs to see the screen or
show a window therefore runs as `rc-helper` *inside* the child's KDE session
and talks to `rc-agentd` over a local, token-secured Unix socket.
`rc-agentd` stays responsible for everything that needs root privileges
(killing processes, network capture, shutting down).

**LAN only:** the browser connects directly and exclusively over the LAN to
`rc-agentd` (self-signed TLS certificate, one-time warning in the browser).
No relay server is included for access from outside the home network — a
VPN (e.g. WireGuard) into the home network is the recommended way to do
that.

### Why an `<img>` tag as "video"?

Screen transmission runs as an MJPEG stream (`multipart/x-mixed-replace`)
over a perfectly normal HTTP connection, which an `<img src="/api/stream">`
plays natively in the browser. That covers both "single screenshots" (`GET
/api/screenshot`) and "video stream" with one single, very simple
mechanism — no WebRTC/WebSocket video stack needed.

### How is "system service" vs. "program with UI" detected?

- **Program with UI**: `rc-helper` asks the window manager (KWin) via the
  EWMH property `_NET_CLIENT_LIST` for all top-level windows and their PID
  (`_NET_WM_PID`). Any process with a mapped window counts as a GUI
  program.
- **System service**: `rc-agentd` reads `/proc/<pid>/cgroup` and detects
  processes systemd started as a `.service` unit under `system.slice`.

A process can be neither (e.g. a background process of an application with
no window of its own) — filterable in the portal as "Other".

### How is network traffic measured per process?

Like `nethogs`: `rc-agentd` periodically reads `/proc/net/{tcp,udp}[6]` and
`/proc/<pid>/fd/*` to map local ports to PIDs, and counts the byte length of
every packet on the matching local port via a `libpcap` capture (`tcp or
udp` on all interfaces). This is an approximation (no kernel eBPF
accounting), but accurate enough for a parental dashboard. If `libpcap`
isn't available, the rest of the system still works — just without the
network columns.

## Prerequisites on the Fedora/KDE machine (child's PC)

```sh
sudo dnf install -y golang gcc pkgconf-pkg-config \
  libpcap-devel \
  mesa-libGL-devel libX11-devel libXcursor-devel libXrandr-devel \
  libXinerama-devel libXi-devel libXxf86vm-devel \
  wayland-devel libxkbcommon-devel
sudo dnf install -y nodejs npm   # only needed to build the frontend
```

(`rc-overlay` uses Fyne/OpenGL for the fullscreen overlays, `rc-agentd`'s
network monitor uses libpcap — both need Cgo and the dev packages listed
above. Easiest to just build directly on the target machine, see below.)

## Installation

Everything is built and installed **on the child's Fedora machine**:

```sh
git clone <this-repo> linux-remote-control
cd linux-remote-control
sudo ./deploy/install.sh --child-user <child's-linux-username>
```

The script builds the frontend + agent (`make all`), installs the binaries
to `/usr/local/bin`, sets up the `rc-agent` group (so `rc-helper`, running
as the child's user, can reach the privileged socket), installs and starts
the `rc-agentd` systemd service, and creates a KDE autostart entry for
`rc-helper`.

Afterwards:

```sh
# Create the first full-access account (interactive, prompts for a password):
sudo rc-agentctl create-user -username parent -role full

# Additional accounts:
sudo rc-agentctl create-user -username child-viewer -role viewer
sudo rc-agentctl list-users
```

The child needs to log out and back in once (for the new group membership
and the KDE autostart entry to take effect). Then, in a browser on the
parent's PC (same home network):

```
https://<hostname-or-ip-of-the-childs-pc>:8443
```

The browser will show a certificate warning on first use (self-signed
certificate, no external CA needed) — confirm it once.

## Roles

- **Full access**: view the screen, view/filter/terminate processes, send
  messages, schedule/cancel shutdown, manage accounts.
- **Viewer**: view the screen + view/filter processes only.

## Development

```sh
# Build and test the backend (Go):
cd agent && go build ./... && go vet ./...

# Frontend with live reload against a locally running rc-agentd:
cd web && npm install && npm run dev
# (vite.config.ts proxies /api to https://localhost:8443)

# Build everything (frontend + Go binaries into ./bin):
make all
```

`rc-agentd` automatically generates a self-signed TLS certificate under
`<data-dir>/certs/` on first start. To run it locally without root (e.g. for
testing), just pass a different `-data-dir`/`-socket`/`-token-file` and a
port > 1024 via `-addr`.

## Security notes

- The Unix socket between `rc-agentd` and `rc-helper` is secured not only by
  filesystem permissions (the `rc-agent` group) but also by a token
  generated on first start (`/etc/rc-agent/helper.token`, readable only by
  root and the `rc-agent` group).
- Logins are coarsely rate-limited per IP (brute-force throttling).
- There is deliberately **no self-registration** in the portal — accounts
  are created exclusively locally via `rc-agentctl`.
- The last remaining full-access account cannot be deleted, to prevent
  locking everyone out.

---

🇩🇪 A German version of this document is available in
[README_DE.md](README_DE.md).
