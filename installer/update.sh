#!/usr/bin/env bash
set -Eeuo pipefail
umask 027
REPO="devseckobz/cloudflix"
BIN="/usr/local/bin/cloudflix"
SERVICE="/etc/systemd/system/cloudflix.service"
CONFIG="/etc/cloudflix/cloudflix.env"
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
install -m 0755 "$work/cloudflix" "$work/cloudflix.candidate"
command -v runuser >/dev/null || die "runuser is required for PIN configuration"
# Detect legacy installs that still have password authentication. Never reset
# an already configured PIN during a normal update.
pin_status=0
runuser -u cloudflix -- env CLOUDFLIX_DATA_DIR=/var/lib/cloudflix "$work/cloudflix.candidate" pin status >/dev/null 2>&1 || pin_status=$?
if [[ "$pin_status" == "3" ]];then
  [[ -r /dev/tty && -w /dev/tty ]] || die "Interactive terminal required to migrate existing password to a 6-digit PIN"
  info "Your existing Cloudflix login will be replaced by a 6-digit PIN."
  while true;do
    IFS= read -r -s -p "Choose Cloudflix 6-digit PIN: " PIN < /dev/tty || die "Cannot read PIN"
    printf '\n' > /dev/tty
    if [[ ! "$PIN" =~ ^[0-9]{6}$ ]];then printf 'PIN must contain six digits.\n' > /dev/tty;continue;fi
    IFS= read -r -s -p "Confirm PIN: " PIN_CONFIRM < /dev/tty || die "Cannot confirm PIN"
    printf '\n' > /dev/tty
    if [[ "$PIN" == "$PIN_CONFIRM" ]];then break;fi
    printf 'PINs do not match.\n' > /dev/tty
  done
  if ! printf '%s\n' "$PIN" | runuser -u cloudflix -- env CLOUDFLIX_DATA_DIR=/var/lib/cloudflix "$work/cloudflix.candidate" pin set;then
    unset PIN PIN_CONFIRM
    die "Cannot set the Cloudflix PIN; binary not replaced"
  fi
  unset PIN PIN_CONFIRM
elif [[ "$pin_status" != "0" ]];then
  die "Cannot inspect existing Cloudflix access settings (PIN status code $pin_status). Update aborted."
fi
install -m 0755 "$BIN" "$work/cloudflix.previous"
config_backup=""
if [[ -f "$CONFIG" ]] && grep -Eq '^CLOUDFLIX_LISTEN=127[.]0[.]0[.]1:3000
mv -f "$BIN.new" "$BIN"
info "Restarting service"
if ! systemctl restart cloudflix;then
 info "Restart failed, restoring previous binary"
 install -m 0755 "$work/cloudflix.previous" "$BIN"
 if [[ -n "$config_backup" ]];then cp -p -- "$config_backup" "$CONFIG";fi
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
 if [[ -n "$config_backup" ]];then cp -p -- "$config_backup" "$CONFIG";fi
 systemctl restart cloudflix || true
 die "Update rolled back"
fi
info "Update successful. Data and PIN preserved."
info "Open http://YOUR_VPS_IP:3000 and sign in with your 6-digit PIN."
info "Allow TCP port 3000 in your VPS firewall/provider rules for trusted IPs."
info "WARNING: Plain HTTP is unencrypted; avoid untrusted networks."
 "$CONFIG";then
  [[ ! -L "$CONFIG" ]] || die "Refusing to modify symlinked configuration"
  config_backup="$work/cloudflix.env.previous"
  cp -p -- "$CONFIG" "$config_backup"
  sed -i 's/^CLOUDFLIX_LISTEN=127[.]0[.]0[.]1:3000$/CLOUDFLIX_LISTEN=0.0.0.0:3000/' "$CONFIG"
  info "Enabled direct VPS IP access on port 3000."
fi
install -m 0755 "$work/cloudflix.candidate" "$BIN.new"
mv -f "$BIN.new" "$BIN"
info "Restarting service"
if ! systemctl restart cloudflix;then
 info "Restart failed, restoring previous binary"
 install -m 0755 "$work/cloudflix.previous" "$BIN"
 if [[ -n "$config_backup" ]];then cp -p -- "$config_backup" "$CONFIG";fi
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
 if [[ -n "$config_backup" ]];then cp -p -- "$config_backup" "$CONFIG";fi
 systemctl restart cloudflix || true
 die "Update rolled back"
fi
info "Update successful. Data and configuration preserved. Sign in with your 6-digit PIN."
