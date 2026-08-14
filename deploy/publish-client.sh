#!/usr/bin/env bash
# publish-client.sh — package the exported Godot builds into versioned zips and
# stage them under $AETHERIA_CLIENT_BUILDS (default ~/aetheria/client-builds),
# which the portal serves at https://aetheria.apps.deployden.tech/download/.
set -euo pipefail
cd "$(dirname "$0")/.."

BUILD_DIR=client/build
OUT_DIR="${AETHERIA_CLIENT_BUILDS:-$HOME/aetheria/client-builds}"
mkdir -p "$OUT_DIR"

[ -f "$BUILD_DIR/aetheria-windows.exe" ] || { echo "publish-client: missing $BUILD_DIR/aetheria-windows.exe (run make export-client first)"; exit 1; }

STAMP="$(date +%Y%m%d-%H%M%S)"

# Windows bundle: exe + pck + config.json next to the exe.
rm -rf /tmp/aetheria-pub && mkdir -p /tmp/aetheria-pub/win /tmp/aetheria-pub/linux
cp "$BUILD_DIR/aetheria-windows.exe" "$BUILD_DIR/aetheria-windows.pck" "$BUILD_DIR/config.json" /tmp/aetheria-pub/win/
cp "$BUILD_DIR/aetheria-linux.x86_64" "$BUILD_DIR/aetheria-linux.pck" "$BUILD_DIR/config.json" /tmp/aetheria-pub/linux/
python3 - "$OUT_DIR" "$STAMP" <<'PY'
import shutil, sys, zipfile
out, stamp = sys.argv[1], sys.argv[2]
def pack(src_dir, out_name):
    with zipfile.ZipFile(f"{out}/{out_name}", "w", zipfile.ZIP_DEFLATED) as z:
        for name in sorted(__import__("os").listdir(src_dir)):
            z.write(f"{src_dir}/{name}", name)
    print("packed", out_name)
pack("/tmp/aetheria-pub/win", f"aetheria-windows-{stamp}.zip")
pack("/tmp/aetheria-pub/linux", f"aetheria-linux-{stamp}.zip")
PY
rm -rf /tmp/aetheria-pub

# A stable "latest" copy so the download URL never changes between builds.
cp "$OUT_DIR/aetheria-windows-$STAMP.zip" "$OUT_DIR/aetheria-windows-latest.zip"
cp "$OUT_DIR/aetheria-linux-$STAMP.zip" "$OUT_DIR/aetheria-linux-latest.zip"

ls -la "$OUT_DIR"
echo "published client builds (stamp=$STAMP)"
