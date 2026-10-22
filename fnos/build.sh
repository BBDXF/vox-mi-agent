#!/bin/sh
# Usage: fnos/build.sh VERSION   -> dist/fpk/
set -eu
V="${1:?version required}"
V="${V#v}"
# fnOS wants numeric-ish version; keep git-describe suffix out
V=$(echo "$V" | sed 's/-dirty$//; s/^\([0-9][0-9.]*\).*/\1/')
case "$V" in [0-9]*) ;; *) V=0.0.0 ;; esac
HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(dirname "$HERE")
OUT="$ROOT/dist/fpk"
B="$OUT/vox-mi-agent"
rm -rf "$OUT"; mkdir -p "$OUT"
cp -R "$HERE/vox-mi-agent" "$B"
sed -i "s/@VERSION@/$V/" "$B/manifest" "$B/app/docker/.env"
cp "$ROOT/LICENSE" "$B/LICENSE"
chmod +x "$B"/cmd/*
if command -v fnpack >/dev/null 2>&1; then
  (cd "$OUT" && fnpack build vox-mi-agent)
else
  tar -C "$OUT" -czf "$OUT/vox-mi-agent_$V.tar.gz" vox-mi-agent
  echo "fnpack not found: made tarball only. Install fnpack, then: cd $OUT && fnpack build vox-mi-agent"
fi
ls -l "$OUT"
