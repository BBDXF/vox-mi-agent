#!/bin/sh
# Download pinned tools from tools.lock, verify SHA256, install into DEST.
# Usage: fetch-tools.sh [DEST] [ARCH]   (defaults: .tools, host arch)
# Used both on the host and inside the Dockerfile (busybox sh compatible).
set -eu

LOCK="${LOCK:-$(dirname "$0")/../tools.lock}"
DEST="${1:-.tools}"
ARCH="${2:-}"
if [ -z "$ARCH" ]; then
  case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;;
  esac
fi

mkdir -p "$DEST"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

grep -v '^#' "$LOCK" | while read -r name version arch url sha; do
  [ -n "${name:-}" ] || continue
  [ "$arch" = "$ARCH" ] || continue
  file="$TMP/$(basename "$url")"
  echo "fetch $name $version ($arch)"
  curl -fsSL --retry 5 --retry-all-errors --retry-delay 2 -o "$file" "$url"
  echo "$sha  $file" | sha256sum -c - >/dev/null
  case "$url" in
    *.tar.gz)
      mkdir -p "$TMP/x-$name"
      tar -xzf "$file" -C "$TMP/x-$name"
      bin="$(find "$TMP/x-$name" -type f -name "$name" | head -n1)"
      [ -n "$bin" ] || { echo "binary $name not found in archive" >&2; exit 1; }
      cp "$bin" "$DEST/$name" ;;
    *) cp "$file" "$DEST/$name" ;;
  esac
  chmod 0755 "$DEST/$name"
done

echo "tools installed in $DEST"
