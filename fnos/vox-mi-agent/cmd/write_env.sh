#!/bin/sh
# Shared helper: write docker/.env from wizard_* vars. TODO(verify on device): wizard var passing
set -eu
F="${TRIM_APPDEST}/docker/.env"
touch "$F"
setv() { grep -v "^$1=" "$F" > "$F.tmp" || true; echo "$1=$2" >> "$F.tmp"; mv "$F.tmp" "$F"; }
[ -n "${wizard_music_dir:-}" ] && setv VOX_MUSIC "$wizard_music_dir"
ip="${wizard_lan_ip:-}"
[ -n "$ip" ] || ip=$(ip route get 1 2>/dev/null | awk '{for(i=1;i<NF;i++) if($i=="src"){print $(i+1);exit}}')
[ -n "$ip" ] || ip=$(hostname -I 2>/dev/null | awk '{print $1}')
if [ -n "$ip" ]; then
  setv VOX_PUBLIC_BASE_URL "http://$ip:${TRIM_SERVICE_PORT:-18080}"
else
  setv VOX_PUBLIC_BASE_URL ""
fi
setv TZ "${TZ:-Asia/Shanghai}"
mkdir -p "${TRIM_PKGVAR}/data"
