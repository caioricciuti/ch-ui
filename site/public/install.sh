#!/bin/sh
set -eu

log() {
  printf '%s\n' "$*"
}

fail() {
  printf 'Error: %s\n' "$*" >&2
  exit 1
}

has_cmd() {
  command -v "$1" >/dev/null 2>&1
}

compute_sha256() {
  if has_cmd sha256sum; then
    sha256sum "$1" | awk '{print $1}'
    return
  fi
  shasum -a 256 "$1" | awk '{print $1}'
}

log "CH-UI Installer"
log "================"
log ""

if ! has_cmd curl; then
  fail "curl is required for installation."
fi

OS="$(uname -s)"
case "$OS" in
  Linux*)  OS="linux" ;;
  Darwin*) OS="darwin" ;;
  *) fail "Unsupported operating system: $OS" ;;
esac

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) fail "Unsupported architecture: $ARCH" ;;
esac

log "Detected platform: ${OS}/${ARCH}"

VERSION_INPUT="${CHUI_VERSION:-}"
if [ -n "$VERSION_INPUT" ]; then
  case "$VERSION_INPUT" in
    v*) VERSION="$VERSION_INPUT" ;;
    *) VERSION="v$VERSION_INPUT" ;;
  esac
  RELEASE_PATH="download/${VERSION}"
  RELEASE_LABEL="$VERSION"
else
  RELEASE_PATH="latest/download"
  RELEASE_LABEL="latest"
fi

ASSET_NAME="ch-ui-${OS}-${ARCH}"
BASE_URL="https://github.com/caioricciuti/ch-ui/releases/${RELEASE_PATH}"
BIN_URL="${BASE_URL}/${ASSET_NAME}"
SUM_URL="${BASE_URL}/checksums.txt"

TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t ch-ui-install)"
trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

BIN_FILE="${TMP_DIR}/ch-ui"
SUM_FILE="${TMP_DIR}/checksums.txt"

log "Downloading CH-UI ${RELEASE_LABEL} binary..."
curl --fail --silent --show-error --location --retry 3 --connect-timeout 15 -o "$BIN_FILE" "$BIN_URL" \
  || fail "Failed to download binary from ${BIN_URL}"

log "Downloading checksums..."
curl --fail --silent --show-error --location --retry 3 --connect-timeout 15 -o "$SUM_FILE" "$SUM_URL" \
  || fail "Failed to download checksums from ${SUM_URL}"

# The binary is never installed unverified. Without a sha256 tool the
# install stops; the release page carries checksums.txt with its Cosign
# signature and certificate for checking by hand.
if ! has_cmd sha256sum && ! has_cmd shasum; then
  fail "sha256sum or shasum is required to verify the download. Install one and run this again."
fi

EXPECTED="$(awk -v f="$ASSET_NAME" '$2==f {print $1}' "$SUM_FILE")"
if [ -z "$EXPECTED" ]; then
  fail "Could not find checksum for ${ASSET_NAME} in checksums.txt"
fi
ACTUAL="$(compute_sha256 "$BIN_FILE")"
if [ "$EXPECTED" != "$ACTUAL" ]; then
  fail "Checksum mismatch for ${ASSET_NAME}. Expected ${EXPECTED}, got ${ACTUAL}."
fi
log "Checksum verified."

INSTALL_DIR="/usr/local/bin"
if [ ! -w "$INSTALL_DIR" ] && [ "$(id -u)" -ne 0 ]; then
  INSTALL_DIR="${HOME}/.local/bin"
  mkdir -p "$INSTALL_DIR" || fail "Failed to create ${INSTALL_DIR}"
fi

INSTALL_PATH="${INSTALL_DIR}/ch-ui"
mkdir -p "$INSTALL_DIR" || fail "Failed to create ${INSTALL_DIR}"

log "Installing to ${INSTALL_PATH}..."
if has_cmd install; then
  install -m 755 "$BIN_FILE" "$INSTALL_PATH" || fail "Failed to install binary to ${INSTALL_PATH}"
else
  TMP_INSTALL="${INSTALL_PATH}.tmp.$$"
  cp "$BIN_FILE" "$TMP_INSTALL" || fail "Failed to stage binary for install"
  chmod 755 "$TMP_INSTALL" || fail "Failed to set executable bit"
  mv "$TMP_INSTALL" "$INSTALL_PATH" || fail "Failed to move binary into place"
fi

if ! "$INSTALL_PATH" version >/dev/null 2>&1; then
  fail "Installed binary failed validation at ${INSTALL_PATH}"
fi

log ""
log "CH-UI installed successfully: ${INSTALL_PATH}"
"$INSTALL_PATH" version || true
log ""

if [ "$INSTALL_DIR" = "${HOME}/.local/bin" ]; then
  case ":${PATH}:" in
    *":${INSTALL_DIR}:"*) ;;
    *)
      log "Your PATH does not include ${INSTALL_DIR}."
      log "Run this now:"
      log "  export PATH=\"${HOME}/.local/bin:\$PATH\""
      log "Add this to ~/.zshrc or ~/.bashrc:"
      log "  export PATH=\"\$HOME/.local/bin:\$PATH\""
      log ""
      ;;
  esac
fi

if has_cmd ch-ui; then
  RUN_CMD="ch-ui"
else
  RUN_CMD="$INSTALL_PATH"
fi

log "Next steps:"
log "  ${RUN_CMD}"
log "  ${RUN_CMD} server --port 3488"
log "  Open: http://localhost:3488"
log "  Can't login guide: https://ch-ui.com/docs/cant-login"
