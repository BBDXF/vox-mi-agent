#!/bin/sh
# Shared helper: write docker/.env from wizard_* vars. TODO(verify on device): wizard var passing
set -eu
F="${TRIM_APPDEST}/docker/.env"
touch "$F"
setv() { grep -v "^$1=" "$F" > "$F.tmp" || true; echo "$1=$2" >> "$F.tmp"; mv "$F.tmp" "$F"; }
[ -n "${wizard_music_dir:-}" ] && setv VOX_MUSIC "$wizard_music_dir"
setv VOX_PUBLIC_BASE_URL "${wizard_public_base_url:-}"
setv TZ "${TZ:-Asia/Shanghai}"
mkdir -p "${TRIM_PKGVAR}/data"
