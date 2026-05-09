#!/usr/bin/env bash
set -euo pipefail
REPO="rohitsingh4334/nginx-ingress-to-istio"
BINARY="ingress-nginx-migration"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
VERSION=""
while [[ $# -gt 0 ]]; do
  case "$1" in --version) VERSION="$2"; shift 2 ;; *) echo "Unknown flag: $1"; exit 1 ;; esac
done
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$OS" in linux) PLATFORM="linux" ;; darwin) PLATFORM="darwin" ;; *) echo "❌ Unsupported OS: $OS"; exit 1 ;; esac
case "$ARCH" in x86_64|amd64) ARCH="amd64" ;; arm64|aarch64) ARCH="arm64" ;; *) echo "❌ Unsupported arch: $ARCH"; exit 1 ;; esac
if [[ -z "$VERSION" ]]; then
  VERSION="$(curl -sSfL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')"
fi
echo "📦 Installing ${BINARY} ${VERSION} (${PLATFORM}/${ARCH})…"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY}-${PLATFORM}-${ARCH}"
TMP="$(mktemp)"
curl -sSfL "$URL" -o "$TMP"
chmod +x "$TMP"
if [[ -w "$INSTALL_DIR" ]]; then mv "$TMP" "${INSTALL_DIR}/${BINARY}"
else sudo mv "$TMP" "${INSTALL_DIR}/${BINARY}"; fi
echo "✅ Installed: ${INSTALL_DIR}/${BINARY}"
