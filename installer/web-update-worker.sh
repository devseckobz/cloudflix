#!/usr/bin/env bash
# Root-owned service helper. It accepts no arguments or URLs from the web.
set -Eeuo pipefail
umask 077

REQUEST=/var/lib/cloudflix/update.request
STATUS_DIR=/var/lib/cloudflix-update
UPDATER=/usr/local/libexec/cloudflix/update.sh
STATUS="$STATUS_DIR/status.json"

# The unit runs as root. The cloudflix process can only create the trigger.
[[ "$EUID" -eq 0 ]] || exit 1
[[ -f "$UPDATER" && ! -L "$UPDATER" ]] || exit 1
install -d -o root -g root -m 0755 "$STATUS_DIR"
[[ -L "$REQUEST" ]] && exit 1
[[ -f "$REQUEST" ]] || exit 0
# Remove the trigger immediately so PathExists doesn't repeatedly activate.
rm -f -- "$REQUEST"

write_status(){
 local state="$1" message="$2" tmp
 tmp="$(mktemp "$STATUS_DIR/.status.XXXXXXXX")"
 printf '{"state":"%s","message":"%s","updatedAt":%s}\n' "$state" "$message" "$(date +%s)" > "$tmp"
 chmod 0644 "$tmp"
 mv -f -- "$tmp" "$STATUS"
}
write_status running 'Downloading and installing the latest Cloudflix release'
if CLOUDFLIX_NONINTERACTIVE=1 /bin/bash "$UPDATER" > "$STATUS_DIR/last-update.log" 2>&1;then
 write_status success 'Cloudflix was updated successfully'
 printf '[cloudflix updater] Update completed successfully.\n'
else
 code="$?"
 write_status error 'Update failed; check sudo journalctl -u cloudflix-update or the VPS update log'
 printf '[cloudflix updater] Update failed with exit status %s.\n' "$code" >&2
 # Do not expose root execution errors or local paths to the web UI.
 tail -n 8 "$STATUS_DIR/last-update.log" >&2 || true
 exit "$code"
fi
