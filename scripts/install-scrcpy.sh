#!/bin/bash
# Install scrcpy + adb for Batter device remote control
# Downloads prebuilt binaries from GitHub releases

set -e

SCRCPY_VERSION="5.0"
# SHA-256s from upstream's signed SHA256SUMS.txt for this version.
SCRCPY_TARBALL_SHA256="f052ad9eb981879e8c5f066c5ef122b39b6c9853b383d6497219030e6549baed"
SCRCPY_SERVER_SHA256="26cbc9ad0aced6c2282455bef4fb43462605c1f8758c74b4ab1dbf818c229daa"

verify() { # file sha256
    echo "$2  $1" | sha256sum -c --quiet - || {
        echo "Error: checksum mismatch for $1"
        rm -rf "$TMPDIR"
        exit 1
    }
}

echo "=== scrcpy + adb Installation Script ==="
echo "Version: $SCRCPY_VERSION"
echo ""

# Check architecture
ARCH=$(uname -m)
if [ "$ARCH" != "x86_64" ]; then
    echo "Error: Only x86_64 is supported by prebuilt binaries."
    echo "For other architectures, build scrcpy from source:"
    echo "  https://github.com/Genymobile/scrcpy/blob/master/doc/build.md"
    exit 1
fi

# Check if running as root
if [ "$EUID" -eq 0 ]; then
    SUDO=""
else
    SUDO="sudo"
fi

# Install runtime dependencies
echo "=== Step 1: Install runtime dependencies ==="
echo ""
$SUDO apt-get update -qq
$SUDO apt-get install -y -qq ffmpeg libsdl2-2.0-0 libusb-1.0-0 wget > /dev/null
echo "Dependencies installed."
echo ""

# Check for existing installation
EXISTING_VERSION=""
if command -v scrcpy &> /dev/null; then
    EXISTING_VERSION=$(scrcpy --version 2>&1 | head -1 | grep -oP '\d+\.\d+(\.\d+)?' | head -1 || true)
    if [ "$EXISTING_VERSION" = "$SCRCPY_VERSION" ]; then
        echo "scrcpy $SCRCPY_VERSION is already installed."
        echo ""
        echo "Checking scrcpy-server..."
        if [ -f /usr/local/share/scrcpy/scrcpy-server ]; then
            echo "scrcpy-server found at /usr/local/share/scrcpy/scrcpy-server"
            echo "Nothing to do."
            exit 0
        fi
    else
        echo "Existing scrcpy version: $EXISTING_VERSION (will upgrade to $SCRCPY_VERSION)"
    fi
fi

echo "=== Step 2: Download scrcpy $SCRCPY_VERSION ==="
echo ""

TARBALL="scrcpy-linux-x86_64-v${SCRCPY_VERSION}.tar.gz"
DOWNLOAD_URL="https://github.com/Genymobile/scrcpy/releases/download/v${SCRCPY_VERSION}/${TARBALL}"
TMPDIR=$(mktemp -d)

echo "Downloading $TARBALL..."
wget -q --show-progress -O "$TMPDIR/$TARBALL" "$DOWNLOAD_URL" || {
    echo ""
    echo "Failed to download tarball. Trying server-only fallback..."
    echo ""

    # Fallback: download just the server JAR and install adb from apt
    SERVER_URL="https://github.com/Genymobile/scrcpy/releases/download/v${SCRCPY_VERSION}/scrcpy-server-v${SCRCPY_VERSION}"
    wget -q --show-progress -O "$TMPDIR/scrcpy-server" "$SERVER_URL" || {
        echo "Error: Failed to download scrcpy-server."
        rm -rf "$TMPDIR"
        exit 1
    }
    verify "$TMPDIR/scrcpy-server" "$SCRCPY_SERVER_SHA256"

    echo ""
    echo "Installing adb from apt..."
    $SUDO apt-get install -y -qq adb > /dev/null

    echo "Installing scrcpy-server..."
    $SUDO mkdir -p /usr/local/share/scrcpy
    $SUDO cp "$TMPDIR/scrcpy-server" /usr/local/share/scrcpy/scrcpy-server
    $SUDO chmod 644 /usr/local/share/scrcpy/scrcpy-server
    rm -rf "$TMPDIR"

    echo ""
    echo "=== Installation Complete (server-only) ==="
    echo ""
    echo "Installed:"
    echo "  adb:            $(which adb)"
    echo "  scrcpy-server:  /usr/local/share/scrcpy/scrcpy-server"
    echo ""
    echo "Note: Full scrcpy client was not installed (tarball download failed)."
    echo "Batter only needs adb + scrcpy-server."
    echo ""
    echo "Add to your .env:"
    echo "  SCRCPY_SERVER_PATH=/usr/local/share/scrcpy/scrcpy-server"
    echo "  SCRCPY_VERSION=${SCRCPY_VERSION}"
    exit 0
}

echo ""
echo "=== Step 3: Install binaries ==="
echo ""

# Extract
verify "$TMPDIR/$TARBALL" "$SCRCPY_TARBALL_SHA256"
tar xzf "$TMPDIR/$TARBALL" -C "$TMPDIR"
EXTRACTED="$TMPDIR/scrcpy-linux-x86_64-v${SCRCPY_VERSION}"
verify "$EXTRACTED/scrcpy-server" "$SCRCPY_SERVER_SHA256"

# Install binaries
echo "Installing scrcpy to /usr/local/bin/scrcpy..."
$SUDO cp "$EXTRACTED/scrcpy" /usr/local/bin/scrcpy
$SUDO chmod 755 /usr/local/bin/scrcpy

echo "Installing adb to /usr/local/bin/adb..."
$SUDO cp "$EXTRACTED/adb" /usr/local/bin/adb
$SUDO chmod 755 /usr/local/bin/adb

echo "Installing scrcpy-server to /usr/local/share/scrcpy/..."
$SUDO mkdir -p /usr/local/share/scrcpy
$SUDO cp "$EXTRACTED/scrcpy-server" /usr/local/share/scrcpy/scrcpy-server
$SUDO chmod 644 /usr/local/share/scrcpy/scrcpy-server

# Install man page if present
if [ -f "$EXTRACTED/scrcpy.1" ]; then
    $SUDO mkdir -p /usr/local/share/man/man1
    $SUDO cp "$EXTRACTED/scrcpy.1" /usr/local/share/man/man1/
fi

# Cleanup
rm -rf "$TMPDIR"

echo ""
echo "=== Step 4: Configure USB permissions ==="
echo ""

# Add udev rules for Android devices (so adb works without root)
UDEV_RULE="/etc/udev/rules.d/51-android.rules"
if [ ! -f "$UDEV_RULE" ]; then
    echo "Adding udev rules for Android USB debugging..."
    $SUDO tee "$UDEV_RULE" > /dev/null <<'UDEVRULE'
# Android Debug Bridge (adb) - allow USB access for plugdev group
SUBSYSTEM=="usb", ATTR{idVendor}=="0502", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="0b05", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="413c", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="0489", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="04c5", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="091e", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="18d1", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="201e", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="109b", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="0bb4", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="12d1", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="8087", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="24e3", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="2116", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="0482", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="17ef", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="1004", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="22b8", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="0e8d", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="0409", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="2080", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="1d4d", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="0471", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="04da", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="05c6", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="1f53", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="04e8", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="04dd", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="054c", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="0fce", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="2340", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="0930", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="2970", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="1ebf", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="19d2", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="2ae5", MODE="0660", GROUP="plugdev"
SUBSYSTEM=="usb", ATTR{idVendor}=="2a70", MODE="0660", GROUP="plugdev"
UDEVRULE
    $SUDO udevadm control --reload-rules
    $SUDO udevadm trigger
    echo "udev rules installed."

    # Add user to plugdev group
    if ! groups "$USER" | grep -q plugdev; then
        $SUDO usermod -a -G plugdev "$USER"
        echo "Added $USER to plugdev group (re-login required for effect)."
    fi
else
    echo "udev rules already exist at $UDEV_RULE, skipping."
fi

echo ""
echo "=== Step 5: Verify installation ==="
echo ""

echo "scrcpy:         $(scrcpy --version 2>&1 | head -1)"
echo "adb:            $(adb --version 2>&1 | head -1)"
echo "scrcpy-server:  /usr/local/share/scrcpy/scrcpy-server ($(wc -c < /usr/local/share/scrcpy/scrcpy-server) bytes)"

# Try to start adb server and list devices
echo ""
echo "Starting adb server..."
adb start-server 2>&1 || true

DEVICES=$(adb devices -l 2>/dev/null | grep -v "^List" | grep -v "^$" || true)
if [ -n "$DEVICES" ]; then
    echo ""
    echo "Connected devices:"
    echo "$DEVICES"
else
    echo "No devices connected (connect a phone via USB with USB debugging enabled)."
fi

echo ""
echo "=== Installation Complete ==="
echo ""
echo "Add to your .env:"
echo "  SCRCPY_SERVER_PATH=/usr/local/share/scrcpy/scrcpy-server"
echo "  SCRCPY_VERSION=${SCRCPY_VERSION}"
echo ""
echo "To test locally:"
echo "  scrcpy --no-audio"
echo ""
