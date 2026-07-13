#!/bin/bash
#
# Package threev as a Linux AppImage.
#
# This script:
# - Builds a Linux amd64 binary via `wails build -platform linux/amd64`
# - Downloads linuxdeploy and its AppImage-output plugin
#   (linuxdeploy-plugin-appimage), from their continuous releases
# - Packages the app into an AppImage using linuxdeploy
#
# Code signing: The AppImage is unsigned by nature.
#
# Prerequisites: Linux, wails CLI, wget or curl, ImageMagick (`convert`),
# standard build tools
# Invoke from repo root: ./scripts/package-appimage.sh
#

set -euo pipefail

# linuxdeploy and its plugin are themselves shipped as AppImages, which
# normally require FUSE to mount. GitHub Actions' ubuntu-latest runners
# don't reliably have FUSE available, so force the extract-and-run fallback
# unconditionally - it works the same whether or not FUSE is present, so
# there's no downside to always setting it (locally or in CI).
export APPIMAGE_EXTRACT_AND_RUN=1

# Repo root is the parent of scripts/
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${REPO_ROOT}/build"
BIN_DIR="${BUILD_DIR}/bin"
LINUX_BUILD_DIR="${BUILD_DIR}/linux"
WAILS_JSON="${REPO_ROOT}/wails.json"

# Extract version from wails.json
VERSION=$(jq -r '.info.productVersion' "$WAILS_JSON")
if [ -z "$VERSION" ] || [ "$VERSION" = "null" ]; then
  echo "ERROR: Could not read productVersion from $WAILS_JSON"
  exit 1
fi

APPIMAGE_NAME="threev-${VERSION}-linux-amd64.AppImage"
APPIMAGE_PATH="${BIN_DIR}/${APPIMAGE_NAME}"
BINARY_PATH="${BIN_DIR}/threev"

# Paths for linuxdeploy and its AppImage-output plugin. Note: this is
# linuxdeploy-plugin-appimage (a linuxdeploy plugin binary), NOT the
# standalone AppImage/AppImageKit appimagetool - `--output appimage` invokes
# the plugin, which linuxdeploy discovers by looking in its own directory
# (per linuxdeploy's plugin auto-discovery), not a bare appimagetool.
LINUXDEPLOY="${BIN_DIR}/linuxdeploy-x86_64.AppImage"
LINUXDEPLOY_PLUGIN_APPIMAGE="${BIN_DIR}/linuxdeploy-plugin-appimage-x86_64.AppImage"

echo "Building threev (Linux amd64)..."
wails build -platform linux/amd64 -clean

if [ ! -f "$BINARY_PATH" ]; then
  echo "ERROR: Build failed or binary not found at $BINARY_PATH"
  exit 1
fi

# Ensure build/linux directory exists
mkdir -p "$LINUX_BUILD_DIR"

# Download linuxdeploy if not present
if [ ! -f "$LINUXDEPLOY" ]; then
  echo "Downloading linuxdeploy..."
  # linuxdeploy only publishes continuous builds, no versioned releases
  LINUXDEPLOY_URL="https://github.com/linuxdeploy/linuxdeploy/releases/download/continuous/linuxdeploy-x86_64.AppImage"
  if command -v wget &> /dev/null; then
    wget -q "$LINUXDEPLOY_URL" -O "$LINUXDEPLOY"
  elif command -v curl &> /dev/null; then
    curl -sL "$LINUXDEPLOY_URL" -o "$LINUXDEPLOY"
  else
    echo "ERROR: wget or curl not found"
    exit 1
  fi
  chmod +x "$LINUXDEPLOY"
fi

# Download linuxdeploy-plugin-appimage if not present. Must sit in the same
# directory as $LINUXDEPLOY - that's how linuxdeploy's plugin discovery
# finds a `linuxdeploy-plugin-<name>` binary for `--output <name>`.
if [ ! -f "$LINUXDEPLOY_PLUGIN_APPIMAGE" ]; then
  echo "Downloading linuxdeploy-plugin-appimage..."
  # Also continuous-build-only, no versioned releases.
  PLUGIN_URL="https://github.com/linuxdeploy/linuxdeploy-plugin-appimage/releases/download/continuous/linuxdeploy-plugin-appimage-x86_64.AppImage"
  if command -v wget &> /dev/null; then
    wget -q "$PLUGIN_URL" -O "$LINUXDEPLOY_PLUGIN_APPIMAGE"
  elif command -v curl &> /dev/null; then
    curl -sL "$PLUGIN_URL" -o "$LINUXDEPLOY_PLUGIN_APPIMAGE"
  else
    echo "ERROR: wget or curl not found"
    exit 1
  fi
  chmod +x "$LINUXDEPLOY_PLUGIN_APPIMAGE"
fi

echo "Creating AppImage..."

# Create a temporary appdir structure for linuxdeploy
APPDIR="${BIN_DIR}/.appimage-staging"
rm -rf "$APPDIR"
mkdir -p "$APPDIR/usr/bin"

# Copy the binary
cp "$BINARY_PATH" "$APPDIR/usr/bin/threev"
chmod +x "$APPDIR/usr/bin/threev"

# Prepare desktop file (created at build/linux/threev.desktop)
DESKTOP_FILE="${LINUX_BUILD_DIR}/threev.desktop"
if [ ! -f "$DESKTOP_FILE" ]; then
  echo "ERROR: $DESKTOP_FILE not found. Please ensure build/linux/threev.desktop exists."
  exit 1
fi

# Prepare icon. build/appicon.png is 1024x1024, but linuxdeploy only
# accepts a fixed set of square icon resolutions (8x8 up to 512x512) and
# rejects anything else outright ("invalid x resolution") - resize down to
# 512x512 (the largest accepted size) before handing it to linuxdeploy.
ICON_SOURCE_ORIGINAL="${BUILD_DIR}/appicon.png"
if [ ! -f "$ICON_SOURCE_ORIGINAL" ]; then
  echo "ERROR: Icon not found at $ICON_SOURCE_ORIGINAL"
  exit 1
fi
if ! command -v convert &> /dev/null; then
  echo "ERROR: ImageMagick's 'convert' command not found - required to resize" \
       "build/appicon.png (1024x1024) down to a resolution linuxdeploy accepts" \
       "(max 512x512). Install ImageMagick (e.g. 'apt-get install -y imagemagick')."
  exit 1
fi
ICON_SOURCE="${BIN_DIR}/threev-icon-512.png"
convert "$ICON_SOURCE_ORIGINAL" -resize 512x512 "$ICON_SOURCE"

mkdir -p "$APPDIR/usr/share/pixmaps"
cp "$ICON_SOURCE" "$APPDIR/usr/share/pixmaps/threev.png"

# Use linuxdeploy to create the AppImage.
# -d: desktop file, -i: icon file, -e: executable, --appdir: AppDir to
# populate, --output appimage: invoke the linuxdeploy-plugin-appimage binary
# downloaded above (discovered by linuxdeploy via its own directory - see
# LINUXDEPLOY_PLUGIN_APPIMAGE above; there is no "-a" flag in linuxdeploy's
# actual CLI, --appdir is the only way to pass the AppDir path).
export VERSION="$VERSION"
export ARCH="x86_64"
export PATH="${BIN_DIR}:${PATH}"

# linuxdeploy-plugin-appimage writes the resulting .AppImage to the current
# working directory, not the AppDir - cd into $BIN_DIR first so the `find`
# below (and the final rename) look in the right place.
cd "$BIN_DIR"

"$LINUXDEPLOY" \
  --appdir "$APPDIR" \
  -d "$DESKTOP_FILE" \
  -i "$ICON_SOURCE" \
  -e "$BINARY_PATH" \
  --output appimage

# linuxdeploy creates <AppName>-<arch>.AppImage in $BIN_DIR (see cd above);
# find and rename it to the predictable versioned name. Excludes the
# downloaded linuxdeploy/plugin binaries by name - both also live in
# $BIN_DIR and, on a first run, are freshly-downloaded (so also "-newer"
# than $BINARY_PATH), which would otherwise make `find` ambiguous.
GENERATED_APPIMAGE=$(find "$BIN_DIR" -maxdepth 1 -name "*.AppImage" -newer "$BINARY_PATH" \
  ! -name "linuxdeploy-x86_64.AppImage" \
  ! -name "linuxdeploy-plugin-appimage-x86_64.AppImage" | head -1)

if [ -z "$GENERATED_APPIMAGE" ] || [ ! -f "$GENERATED_APPIMAGE" ]; then
  echo "ERROR: Failed to create AppImage"
  rm -rf "$APPDIR"
  exit 1
fi

# Rename to predictable name
mv "$GENERATED_APPIMAGE" "$APPIMAGE_PATH"

# Verify the AppImage
if [ ! -f "$APPIMAGE_PATH" ]; then
  echo "ERROR: Failed to create $APPIMAGE_PATH"
  rm -rf "$APPDIR"
  exit 1
fi

# Check it's a valid AppImage (should be an ELF executable)
if ! file "$APPIMAGE_PATH" | grep -q "ELF\|AppImage"; then
  echo "ERROR: Created file is not a valid AppImage"
  rm -rf "$APPDIR" "$APPIMAGE_PATH"
  exit 1
fi

# Clean up staging directory
rm -rf "$APPDIR"

echo "Successfully created: $APPIMAGE_PATH"
echo "Version: $VERSION"
