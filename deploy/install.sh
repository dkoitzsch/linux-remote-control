#!/usr/bin/env bash
# Installs linux-remote-control on this Fedora machine (the CHILD's PC):
# builds/copies the binaries, sets up the shared group so rc-helper (running
# as the child's own user) can reach rc-agentd's privileged socket, installs
# the systemd system service, and adds the KDE autostart entry for
# rc-helper. Run as root: `sudo ./deploy/install.sh --child-user <name>`.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PREFIX=/usr/local/bin
GROUP=rc-agent

CHILD_USER=""
SKIP_BUILD=0

usage() {
  cat <<EOF
Verwendung: sudo $0 --child-user <linux-benutzername> [--skip-build]

  --child-user NAME   Der lokale Linux-Benutzername des Kindes (dessen
                       KDE-Sitzung rc-helper für Screenshots/Overlays
                       benötigt).
  --skip-build         Nimmt bereits gebaute Binaries aus ./bin statt neu
                       zu bauen (erfordert vorheriges 'make').
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --child-user) CHILD_USER="$2"; shift 2 ;;
    --skip-build) SKIP_BUILD=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unbekannte Option: $1" >&2; usage; exit 1 ;;
  esac
done

if [[ $EUID -ne 0 ]]; then
  echo "Bitte mit sudo/als root ausführen." >&2
  exit 1
fi

if [[ -z "$CHILD_USER" ]]; then
  echo "Fehler: --child-user ist erforderlich." >&2
  usage
  exit 1
fi

if ! id "$CHILD_USER" &>/dev/null; then
  echo "Fehler: Linux-Benutzer '$CHILD_USER' existiert nicht." >&2
  exit 1
fi

if [[ "$SKIP_BUILD" -eq 0 ]]; then
  echo "==> Baue Frontend und Agent…"
  make -C "$REPO_ROOT" all
fi

for bin in rc-agentd rc-helper rc-overlay rc-agentctl; do
  if [[ ! -x "$REPO_ROOT/bin/$bin" ]]; then
    echo "Fehler: $REPO_ROOT/bin/$bin fehlt. Vorher 'make' ausführen oder --skip-build weglassen." >&2
    exit 1
  fi
done

echo "==> Installiere Binaries nach $PREFIX"
install -m 0755 "$REPO_ROOT/bin/rc-agentd" "$PREFIX/rc-agentd"
install -m 0755 "$REPO_ROOT/bin/rc-helper" "$PREFIX/rc-helper"
install -m 0755 "$REPO_ROOT/bin/rc-overlay" "$PREFIX/rc-overlay"
install -m 0755 "$REPO_ROOT/bin/rc-agentctl" "$PREFIX/rc-agentctl"

echo "==> Richte Gruppe '$GROUP' ein und füge '$CHILD_USER' hinzu"
getent group "$GROUP" >/dev/null || groupadd --system "$GROUP"
usermod -aG "$GROUP" "$CHILD_USER"

echo "==> Lege Verzeichnisse an"
install -d -m 0755 /var/lib/rc-agent
install -d -m 0755 /etc/rc-agent

echo "==> Installiere systemd-Dienst rc-agentd"
install -m 0644 "$REPO_ROOT/deploy/rc-agentd.service" /etc/systemd/system/rc-agentd.service
systemctl daemon-reload
systemctl enable --now rc-agentd

echo "==> Richte rc-helper-Autostart für '$CHILD_USER' ein (KDE-Sitzung)"
CHILD_HOME="$(getent passwd "$CHILD_USER" | cut -d: -f6)"
install -d -m 0755 "$CHILD_HOME/.config/autostart"
install -m 0644 "$REPO_ROOT/deploy/rc-helper.desktop" "$CHILD_HOME/.config/autostart/rc-helper.desktop"
chown -R "$CHILD_USER:" "$CHILD_HOME/.config/autostart/rc-helper.desktop"

if command -v firewall-cmd &>/dev/null && systemctl is-active --quiet firewalld; then
  echo "==> Öffne Port 8443/tcp in firewalld (nur im Heimnetz relevant)"
  firewall-cmd --add-port=8443/tcp --permanent
  firewall-cmd --reload
fi

cat <<EOF

==============================================================================
Installation abgeschlossen.

Nächste Schritte:
1. Ersten Vollzugriff-Account anlegen:
     sudo rc-agentctl create-user -username <name> -role full

2. '$CHILD_USER' muss sich einmal ab- und wieder anmelden, damit
   - die neue Gruppenmitgliedschaft ('$GROUP') wirksam wird und
   - rc-helper über den KDE-Autostart gestartet wird.

3. Portal im Browser öffnen (auf dem Rechner der Eltern, gleiches Heimnetz):
     https://<hostname-oder-ip-des-kind-pcs>:8443
   Der Browser zeigt beim ersten Aufruf eine Zertifikatswarnung (selbst-
   signiertes Zertifikat) -- das ist erwartet, einmalig bestätigen.

Logs: journalctl -u rc-agentd -f
==============================================================================
EOF
