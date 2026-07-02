#!/bin/sh
set -eu

REPO="nicoalama/sekaiserver"
VERSION="${SEKAISER_VERSION:-latest}"
INSTALL_DIR="${SEKAISER_INSTALL_DIR:-/usr/local/bin}"
CONFIG_DIR="${HOME}/.sekai-server"
CONFIG_FILE="${CONFIG_DIR}/config.json"
BIN_NAME="sekai-server"

#  Parse flags 
URL_PROVIDER=""
API_KEY=""
LOCAL_PORT=""
LOCAL_HOST=""
RELAY=""

while [ $# -gt 0 ]; do
  case "$1" in
    --url-provider=*) URL_PROVIDER="${1#*=}" ;;
    --api-key=*)      API_KEY="${1#*=}" ;;
    --local-port=*)   LOCAL_PORT="${1#*=}" ;;
    --local-host=*)   LOCAL_HOST="${1#*=}" ;;
    --relay=*)        RELAY="${1#*=}" ;;
    --install-dir=*)  INSTALL_DIR="${1#*=}" ;;
    *)
      echo "Unknown option: $1"
      echo "Usage: install.sh [--url-provider=URL] [--api-key=KEY] [--local-port=PORT] [--local-host=HOST] [--relay=URL]"
      exit 1
      ;;
  esac
  shift
done

#  Detect OS and architecture 
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$OS" in
  linux)   OS="linux" ;;
  darwin)  OS="darwin" ;;
  mingw*|msys*|cygwin*) OS="windows" ;;
  *)
    echo "Unsupported OS: $OS"
    exit 1
    ;;
esac

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

# Windows arm64 not supported
if [ "$OS" = "windows" ] && [ "$ARCH" = "arm64" ]; then
  echo "Windows arm64 is not supported yet."
  exit 1
fi

#  Resolve download URL 
EXT=""
[ "$OS" = "windows" ] && EXT=".exe"

if [ "$VERSION" = "latest" ]; then
  DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/${BIN_NAME}-${OS}-${ARCH}${EXT}"
else
  DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${BIN_NAME}-${OS}-${ARCH}${EXT}"
fi

#  Download binary 
echo "Downloading sekai-server ${VERSION} for ${OS}/${ARCH}..."

DOWNLOAD_FILE="/tmp/${BIN_NAME}${EXT}"
CHECKSUM_FILE="/tmp/${BIN_NAME}-checksums.txt"

if [ "$VERSION" = "latest" ]; then
  CHECKSUM_URL="https://github.com/${REPO}/releases/latest/download/checksums.txt"
else
  CHECKSUM_URL="https://github.com/${REPO}/releases/download/${VERSION}/checksums.txt"
fi

# Download binary
if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$DOWNLOAD_URL" -o "$DOWNLOAD_FILE"
  curl -fsSL "$CHECKSUM_URL" -o "$CHECKSUM_FILE"
elif command -v wget >/dev/null 2>&1; then
  wget -q "$DOWNLOAD_URL" -O "$DOWNLOAD_FILE"
  wget -q "$CHECKSUM_URL" -O "$CHECKSUM_FILE"
else
  echo "Error: need curl or wget to download"
  exit 1
fi

#  Verify SHA256 
BINARY_FILENAME="${BIN_NAME}-${OS}-${ARCH}${EXT}"
EXPECTED_HASH="$(grep "${BINARY_FILENAME}" "$CHECKSUM_FILE" | awk '{print $1}')"

if [ -z "$EXPECTED_HASH" ]; then
  echo "Warning: no se pudo verificar checksum (${BINARY_FILENAME} no encontrado en checksums.txt)"
elif command -v sha256sum >/dev/null 2>&1; then
  COMPUTED_HASH="$(sha256sum "$DOWNLOAD_FILE" | awk '{print $1}')"
  if [ "$EXPECTED_HASH" != "$COMPUTED_HASH" ]; then
    echo "Error: checksum no coincide!"
    echo "  Esperado: ${EXPECTED_HASH}"
    echo "  Obtenido: ${COMPUTED_HASH}"
    rm -f "$DOWNLOAD_FILE" "$CHECKSUM_FILE"
    exit 1
  fi
  echo "Checksum SHA256 verificado correctamente."
elif command -v shasum >/dev/null 2>&1; then
  COMPUTED_HASH="$(shasum -a 256 "$DOWNLOAD_FILE" | awk '{print $1}')"
  if [ "$EXPECTED_HASH" != "$COMPUTED_HASH" ]; then
    echo "Error: checksum no coincide!"
    echo "  Esperado: ${EXPECTED_HASH}"
    echo "  Obtenido: ${COMPUTED_HASH}"
    rm -f "$DOWNLOAD_FILE" "$CHECKSUM_FILE"
    exit 1
  fi
  echo "Checksum SHA256 verificado correctamente."
else
  echo "Warning: no se pudo verificar checksum (falta sha256sum o shasum)"
fi

rm -f "$CHECKSUM_FILE"
chmod +x "$DOWNLOAD_FILE"

#  Move to install directory 
if [ "$OS" = "windows" ]; then
  # On Windows, just put it next to the script or in current dir
  mv "/tmp/${BIN_NAME}${EXT}" "./${BIN_NAME}${EXT}"
  INSTALLED_PATH="$(pwd)/${BIN_NAME}${EXT}"
else
  if [ -d "$INSTALL_DIR" ] && [ -w "$INSTALL_DIR" ]; then
    mv "/tmp/${BIN_NAME}${EXT}" "${INSTALL_DIR}/${BIN_NAME}"
    INSTALLED_PATH="${INSTALL_DIR}/${BIN_NAME}"
  else
    # Fallback: install to HOME/.local/bin
    mkdir -p "${HOME}/.local/bin"
    mv "/tmp/${BIN_NAME}${EXT}" "${HOME}/.local/bin/${BIN_NAME}"
    INSTALLED_PATH="${HOME}/.local/bin/${BIN_NAME}"
    echo "Warning: could not write to ${INSTALL_DIR}, installed to ${INSTALLED_PATH}"
    echo "Add ${HOME}/.local/bin to your PATH if not already."
  fi
fi

echo "Installed to ${INSTALLED_PATH}"

#  Save config 
if [ -n "$URL_PROVIDER" ] || [ -n "$API_KEY" ]; then
  mkdir -p "$CONFIG_DIR"

  # Read existing config if any
  OLD_CONFIG=""
  if [ -f "$CONFIG_FILE" ]; then
    OLD_CONFIG=$(cat "$CONFIG_FILE")
  fi

  # Build JSON config (merge with existing)
  if command -v jq >/dev/null 2>&1; then
    # Use jq for robust JSON manipulation
    RELAY="${RELAY:-$(echo "$OLD_CONFIG" | jq -r '.relay // empty')}"
    RELAY="${RELAY:-https://sekailink.dev}"
    LOCAL_HOST="${LOCAL_HOST:-$(echo "$OLD_CONFIG" | jq -r '.local_host // empty')}"
    LOCAL_HOST="${LOCAL_HOST:-localhost}"
    LOCAL_PORT="${LOCAL_PORT:-$(echo "$OLD_CONFIG" | jq -r '.local_port // empty')}"
    LOCAL_PORT="${LOCAL_PORT:-3000}"

    NEW_CONFIG=$(echo "{}" | jq \
      --arg relay "$RELAY" \
      --arg url_provider "$URL_PROVIDER" \
      --arg api_key "$API_KEY" \
      --arg local_host "$LOCAL_HOST" \
      --argjson local_port "$LOCAL_PORT" \
      '.relay = $relay | .url_provider = $url_provider | .api_key = $api_key | .local_host = $local_host | .local_port = $local_port'
    )
    echo "$NEW_CONFIG" > "$CONFIG_FILE"
  else
    # Fallback: simple shell JSON construction
    RELAY="${RELAY:-$(echo "$OLD_CONFIG" | sed -n 's/.*"relay"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')}"
    RELAY="${RELAY:-https://sekailink.dev}"
    LOCAL_HOST="${LOCAL_HOST:-$(echo "$OLD_CONFIG" | sed -n 's/.*"local_host"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')}"
    LOCAL_HOST="${LOCAL_HOST:-localhost}"
    LOCAL_PORT="${LOCAL_PORT:-$(echo "$OLD_CONFIG" | sed -n 's/.*"local_port"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p')}"
    LOCAL_PORT="${LOCAL_PORT:-3000"}

    cat > "$CONFIG_FILE" <<- ENDOFFILE
	{
	  "relay": "${RELAY}",
	  "url_provider": "${URL_PROVIDER}",
	  "api_key": "${API_KEY}",
	  "local_host": "${LOCAL_HOST}",
	  "local_port": ${LOCAL_PORT}
	}
ENDOFFILE
  fi

  echo "Config saved to ${CONFIG_FILE}"
fi

#  Start sekai-server 
if [ -f "$CONFIG_FILE" ] && [ -s "$CONFIG_FILE" ]; then
  echo ""
  echo "Starting sekai-server..."
  echo ""

  # Run in background
  if [ "$OS" = "windows" ]; then
    "${INSTALLED_PATH}" &
  else
    nohup "${INSTALLED_PATH}" > /dev/null 2>&1 &
  fi

  PID=$!
  echo "sekai-server started (PID: ${PID})"
  echo ""
  echo "To check logs, run: ${INSTALLED_PATH} 2>&1"
  echo "To stop, run: kill ${PID}"
else
  echo ""
  echo "No config found. Run with --url-provider and --api-key flags to configure."
  echo "Example:"
  echo "  curl -sL https://raw.githubusercontent.com/${REPO}/main/install.sh | sh -s -- \\"
  echo "    --url-provider=https://sekailink.dev/YOUR_CODE \\"
  echo "    --api-key=sk_YOUR_API_KEY"
  echo ""
  echo "Or run the binary directly:"
  echo "  ${INSTALLED_PATH} --url-provider=... --api-key=..."
fi
