#!/usr/bin/env bash
set -Eeuo pipefail
umask 027

REPO="devseckobz/cloudflix"
BIN="/usr/local/bin/cloudflix"
DATA="/var/lib/cloudflix"
CONFIG="/etc/cloudflix/cloudflix.env"
SERVICE="/etc/systemd/system/cloudflix.service"
DOMAIN="${CLOUDFLIX_DOMAIN:-}"
VERSION="${CLOUDFLIX_VERSION:-latest}"

die(){ printf 'ERROR: %s\n' "$*" >&2; exit 1; }
info(){ printf '[cloudflix] %s\n' "$*"; }
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
if [[ -n "$DOMAIN" && ! "$DOMAIN" =~ ^[a-zA-Z0-9]([a-zA-Z0-9.-]*[a-zA-Z0-9])?$ ]]; then die "Invalid CLOUDFLIX_DOMAIN"; fi
if [[ -n "${TMDB_API_KEY:-}" && ! "${TMDB_API_KEY}" =~ ^[a-zA-Z0-9_.-]+$ ]];then die "TMDB_API_KEY contains unsupported characters";fi
[[ ! -e "$SERVICE" ]] || die "Cloudflix is installed. Use update.sh instead."
command -v runuser >/dev/null || die "runuser is required"

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
if [[ -z "$DOMAIN" ]];then
  IFS= read -r -p "HTTPS domain (Enter for private SSH access): " DOMAIN < /dev/tty || die "Could not read domain"
  if [[ -n "$DOMAIN" && ! "$DOMAIN" =~ ^[a-zA-Z0-9]([a-zA-Z0-9.-]*[a-zA-Z0-9])?$ ]];then die "Invalid domain";fi
fi

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
 (umask 077; printf 'CLOUDFLIX_DATA_DIR=/var/lib/cloudflix\nCLOUDFLIX_LISTEN=127.0.0.1:3000\nTMDB_API_KEY=%s\n' "${TMDB_API_KEY:-}" > "$CONFIG")
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
if [[ -n "$DOMAIN" ]];then
 command -v apt-get >/dev/null || die "Domain HTTPS automation requires an apt-based distro; Cloudflix installed on localhost"
 info "Configuring Caddy HTTPS for $DOMAIN"
 export DEBIAN_FRONTEND=noninteractive
 apt-get update -qq
 apt-get install -y -qq caddy
 install -d -m 0755 /etc/caddy/conf.d
 printf '%s {\n    encode zstd gzip\n    reverse_proxy 127.0.0.1:3000\n}\n' "$DOMAIN" > /etc/caddy/conf.d/cloudflix.caddy
 if ! grep -Fq 'import /etc/caddy/conf.d/*.caddy' /etc/caddy/Caddyfile;then
  printf '\nimport /etc/caddy/conf.d/*.caddy\n' >> /etc/caddy/Caddyfile
 fi
 caddy validate --config /etc/caddy/Caddyfile >/dev/null
 systemctl enable --now caddy
 systemctl reload caddy
 info "Cloudflix is ready at https://$DOMAIN — sign in with your 6-digit PIN."
else
 info "Installed securely on 127.0.0.1:3000."
 info "From your computer use: ssh -L 3000:127.0.0.1:3000 USER@VPS_IP"
 info "Then open http://localhost:3000 and sign in with your 6-digit PIN."
 info "Set CLOUDFLIX_DOMAIN before installation to configure public HTTPS."
fi
info "Completed. systemctl status cloudflix"
