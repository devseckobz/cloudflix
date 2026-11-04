#!/usr/bin/env bash
# Install the root-owned, fixed-purpose service responsible for web updates.
set -Eeuo pipefail
umask 022
[[ "$EUID" -eq 0 ]] || { echo '[cloudflix] Root privileges required for updater setup.' >&2; exit 1; }

BASE='https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer'
HELPERS=/usr/local/libexec/cloudflix
UNITS=/etc/systemd/system
STATUS=/var/lib/cloudflix-update

fetch(){
 if command -v curl >/dev/null 2>&1;then
  curl --proto '=https' --tlsv1.2 -fLsS --connect-timeout 12 --retry 3 "$1" -o "$2"
 elif command -v wget >/dev/null 2>&1;then
  wget -q --timeout=30 --tries=3 -O "$2" "$1"
 else
  echo '[cloudflix] curl or wget required' >&2
  return 1
 fi
}

[[ -f /etc/systemd/system/cloudflix.service ]] || { echo '[cloudflix] Cloudflix must be installed first.' >&2;exit 1; }
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT
fetch "$BASE/update.sh" "$work/update.sh"
fetch "$BASE/web-update-worker.sh" "$work/web-update-worker.sh"
/bin/bash -n "$work/update.sh" "$work/web-update-worker.sh"
install -d -o root -g root -m 0755 "$HELPERS" "$STATUS"
# Atomically replace the helpers, preserving any script already executing.
for name in update.sh web-update-worker.sh;do
 install -o root -g root -m 0755 "$work/$name" "$HELPERS/$name.next"
 mv -f -- "$HELPERS/$name.next" "$HELPERS/$name"
done
cat > "$UNITS/cloudflix-update.service" <<'UNIT'
[Unit]
Description=Cloudflix GitHub Release updater
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
User=root
Group=root
ExecStart=/usr/local/libexec/cloudflix/web-update-worker.sh
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
TimeoutStartSec=300
UNIT

cat > "$UNITS/cloudflix-update.path" <<'UNIT'
[Unit]
Description=Watch for authorized Cloudflix web update requests
After=cloudflix.service

[Path]
PathExists=/var/lib/cloudflix/update.request
Unit=cloudflix-update.service

[Install]
WantedBy=multi-user.target
UNIT

chmod 0644 "$UNITS/cloudflix-update.path" "$UNITS/cloudflix-update.service"
systemctl daemon-reload
systemctl enable --now cloudflix-update.path
printf '[cloudflix] Web updater enabled. Use Settings > Software Updates.\n'
