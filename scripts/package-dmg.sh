#!/bin/bash
#
# Package threev as a macOS .dmg installer.
#
# This script:
# - Builds a universal (arm64+amd64) binary via `wails build -platform darwin/universal`
# - Packages it into a .dmg disk image using macOS's built-in hdiutil
# - Creates a standard drag-to-Applications layout
#
# Code signing: The binary is self-signed with ad-hoc signature (Wails' default).
# No Developer ID or notarization is performed.
#
# Prerequisites: macOS, wails CLI, hdiutil, jq
# Invoke from repo root: ./scripts/package-dmg.sh
#

set -euo pipefail

# Repo root is the parent of scripts/
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${REPO_ROOT}/build"
BIN_DIR="${BUILD_DIR}/bin"
WAILS_JSON="${REPO_ROOT}/wails.json"

# Extract version from wails.json
VERSION=$(jq -r '.info.productVersion' "$WAILS_JSON")
if [ -z "$VERSION" ] || [ "$VERSION" = "null" ]; then
  echo "ERROR: Could not read productVersion from $WAILS_JSON"
  exit 1
fi

DMG_NAME="threev-${VERSION}-macos-universal.dmg"
DMG_PATH="${BIN_DIR}/${DMG_NAME}"
STAGING_DIR="${BIN_DIR}/.dmg-staging-${VERSION}"
APP_PATH="${BIN_DIR}/threev.app"

echo "Building threev (macOS universal)..."
wails build -platform darwin/universal -clean

if [ ! -d "$APP_PATH" ]; then
  echo "ERROR: Build failed or threev.app not found at $APP_PATH"
  exit 1
fi

echo "Creating .dmg installer..."

# Clean up any previous staging or dmg
rm -rf "$STAGING_DIR" "$DMG_PATH"
mkdir -p "$STAGING_DIR"

# Copy the app and create Applications symlink
cp -R "$APP_PATH" "$STAGING_DIR/"
ln -s /Applications "$STAGING_DIR/Applications"

# Create the dmg
# - Suppress dialogs with -quiet
# - Use UDBZ compression for smaller size
hdiutil create -quiet \
  -volname "threev" \
  -srcfolder "$STAGING_DIR" \
  -ov \
  -format UDBZ \
  "$DMG_PATH"

if [ ! -f "$DMG_PATH" ]; then
  echo "ERROR: Failed to create $DMG_PATH"
  rm -rf "$STAGING_DIR"
  exit 1
fi

# Verify the dmg is valid
if ! hdiutil verify "$DMG_PATH" > /dev/null 2>&1; then
  echo "ERROR: Created .dmg failed verification"
  rm -rf "$STAGING_DIR" "$DMG_PATH"
  exit 1
fi

# Clean up staging directory
rm -rf "$STAGING_DIR"

echo "Successfully created: $DMG_PATH"
echo "Version: $VERSION"
