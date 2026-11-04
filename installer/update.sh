#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

REPO="devseckobz/cloudflix"
BIN="/usr/local/bin/cloudflix"
SERVICE="/etc/systemd/system/cloudflix.service"
CONFIG="/etc/cloudflix/cloudflix.env"
VERSION="${CLOUDFLIX_VERSION:-latest}"

die(){ printf '[cloudflix update] ERROR: %s\n' "$*" >&2; exit 1; }
info(){ printf '[cloudflix update] %s\n' "$*"; }

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

[[ "$EUID" -eq 0 ]] || die "Run with sudo."
[[ -f "$SERVICE" && ! -L "$SERVICE" && -x "$BIN" && ! -L "$BIN" ]] || die "Cloudflix is not installed in the expected locations."
[[ -f "$CONFIG" && ! -L "$CONFIG" ]] || die "Cloudflix configuration file is missing or a symlink."
[[ "$(uname -s)" == "Linux" ]] || die "Linux is required."
case "$(uname -m)" in
 x86_64|amd64) ARCH=amd64;;
 aarch64|arm64) ARCH=arm64;;
 *) die "Unsupported architecture. Only linux/amd64 and linux/arm64 are available.";;
esac
[[ "$VERSION" == "latest" || "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-.][a-zA-Z0-9.]+)?$ ]] || die "Invalid version."
for command_name in ss sha256sum tar systemctl runuser flock;do
 command -v "$command_name" >/dev/null || die "Required command not found: $command_name"
done
command -v curl >/dev/null || command -v wget >/dev/null || die "curl or wget is required."
# Root-owned lock path prevents overlapping shell and Settings updates.
exec 9>/run/cloudflix-update.lock
flock -n 9 || die "Another Cloudflix update is already running."

# Port 80 must not be owned by unrelated software. During an update, the
# existing Cloudflix service itself is allowed to occupy this port.
verify_port80(){
 local listeners mainpid line pid allpids foreign
 listeners="$(ss -H -ltnp '( sport = :80 )')" || die "Could not inspect TCP port 80."
 [[ -n "$listeners" ]] || return 0
 mainpid="$(systemctl show cloudflix.service -p MainPID --value)" || die "Cannot identify Cloudflix PID."
 foreign=0
 while IFS= read -r line;do
   [[ -z "$line" ]] && continue
   allpids="$(grep -oE 'pid=[0-9]+' <<< "$line" || true)"
   if [[ -z "$allpids" || ! "$mainpid" =~ ^[0-9]+$ || "$mainpid" == 0 ]];then
     foreign=1
     continue
   fi
   while IFS= read -r pid;do
     [[ "${pid#pid=}" == "$mainpid" ]] || foreign=1
   done <<< "$allpids"
 done <<< "$listeners"
 if [[ "$foreign" == 1 ]];then
   printf '\n[cloudflix update] TCP port 80 is used by another process:\n%s\n\n' "$listeners" >&2
   die "Stop or reconfigure that service first. Nothing was updated."
 fi
}
verify_port80

fetch(){
 if command -v curl >/dev/null;then
   curl -fL --connect-timeout 15 --retry 3 --silent --show-error "$1" -o "$2"
 else
   wget -q --timeout=30 --tries=3 -O "$2" "$1"
 fi
}
work="$(mktemp -d)"
trap 'rm -rf "$work"; if [[ -f "$BIN.new" && ! -L "$BIN.new" ]];then rm -f -- "$BIN.new";fi' EXIT
if [[ "$VERSION" == "latest" ]];then
  BASE="https://github.com/$REPO/releases/latest/download"
else
  BASE="https://github.com/$REPO/releases/download/$VERSION"
fi
ASSET="cloudflix-linux-$ARCH.tar.gz"
info "Downloading $ASSET"
fetch "$BASE/$ASSET" "$work/$ASSET" || die "Release download failed."
fetch "$BASE/$ASSET.sha256" "$work/$ASSET.sha256" || die "Release checksum download failed."
(cd "$work" && sha256sum -c "$ASSET.sha256") || die "Checksum mismatch. Update aborted."
tar -xzf "$work/$ASSET" -C "$work" cloudflix
[[ -f "$work/cloudflix" ]] || die "Release archive did not contain the Cloudflix binary."
# The mktemp directory is root-only (0700); the cloudflix service user
# cannot traverse it. Stage the candidate in root-owned /usr/local/bin before
# calling the candidate's PIN migration command as the cloudflix user.
[[ ! -L "$BIN.new" ]] || die "Unexpected symlink at $BIN.new"
install -m 0755 "$work/cloudflix" "$BIN.new"

# Older password-based installations are migrated to PIN authentication only
# when needed; do not ask existing PIN users for a new code.
pin_status=0
runuser -u cloudflix -- env CLOUDFLIX_DATA_DIR=/var/lib/cloudflix "$BIN.new" pin status >/dev/null 2>&1 || pin_status=$?
if [[ "$pin_status" == 3 ]];then
  if [[ "${CLOUDFLIX_NONINTERACTIVE:-0}" == 1 ]];then
    die "The existing installation needs a 6-digit PIN. Run update.sh from the VPS terminal first."
  fi
  [[ -r /dev/tty && -w /dev/tty ]] || die "An interactive terminal is required for the six-digit PIN migration."
  info "Your old login must be replaced with a six-digit PIN."
  while true;do
    IFS= read -r -s -p "New Cloudflix PIN (6 digits): " PIN </dev/tty || die "Could not read PIN."
    printf '\n' >/dev/tty
    [[ "$PIN" =~ ^[0-9]{6}$ ]] || { printf 'Exactly six digits required.\n' >/dev/tty;continue; }
    IFS= read -r -s -p "Confirm new PIN: " PIN_CONFIRM </dev/tty || die "Could not confirm PIN."
    printf '\n' >/dev/tty
    if [[ "$PIN" == "$PIN_CONFIRM" ]];then break;fi
    printf 'PINs did not match.\n' >/dev/tty
  done
  printf '%s\n' "$PIN" | runuser -u cloudflix -- env CLOUDFLIX_DATA_DIR=/var/lib/cloudflix "$BIN.new" pin set || die "PIN migration failed."
  unset PIN PIN_CONFIRM
elif [[ "$pin_status" != 0 ]];then
  die "Could not check existing PIN configuration (exit status $pin_status)."
fi

verify_port80
cp -p -- "$BIN" "$work/cloudflix.previous"
cp -p -- "$SERVICE" "$work/cloudflix.service.previous"
cp -p -- "$CONFIG" "$work/cloudflix.env.previous"

rollback(){
 info "Rolling back the executable and systemd configuration."
 install -m 0755 "$work/cloudflix.previous" "$BIN"
 cp -p -- "$work/cloudflix.env.previous" "$CONFIG"
 cp -p -- "$work/cloudflix.service.previous" "$SERVICE"
 systemctl daemon-reload
 systemctl restart cloudflix.service || true
}
if ! grep -q '^ExecStart=/usr/local/bin/cloudflix$' "$SERVICE";then
 die "Nonstandard Cloudflix systemd unit; refusing automatic modification."
fi
# Add just the permission needed for a non-root process to bind port 80.
if ! grep -q '^AmbientCapabilities=CAP_NET_BIND_SERVICE$' "$SERVICE";then
  if ! grep -q '^NoNewPrivileges=true$' "$SERVICE";then
    die "Unexpected systemd service layout; cannot safely add port-80 capability."
  fi
  sed -i '/^NoNewPrivileges=true$/a AmbientCapabilities=CAP_NET_BIND_SERVICE\nCapabilityBoundingSet=CAP_NET_BIND_SERVICE' "$SERVICE"
fi

# Only migrate standard previous defaults. Leave explicitly customized binds
# untouched and retain all other configuration (including environment secrets).
if grep -Eq '^CLOUDFLIX_LISTEN=(127[.]0[.]0[.]1|0[.]0[.]0):3000$' "$CONFIG";then
  sed -i -E 's/^CLOUDFLIX_LISTEN=(127[.]0[.]0[.]1|0[.]0[.]0):3000$/CLOUDFLIX_LISTEN=0.0.0.0:80/' "$CONFIG"
  info "Moved Cloudflix from port 3000 to HTTP port 80."
fi

# A custom port is supported but must be numeric for a reliable health check.
listen="$(sed -n 's/^CLOUDFLIX_LISTEN=//p' "$CONFIG" | tail -n 1)"
[[ -n "$listen" ]] || listen="0.0.0.0:80"
health_port="${listen##*:}"
[[ "$health_port" =~ ^[0-9]{1,5}$ ]] && ((health_port>0 && health_port<=65535)) || { rollback;die "Invalid configured listen port."; }
health_host=127.0.0.1
case "$listen" in
 0.0.0.0:*|127.0.0.1:*) ;;
 *:*) health_host="${listen%:*}";;
esac

mv -f "$BIN.new" "$BIN"
systemctl daemon-reload
if ! systemctl restart cloudflix.service;then
 rollback
 die "Cloudflix failed to restart; prior version restored."
fi
healthy=0
for i in $(seq 1 20);do
 if command -v curl >/dev/null;then
   if curl -fsS --max-time 2 "http://$health_host:$health_port/api/health" >/dev/null 2>&1;then healthy=1;break;fi
 else
   if wget -q --timeout=2 -O /dev/null "http://$health_host:$health_port/api/health";then healthy=1;break;fi
 fi
 sleep 1
done
if [[ "$healthy" != 1 ]];then
 rollback
 die "The health check failed; restored the previous version."
fi
info "Update completed. Movies, PIN and TMDB settings preserved."
# Use only root-owned helper scripts for browser-triggered updates.
# A setup failure must not roll back an otherwise healthy application.
if fetch "https://raw.githubusercontent.com/$REPO/main/installer/setup-web-updater.sh" "$work/setup-web-updater.sh";then
 if ! /bin/bash "$work/setup-web-updater.sh";then
  info "WARNING: Could not enable the Settings updater. The one-line updater still works."
 fi
else
 info "WARNING: Could not fetch the Settings updater service. Retry the one-line updater later."
fi
print_access_url
info "Allow inbound TCP 80 only from trusted IP addresses where possible."
info "WARNING: HTTP does not encrypt PINs, session cookies or API tokens."
