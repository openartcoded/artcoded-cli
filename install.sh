#!/bin/sh
# Installs or updates the artcoded cli (linux, x86_64 or arm64).
#
#   curl -fsSL https://raw.githubusercontent.com/<owner>/<repo>/main/install.sh | sh
#   curl -fsSL .../install.sh | sh -s -- edge        # the latest build of main
#   curl -fsSL .../install.sh | sh -s -- v1.2.0      # a given version
#
# Environment: ARTCODED_VERSION (latest | edge | vX.Y.Z), ARTCODED_INSTALL_DIR (where to put it),
# ARTCODED_REPO (owner/repo of the releases), ARTCODED_BASE_URL (a mirror of the releases).
set -eu

REPO="${ARTCODED_REPO:-openartcoded/artcoded-cli}"
VERSION="${1:-${ARTCODED_VERSION:-latest}}"
NAME=artcoded

fail() { printf 'artcoded install: %s\n' "$*" >&2; exit 1; }

case "$(uname -s)" in
  Linux) os=linux ;;
  *) fail "only linux is built (x86_64, arm64); elsewhere: go install github.com/$REPO@latest, or the docker image" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64 | armv8*) arch=arm64 ;;
  *) fail "unsupported architecture $(uname -m) (x86_64 and arm64 are built)" ;;
esac
asset="$NAME-$os-$arch"

releases="${ARTCODED_BASE_URL:-https://github.com/$REPO/releases}"
if [ "$VERSION" = latest ]; then
  base="$releases/latest/download"
else
  base="$releases/download/$VERSION"
fi

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO "$2" "$1"; }
else
  fail "curl or wget is needed"
fi

# where: ARTCODED_INSTALL_DIR, where it already is (update), /usr/local/bin, else ~/.local/bin
if [ -n "${ARTCODED_INSTALL_DIR:-}" ]; then
  dir="$ARTCODED_INSTALL_DIR"
elif existing="$(command -v "$NAME" 2>/dev/null)" && [ -n "$existing" ]; then
  dir="$(dirname "$existing")"
elif [ -w /usr/local/bin ]; then
  dir=/usr/local/bin
else
  dir="$HOME/.local/bin"
fi
mkdir -p "$dir" 2>/dev/null || true

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

printf 'downloading %s (%s)...\n' "$asset" "$VERSION"
fetch "$base/$asset" "$tmp/$NAME" || fail "cannot download $base/$asset"

# checksum, when a sha256 tool is there
if fetch "$base/checksums.txt" "$tmp/checksums.txt" 2>/dev/null; then
  expected="$(grep " $asset\$" "$tmp/checksums.txt" | cut -d' ' -f1)"
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$tmp/$NAME" | cut -d' ' -f1)"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$tmp/$NAME" | cut -d' ' -f1)"
  else
    actual="$expected"
  fi
  if [ -n "$expected" ] && [ "$expected" != "$actual" ]; then
    fail "checksum mismatch for $asset"
  fi
fi
chmod +x "$tmp/$NAME"

if [ -w "$dir" ]; then
  mv "$tmp/$NAME" "$dir/$NAME"
elif command -v sudo >/dev/null 2>&1; then
  printf 'installing in %s (sudo)\n' "$dir"
  sudo mv "$tmp/$NAME" "$dir/$NAME"
else
  fail "$dir is not writable: set ARTCODED_INSTALL_DIR"
fi

printf 'installed %s: %s\n' "$dir/$NAME" "$("$dir/$NAME" version 2>/dev/null || echo "$VERSION")"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) printf 'add it to your PATH: export PATH="%s:$PATH"\n' "$dir" ;;
esac
