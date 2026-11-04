#!/usr/bin/env bash
set -Eeuo pipefail
umask 027

REPO="devseckobz/cloudflix"
BIN="/usr/local/bin/cloudflix"
DATA="/var/lib/cloudflix"
CONFIG="/etc/cloudflix/cloudflix.env"
SERVICE="/etc/systemd/system/cloudflix.service"
VERSION="${CLOUDFLIX_VERSION:-latest}"

die(){ printf 'ERROR: %s\n' "$*" >&2; exit 1; }
info(){ printf '[cloudflix] %s\n' "$*"; }

# Display the VPS's actual IP after setup (advisory only; never changes routing).
valid_ipv4(){
 local addr="$1" a b c d extra n
 [[ "$addr" =~ ^[0-9]+[.][0-9]+[.][0-9]+[.][0-9]+$ ]] || return 1
 IFS=. read -r a b c d extra <<< "$addr"
 for n in "$a" "$b" "$c" "$d";do
   (("${#n}" <= 3 && 10#$n <= 255)) || return 1
 done
}
detect_vps_ip(){
 local remote="" local_ip="" candidate=""
 if command -v curl >/dev/null 2>&1;then
   remote="$(curl -4fsS --connect-timeout 3 --max-time 6 https://api.ipify.org 2>/dev/null || true)"
 elif command -v wget >/dev/null 2>&1;then
   remote="$(wget -4 -q -T 6 -O - https://api.ipify.org 2>/dev/null || true)"
 fi
 if valid_ipv4 "$remote";then
   printf '%s' "$remote"
   return 0
 fi
 if command -v ip >/dev/null 2>&1;then
   candidate="$(ip -4 route get 1.1.1.1 2>/dev/null || true)"
   if [[ "$candidate" =~ [[:space:]]src[[:space:]]([0-9.]+) ]];then
      local_ip="${BASH_REMATCH[1]}"
   fi
 fi
 if ! valid_ipv4 "$local_ip" && command -v hostname >/dev/null 2>&1;then
   local_ip="$(hostname -I 2>/dev/null | awk '{print $1}' || true)"
 fi
 if valid_ipv4 "$local_ip";then
   printf '%s' "$local_ip"
   return 0
 fi
 return 1
}
print_access_url(){
 local address=""
 address="$(detect_vps_ip)" || address=""
 if [[ -n "$address" ]];then
   info "Open http://$address and sign in with your 6-digit PIN."
 else
   info "Could not detect the VPS IP; check 'ip -4 addr' or your VPS provider dashboard."
 fi
}

[[ "${EUID}" -eq 0 ]] || die "Run with root privileges: wget -qO- https://raw.githubusercontent.com/devseckobz/cloudflix/main/installer/install.sh | sudo bash"
command -v systemctl >/dev/null || die "systemd is required"
[[ "$(uname -s)" == "Linux" ]] || die "Linux required"
case "$(uname -m)" in
 x86_64|amd64) ARCH="amd64";;
 aarch64|arm64) ARCH="arm64";;
 *) die "Supported architectures: amd64, arm64";;
esac
command -v sha256sum >/dev/null || die "sha256sum is required"
command -v tar >/dev/null || die "tar is required"
command -v curl >/dev/null || command -v wget >/dev/null || die "curl or wget is required"
[[ "$VERSION" == "latest" || "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-.][a-zA-Z0-9.]+)?$ ]] || die "Invalid CLOUDFLIX_VERSION"
[[ ! -e "$SERVICE" ]] || die "Cloudflix is installed. Use update.sh instead."
command -v runuser >/dev/null || die "runuser is required"
command -v ss >/dev/null || die "ss (iproute2) is required to check port 80."
# Fail before prompting for a PIN or making any service or filesystem changes.
port80_listeners="$(ss -H -ltnp '( sport = :80 )')" || die "Could not inspect TCP port 80."
if [[ -n "$port80_listeners" ]];then
  printf '\n[cloudflix] TCP port 80 is already in use by:\n%s\n\n' "$port80_listeners" >&2
  die "Stop or reconfigure the process above and retry. Nothing was installed."
fi
info "Port 80 is available."

# wget/curl uses stdin for the script. Always prompt through the controlling terminal.
[[ -r /dev/tty && -w /dev/tty ]] || die "Interactive terminal required to choose a six-digit PIN"
choose_pin(){
  local first second
  while true;do
    IFS= read -r -s -p "Choose Cloudflix 6-digit PIN: " first < /dev/tty || die "Could not read PIN"
    printf '\n' > /dev/tty
    if [[ ! "$first" =~ ^[0-9]{6}$ ]];then
      printf 'PIN must be exactly six digits.\n' > /dev/tty
      continue
    fi
    IFS= read -r -s -p "Confirm Cloudflix PIN: " second < /dev/tty || die "Could not confirm PIN"
    printf '\n' > /dev/tty
    if [[ "$first" != "$second" ]];then
      printf 'PINs did not match. Try again.\n' > /dev/tty
      continue
    fi
    PIN="$first"
    unset first second
    return
  done
}
choose_pin

fetch(){ local src="$1" dst="$2"
 if command -v curl >/dev/null;then curl -fL --connect-timeout 15 --retry 3 --silent --show-error "$src" -o "$dst"
 else wget -q --timeout=30 --tries=3 -O "$dst" "$src";fi
}
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
if [[ "$VERSION" == "latest" ]];then
 BASE="https://github.com/$REPO/releases/latest/download"
else
 BASE="https://github.com/$REPO/releases/download/$VERSION"
fi
ASSET="cloudflix-linux-$ARCH.tar.gz"
info "Downloading $ASSET from GitHub Releases"
fetch "$BASE/$ASSET" "$work/$ASSET" || die "No release found. Check https://github.com/$REPO/releases"
fetch "$BASE/$ASSET.sha256" "$work/$ASSET.sha256"
(cd "$work"; sha256sum -c "$ASSET.sha256") || die "Release integrity check failed"
tar -xzf "$work/$ASSET" -C "$work" cloudflix
[[ -f "$work/cloudflix" ]] || die "Release missing executable"
if ! id cloudflix >/dev/null 2>&1;then
 useradd --system --home-dir "$DATA" --shell /usr/sbin/nologin cloudflix
fi
install -d -o cloudflix -g cloudflix -m 0700 "$DATA"
install -d -o root -g root -m 0700 /etc/cloudflix
install -m 0755 "$work/cloudflix" "$BIN"
# Configure the PIN with the service account before starting the HTTP server.
if ! printf '%s\n' "$PIN" | runuser -u cloudflix -- env CLOUDFLIX_DATA_DIR="$DATA" "$BIN" pin set;then
  unset PIN
  die "Could not configure Cloudflix access PIN"
fi
unset PIN
if [[ ! -f "$CONFIG" ]];then
 # We do not log or print the secret.
 (umask 077; printf 'CLOUDFLIX_DATA_DIR=/var/lib/cloudflix\nCLOUDFLIX_LISTEN=0.0.0.0:80\n' > "$CONFIG")
fi
chmod 0600 "$CONFIG"
cat > "$SERVICE" <<'UNIT'
[Unit]
Description=Cloudflix movie download manager
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=cloudflix
Group=cloudflix
EnvironmentFile=/etc/cloudflix/cloudflix.env
ExecStart=/usr/local/bin/cloudflix
Restart=on-failure
RestartSec=5
TimeoutStopSec=35
NoNewPrivileges=true
AmbientCapabilities=CAP_NET_BIND_SERVICE
CapabilityBoundingSet=CAP_NET_BIND_SERVICE
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/cloudflix
UMask=0077
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
UNIT
systemctl daemon-reload
systemctl enable --now cloudflix.service
healthy=0
for i in $(seq 1 15);do
  if command -v curl >/dev/null;then
    if curl -fsS --max-time 2 http://127.0.0.1/api/health >/dev/null 2>&1;then healthy=1;break;fi
  else
    if wget -q --timeout=2 -O /dev/null http://127.0.0.1/api/health;then healthy=1;break;fi
  fi
  if ! systemctl is-active --quiet cloudflix.service;then break;fi
  sleep 1
done
if [[ "$healthy" != "1" ]];then
  systemctl status cloudflix.service --no-pager >&2 || true
  die "Cloudflix did not start on port 80. Check: journalctl -u cloudflix -n 80"
fi
info "Cloudflix is listening on all VPS interfaces at port 80."
print_access_url
info "Make sure your VPS firewall/provider allows TCP port 80 from your trusted IP."
info "WARNING: HTTP is unencrypted; the PIN, session cookies and TMDB token may be intercepted."
info "Completed. systemctl status cloudflix"
