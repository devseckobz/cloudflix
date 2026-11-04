#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

die(){ printf '[cloudflix] ERROR: %s\n' "$*" >&2; exit 1; }
[[ "$EUID" -eq 0 ]] || die "Run as root with sudo"
[[ -x /usr/local/bin/cloudflix ]] || die "Cloudflix is not installed"
command -v runuser >/dev/null || die "runuser is required"
[[ -r /dev/tty && -w /dev/tty ]] || die "An interactive terminal is required"
while true;do
  IFS= read -r -s -p "New 6-digit Cloudflix PIN: " PIN < /dev/tty || die "Failed to read PIN"
  printf '\n' > /dev/tty
  if [[ ! "$PIN" =~ ^[0-9]{6}$ ]];then printf 'Use exactly six digits.\n' > /dev/tty;continue;fi
  IFS= read -r -s -p "Confirm new PIN: " PIN2 < /dev/tty || die "Failed to confirm PIN"
  printf '\n' > /dev/tty
  if [[ "$PIN" == "$PIN2" ]];then break;fi
  printf 'PINs do not match.\n' > /dev/tty
done
printf '%s\n' "$PIN" | runuser -u cloudflix -- env CLOUDFLIX_DATA_DIR=/var/lib/cloudflix /usr/local/bin/cloudflix pin set || die "Could not set PIN"
unset PIN PIN2
systemctl restart cloudflix
printf '[cloudflix] PIN updated. Existing sessions have been signed out.\n'
