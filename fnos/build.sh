#!/bin/sh
# Usage: fnos/build.sh VERSION   -> dist/fpk/
#   Always builds the online package vox-mi-agent_<V>.fpk (platform=all, image pulled at start).
#   With FPK_IMAGE=<image ref> (e.g. ghcr.io/bbdxf/vox-mi-agent:0.2.3) also builds offline packages
#   vox-mi-agent_<V>_x86.fpk / _arm.fpk embedding that image (docker save) for the given arch.
set -eu
V="${1:?version required}"
V="${V#v}"
# fnOS wants numeric-ish version; keep git-describe suffix out
V=$(echo "$V" | sed 's/-dirty$//; s/^\([0-9][0-9.]*\).*/\1/')
case "$V" in [0-9]*) ;; *) V=0.0.0 ;; esac
HERE=$(cd "$(dirname "$0")" && pwd)
ROOT=$(dirname "$HERE")
OUT="$ROOT/dist/fpk"
rm -rf "$OUT"; mkdir -p "$OUT"

# pkg NAME PLATFORM [DOCKER_ARCH]
pkg() {
  B="$OUT/work/vox-mi-agent"
  rm -rf "$OUT/work"; mkdir -p "$OUT/work"
  cp -R "$HERE/vox-mi-agent" "$B"
  sed -i "s/@VERSION@/$V/" "$B/manifest" "$B/app/docker/.env"
  sed -i "s/^platform=.*/platform=$2/" "$B/manifest"
  cp "$ROOT/LICENSE" "$B/LICENSE"
  chmod +x "$B"/cmd/*
  if [ -n "${3:-}" ]; then
    docker pull -q --platform "linux/$3" "$FPK_IMAGE" >/dev/null
    # tag as compose expects so `docker load` makes it available without network
    docker tag "$FPK_IMAGE" "ghcr.io/bbdxf/vox-mi-agent:$V"
    docker save --platform "linux/$3" "ghcr.io/bbdxf/vox-mi-agent:$V" | gzip -1 > "$B/app/docker/image.tar.gz"
    docker rmi "$FPK_IMAGE" "ghcr.io/bbdxf/vox-mi-agent:$V" >/dev/null 2>&1 || true
  fi
  if command -v fnpack >/dev/null 2>&1; then
    (cd "$B" && fnpack build && mv ./*.fpk "$OUT/$1.fpk")
  else
    tar -C "$OUT/work" -czf "$OUT/$1.tar.gz" vox-mi-agent
    echo "fnpack not found: made tarball only ($1.tar.gz)"
  fi
}

pkg "vox-mi-agent_$V" all
if [ -n "${FPK_IMAGE:-}" ]; then
  pkg "vox-mi-agent_${V}_x86" x86 amd64
  pkg "vox-mi-agent_${V}_arm" arm arm64
fi
rm -rf "$OUT/work"
ls -l "$OUT"
