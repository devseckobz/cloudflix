#!/usr/bin/env bash
set -Eeuo pipefail
umask 027
REPO="devseckobz/cloudflix"
BIN="/usr/local/bin/cloudflix"
SERVICE="/etc/systemd/system/cloudflix.service"
VERSION="${CLOUDFLIX_VERSION:-latest}"
die(){ printf 'ERROR: %s\n' "$*" >&2; exit 1; }
info(){ printf '[cloudflix update] %s\n' "$*"; }
[[ "${EUID}" -eq 0 ]] || die "Run with sudo: wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/update.sh | sudo bash"
[[ -f "$SERVICE" && -x "$BIN" ]] || die "Cloudflix is not installed. Run install.sh first"
case "$(uname -m)" in x86_64|amd64) ARCH="amd64";; aarch64|arm64) ARCH="arm64";; *) die "Unsupported architecture";; esac
[[ "$VERSION" == "latest" || "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-.][a-zA-Z0-9.]+)?$ ]] || die "Invalid CLOUDFLIX_VERSION"
fetch(){
 if command -v curl >/dev/null;then curl -fL --connect-timeout 15 --retry 3 --silent --show-error "$1" -o "$2"
 else wget -q --timeout=30 --tries=3 -O "$2" "$1";fi
}
command -v curl >/dev/null || command -v wget >/dev/null || die "curl or wget required"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
if [[ "$VERSION" == "latest" ]];then BASE="https://github.com/$REPO/releases/latest/download"
else BASE="https://github.com/$REPO/releases/download/$VERSION";fi
ASSET="cloudflix-linux-$ARCH.tar.gz"
info "Downloading $ASSET"
fetch "$BASE/$ASSET" "$work/$ASSET"
fetch "$BASE/$ASSET.sha256" "$work/$ASSET.sha256"
(cd "$work";sha256sum -c "$ASSET.sha256") || die "Checksum mismatch: update aborted"
tar -xzf "$work/$ASSET" -C "$work" cloudflix
install -m 0755 "$BIN" "$work/cloudflix.previous"
install -m 0755 "$work/cloudflix" "$BIN.new"
mv -f "$BIN.new" "$BIN"
info "Restarting service"
if ! systemctl restart cloudflix;then
 info "Restart failed, restoring previous binary"
 install -m 0755 "$work/cloudflix.previous" "$BIN"
 systemctl restart cloudflix || true
 die "Update rolled back"
fi
healthy=0
for i in $(seq 1 20);do
 if command -v curl >/dev/null;then
  if curl -fsS --max-time 2 http://127.0.0.1:3000/api/health >/dev/null 2>&1;then healthy=1;break;fi
 else
  if wget -q --timeout=2 -O /dev/null http://127.0.0.1:3000/api/health;then healthy=1;break;fi
 fi
 sleep 1
done
if [[ "$healthy" != "1" ]];then
 info "Health check failed, restoring previous binary"
 install -m 0755 "$work/cloudflix.previous" "$BIN"
 systemctl restart cloudflix || true
 die "Update rolled back"
fi
info "Update successful. Data and configuration preserved."
