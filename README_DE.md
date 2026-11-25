# linux-remote-control

Ein selbstgehostetes Eltern-Kontrollportal für einen Fedora/KDE-Rechner im
eigenen Heimnetz: Bildschirmansicht, Prozessliste mit Filtern, Prozesse
beenden, eine "auf oberster Ebene bestehende" Nachricht anzeigen, und einen
geplanten Shutdown mit Countdown-Fenster auslösen – alles über einen Browser
mit rollenbasiertem Login (Vollzugriff / Betrachter).

## Architektur

```
┌──────────────────────┐        HTTPS (LAN, Port 8443)        ┌───────────────────────────────────────┐
│  Browser (Eltern-PC)  │ ───────────────────────────────────▶ │  rc-agentd (root, systemd-Systemdienst) │
└──────────────────────┘                                       │  - Auth/Sessions (SQLite)               │
                                                                 │  - Prozessliste (gopsutil) + Netzwerk-  │
                                                                 │    Monitor pro Prozess (libpcap)        │
                                                                 │  - HTTP-API + eingebettetes Web-Portal  │
                                                                 └───────────────┬─────────────────────────┘
                                                                                 │ Unix-Socket (IPC, token-
                                                                                 │ authentifiziert)
                                                                                 ▼
                                                                 ┌───────────────────────────────────────┐
                                                                 │  rc-helper (Kind-Benutzer, KDE-Autostart)│
                                                                 │  - Screenshot/Stream via X11             │
                                                                 │  - Fenster→PID-Zuordnung (EWMH)          │
                                                                 │  - startet rc-overlay für Popups         │
                                                                 └───────────────┬─────────────────────────┘
                                                                                 │ Subprozess
                                                                                 ▼
                                                                 ┌───────────────────────────────────────┐
                                                                 │  rc-overlay (Fyne, Vollbild, non-       │
                                                                 │  dismissable): Nachricht "Verstanden"   │
                                                                 │  oder Shutdown-Countdown                │
                                                                 └───────────────────────────────────────┘
```

**Warum zwei Prozesse auf dem Kind-PC?** `rc-agentd` läuft als root-Systemdienst
und hat deshalb *keinen* Zugriff auf die grafische Sitzung des Kindes (kein
`DISPLAY`, keine X11-Rechte). Alles, was den Bildschirm sehen oder ein Fenster
zeigen muss, läuft deshalb als `rc-helper` *innerhalb* der KDE-Sitzung des
Kindes und spricht über einen lokalen, mit einem Token abgesicherten
Unix-Socket mit `rc-agentd`. `rc-agentd` bleibt für alles verantwortlich, was
Root-Rechte braucht (Prozesse beenden, Netzwerk-Mitschnitt, Herunterfahren).

**Nur Heimnetz:** Der Browser verbindet sich direkt und ausschließlich per LAN
mit `rc-agentd` (selbstsigniertes TLS-Zertifikat, einmalige Warnung im
Browser). Für Zugriff von unterwegs wird kein zusätzlicher Relay-Server
mitgeliefert – dafür eignet sich ein VPN (z.B. WireGuard) ins Heimnetz.

### Warum ein `<img>`-Tag als "Video"?

Bildschirmübertragung läuft als MJPEG-Stream (`multipart/x-mixed-replace`)
über eine ganz normale HTTP-Verbindung, die ein `<img src="/api/stream">` im
Browser nativ abspielt. Das deckt sowohl "einzelne Screenshots" (`GET
/api/screenshot`) als auch "Videostream" mit einem einzigen, sehr einfachen
Mechanismus ab – kein WebRTC/WebSocket-Video-Stack nötig.

### Wie wird "System-Dienst" vs. "Programm mit UI" erkannt?

- **Programm mit UI**: `rc-helper` fragt den Fenstermanager (KWin) über das
  EWMH-Property `_NET_CLIENT_LIST` nach allen Top-Level-Fenstern und deren
  PID (`_NET_WM_PID`). Jeder Prozess mit einem zugeordneten Fenster gilt als
  GUI-Programm.
- **System-Dienst**: `rc-agentd` liest `/proc/<pid>/cgroup` und erkennt
  Prozesse, die systemd unter `system.slice` als `.service`-Unit gestartet
  hat.

Ein Prozess kann keins von beidem sein (z.B. ein Hintergrundprozess einer
Anwendung ohne eigenes Fenster) – im Portal als "Sonstige" filterbar.

### Wie wird Netzwerkverkehr pro Prozess gemessen?

Wie bei `nethogs`: `rc-agentd` liest periodisch `/proc/net/{tcp,udp}[6]` und
`/proc/<pid>/fd/*`, um lokale Ports auf PIDs abzubilden, und zählt per
`libpcap`-Mitschnitt (`tcp or udp` auf allen Interfaces) die Byte-Länge jedes
Pakets auf den passenden lokalen Port. Das ist eine Näherung (kein
Kernel-eBPF-Accounting), aber ausreichend genau für ein Eltern-Dashboard.
Falls `libpcap` nicht verfügbar ist, läuft der Rest des Systems trotzdem
weiter – nur ohne Netzwerkspalten.

## Voraussetzungen auf dem Fedora/KDE-Rechner (Kind-PC)

```sh
sudo dnf install -y golang gcc pkgconf-pkg-config \
  libpcap-devel \
  mesa-libGL-devel libX11-devel libXcursor-devel libXrandr-devel \
  libXinerama-devel libXi-devel libXxf86vm-devel \
  wayland-devel libxkbcommon-devel
sudo dnf install -y nodejs npm   # nur zum Bauen des Frontends
```

(`rc-overlay` nutzt Fyne/OpenGL für die Vollbild-Overlays, `rc-agentd`s
Netzwerk-Monitor nutzt libpcap – beides braucht Cgo und die genannten
Dev-Pakete. Am einfachsten direkt auf dem Zielrechner bauen, siehe unten.)

## Installation

Alles wird **auf dem Fedora-Rechner des Kindes** gebaut und installiert:

```sh
git clone <dieses-repo> linux-remote-control
cd linux-remote-control
sudo ./deploy/install.sh --child-user <linux-benutzername-des-kindes>
```

Das Skript baut Frontend + Agent (`make all`), installiert die Binaries nach
`/usr/local/bin`, richtet die Gruppe `rc-agent` ein (damit `rc-helper` als
Kind-Benutzer den privilegierten Socket erreichen darf), installiert und
startet den systemd-Dienst `rc-agentd`, und legt einen KDE-Autostart-Eintrag
für `rc-helper` an.

Danach:

```sh
# Ersten Vollzugriff-Account anlegen (interaktiv, fragt nach Passwort):
sudo rc-agentctl create-user -username eltern -role full

# Weitere Accounts:
sudo rc-agentctl create-user -username kind-betrachter -role viewer
sudo rc-agentctl list-users
```

Das Kind muss sich einmal ab- und wieder anmelden (neue Gruppenmitgliedschaft
+ KDE-Autostart). Danach im Browser auf dem Eltern-PC (gleiches Heimnetz):

```
https://<hostname-oder-ip-des-kind-pcs>:8443
```

Der Browser zeigt beim ersten Aufruf eine Zertifikatswarnung
(selbstsigniertes Zertifikat, kein externer CA nötig) – einmalig bestätigen.

## Rollen

- **Vollzugriff**: Bildschirm ansehen, Prozessliste ansehen/filtern/beenden,
  Nachricht senden, Herunterfahren planen/abbrechen, Accounts verwalten.
- **Betrachter**: nur Bildschirm ansehen + Prozessliste ansehen/filtern.

## Entwicklung

```sh
# Backend (Go) bauen und testen:
cd agent && go build ./... && go vet ./...

# Frontend mit Live-Reload gegen einen lokal laufenden rc-agentd:
cd web && npm install && npm run dev
# (vite.config.ts proxied /api an https://localhost:8443)

# Alles bauen (Frontend + Go-Binaries nach ./bin):
make all
```

`rc-agentd` legt beim ersten Start automatisch ein selbstsigniertes
TLS-Zertifikat unter `<data-dir>/certs/` an. Läuft er lokal ohne Root (z.B.
zum Testen), einfach einen anderen `-data-dir`/`-socket`/`-token-file` sowie
einen Port > 1024 via `-addr` angeben.

## Sicherheitshinweise

- Der Unix-Socket zwischen `rc-agentd` und `rc-helper` ist zusätzlich zu den
  Dateisystem-Rechten (Gruppe `rc-agent`) mit einem beim ersten Start
  generierten Token abgesichert (`/etc/rc-agent/helper.token`, nur für root
  und Gruppe `rc-agent` lesbar).
- Logins werden pro IP grob gedrosselt (Brute-Force-Bremse).
- Es gibt bewusst **keine Selbstregistrierung** im Portal – Accounts werden
  ausschließlich lokal über `rc-agentctl` angelegt.
- Das letzte verbleibende Vollzugriff-Konto kann nicht gelöscht werden, um
  ein Aussperren zu verhindern.

---

🇬🇧 An English version of this document is available in
[README.md](README.md).
