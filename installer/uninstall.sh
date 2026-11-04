#!/usr/bin/env bash
# Uninstall Cloudflix from its standard VPS locations.
# Destructive: full removal by default, including movies and database.
set -Eeuo pipefail
umask 077

BIN="/usr/local/bin/cloudflix"
UNIT="/etc/systemd/system/cloudflix.service"
CONFIG_DIR="/etc/cloudflix"
DATA_DIR="/var/lib/cloudflix"
CADDY_SNIPPET="/etc/caddy/conf.d/cloudflix.caddy"
KEEP_DATA="${CLOUDFLIX_KEEP_DATA:-0}"

log(){ printf '[cloudflix] %s\n' "$*"; }
die(){ printf '[cloudflix] ERROR: %s\n' "$*" >&2; exit 1; }

[[ "$EUID" -eq 0 ]] || die "Run as root (e.g. wget ... | sudo bash)"
[[ "$(uname -s)" == "Linux" ]] || die "This uninstaller supports Linux only."
command -v systemctl >/dev/null 2>&1 || die "systemd is required."
[[ "$KEEP_DATA" == "0" || "$KEEP_DATA" == "1" ]] || die "CLOUDFLIX_KEEP_DATA must be 0 or 1."

# Never follow an unexpected symlink or traverse into other filesystems when
# removing persistent data. User-selected download locations are not touched.
guard_directory(){
  local dir="$1" resolved mount
  [[ -e "$dir" || -L "$dir" ]] || return 0
  [[ -d "$dir" && ! -L "$dir" ]] || die "Unexpected directory type at $dir. Review manually."
  resolved="$(realpath -e -- "$dir")" || die "Unable to resolve $dir."
  [[ "$resolved" == "$dir" ]] || die "Refusing to delete a redirected directory: $dir -> $resolved"
  # Protect both configuration and downloaded data from mounted external
  # filesystems. Command substitution preserves findmnt's failure status.
  command -v findmnt >/dev/null 2>&1 || die "findmnt is required for a safe uninstall."
  local mounts
  mounts="$(findmnt -rn -o TARGET)" || die "Cannot inspect mounted filesystems."
  while IFS= read -r mount;do
    if [[ "$mount" == "$dir" || "$mount" == "$dir/"* ]];then
      die "Mount found inside Cloudflix path ($mount). Unmount it before uninstalling."
    fi
  done <<< "$mounts"
}
guard_directory "$CONFIG_DIR"
if [[ "$KEEP_DATA" == "0" ]];then guard_directory "$DATA_DIR";fi
[[ ! -L "$UNIT" && ! -L "$BIN" ]] || die "Unexpected symlink at the service or binary path. Review manually."

log "Uninstalling Cloudflix and stopping its service..."
if [[ "$KEEP_DATA" == 0 ]];then
  log "WARNING: Permanently deleting $DATA_DIR, including downloaded movies, database and PIN."
else
  log "Keeping $DATA_DIR and the service account."
fi
if [[ -f "$UNIT" ]] || systemctl is-active --quiet cloudflix.service;then
  log "Stopping Cloudflix..."
  systemctl stop cloudflix.service || die "Unable to stop Cloudflix. No files removed."
  if systemctl is-active --quiet cloudflix.service;then
    die "Cloudflix is still active. No files removed."
  fi
  systemctl disable cloudflix.service || die "Unable to disable Cloudflix. No files removed."
fi

# Remove only installer-owned, hard-coded paths. Never remove the parent
# /etc/caddy, /etc/systemd, /usr/local/bin or /var/lib directories.
if [[ -f "$UNIT" ]];then rm -f -- "$UNIT";fi
systemctl daemon-reload
systemctl reset-failed cloudflix.service >/dev/null 2>&1 || true
if [[ -f "$BIN" ]];then rm -f -- "$BIN";fi
# An interrupted updater may leave a staged binary behind.
if [[ -f "${BIN}.new" && ! -L "${BIN}.new" ]];then rm -f -- "${BIN}.new";fi
if [[ -d "$CONFIG_DIR" ]];then rm -rf --one-file-system -- "$CONFIG_DIR";fi

if [[ "$KEEP_DATA" == "0" ]];then
  if [[ -d "$DATA_DIR" ]];then
    log "Removing Cloudflix movies and database..."
    rm -rf --one-file-system -- "$DATA_DIR"
  fi
  if command -v getent >/dev/null 2>&1 && getent passwd cloudflix >/dev/null;then
    account="$(getent passwd cloudflix)"
    IFS=: read -r username pass uid primary_gid description home shell <<< "$account"
    if [[ "$username" == "cloudflix" && "$home" == "$DATA_DIR" && ( "$shell" == "/usr/sbin/nologin" || "$shell" == "/sbin/nologin" ) ]];then
      if command -v userdel >/dev/null 2>&1 && userdel cloudflix;then
        # Only remove the group if it is the deleted account's original group,
        # has no explicit members and is not another user's primary group.
        if command -v groupdel >/dev/null 2>&1 && getent group cloudflix >/dev/null;then
          group_info="$(getent group cloudflix)"
          IFS=: read -r group_name group_pass group_gid group_members <<< "$group_info"
          if [[ "$group_name" == "cloudflix" && "$group_gid" == "$primary_gid" && -z "$group_members" ]];then
            groupdel cloudflix >/dev/null 2>&1 || log "Cloudflix group is still in use; keeping it."
          fi
        fi
      else
        log "Could not delete the cloudflix service user; review manually."
      fi
    else
      log "Keeping nonstandard cloudflix account (home/shell differ from installer settings)."
    fi
  fi
fi

if [[ -L "$CADDY_SNIPPET" ]];then
  log "Cloudflix Caddy site is a symlink; leaving it for manual review."
elif [[ -f "$CADDY_SNIPPET" ]];then
  if grep -Fq 'reverse_proxy 127.0.0.1:3000' "$CADDY_SNIPPET";then
    rm -f -- "$CADDY_SNIPPET"
    if command -v caddy >/dev/null 2>&1 && [[ -f /etc/caddy/Caddyfile ]];then
      if caddy validate --config /etc/caddy/Caddyfile >/dev/null 2>&1;then
        if systemctl is-active --quiet caddy.service;then
          systemctl reload caddy.service || log "Caddy reload failed; review its configuration."
        fi
      else
        log "Caddy configuration needs manual review (no reload performed)."
      fi
    fi
  else
    log "Keeping customized Caddy site; review $CADDY_SNIPPET manually."
  fi
fi

if [[ "$KEEP_DATA" == "0" ]];then
  log "Full uninstall finished. Cloudflix data and credentials were deleted."
else
  log "Uninstall finished; Cloudflix data was preserved."
fi
log "You can now run the installer again for a fresh deployment."
