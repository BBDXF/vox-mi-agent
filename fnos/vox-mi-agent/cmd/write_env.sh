#!/bin/sh
# Shared helper: write docker/.env from wizard_* vars. TODO(verify on device): wizard var passing
set -eu
F="${TRIM_APPDEST}/docker/.env"
touch "$F"
getv() { sed -n "s/^$1=//p" "$F" | tail -1; }
setv() { grep -v "^$1=" "$F" > "$F.tmp" || true; echo "$1=$2" >> "$F.tmp"; mv "$F.tmp" "$F"; }
fail() { [ -n "${TRIM_TEMP_LOGFILE:-}" ] && echo "$1" >> "$TRIM_TEMP_LOGFILE"; echo "$1" >&2; exit 1; }
[ -n "${wizard_music_dir:-}" ] && setv VOX_MUSIC "$wizard_music_dir"

# Audio/LAN port: wizard value, else the one already in .env (upgrade), else 18080.
old_port=$(getv VOX_PORT)
port="${wizard_port:-${old_port:-18080}}"
case "$port" in ''|*[!0-9]*) fail "端口无效：$port" ;; esac
if [ "$port" != "$old_port" ] && command -v ss >/dev/null 2>&1 \
   && ss -Hltn "sport = :$port" 2>/dev/null | grep -q .; then
  fail "端口 $port 已被占用，请换一个端口"
fi
setv VOX_PORT "$port"

ip="${wizard_lan_ip:-}"
[ -n "$ip" ] || ip=$(ip route get 1 2>/dev/null | awk '{for(i=1;i<NF;i++) if($i=="src"){print $(i+1);exit}}')
[ -n "$ip" ] || ip=$(hostname -I 2>/dev/null | awk '{print $1}')
if [ -n "$ip" ]; then
  setv VOX_PUBLIC_BASE_URL "http://$ip:$port"
else
  setv VOX_PUBLIC_BASE_URL ""
fi
setv TZ "${TZ:-Asia/Shanghai}"
mkdir -p "${TRIM_PKGVAR}/data"
