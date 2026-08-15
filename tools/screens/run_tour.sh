#!/usr/bin/env bash
# tools/screens/run_tour.sh — launch Xvfb + software GL, run the Godot
# screenshot tour, capture PNGs + webp thumbs to docs/screens/<sha>/.
#
# Called by `make screenshots`. Fully offline on the VPS (no GPU/display/sudo).
# Optionally forward --api/--ws to enable the server-gated live stages.
set -euo pipefail

REPO="$(git rev-parse --show-toplevel)"
PREFIX="${XVFB_PREFIX:-$HOME/.local/xvfb-prefix}"
# godot4 lives in ~/.local/bin (not always on a non-login PATH).
export PATH="$HOME/.local/bin:$HOME/go/bin:${PATH:-}"
GODOT="${GODOT:-godot4}"
SCREENS_DIR="$REPO/docs/screens"

# 1. ensure the user-space Xvfb prefix + shim exist
"$REPO/tools/screens/setup_xvfb.sh" "$PREFIX"

# 2. compute short sha
SHA="$(git -C "$REPO" rev-parse --short HEAD)"
OUT="$SCREENS_DIR/$SHA"
mkdir -p "$OUT/thumb"

# 3. pick a free display number
DISPLAY_NUM=99
while [ -e "/tmp/.X${DISPLAY_NUM}-lock" ]; do
	DISPLAY_NUM=$((DISPLAY_NUM + 1))
done
export XKB_PREFIX="$PREFIX"
export LD_LIBRARY_PATH="$PREFIX/usr/lib/x86_64-linux-gnu:${LD_LIBRARY_PATH:-}"
export LD_PRELOAD="$PREFIX/libxkbshim.so"
export LIBGL_ALWAYS_SOFTWARE=1
export GALLIUM_DRIVER=llvmpipe
export LIBGL_DRIVERS_PATH="$PREFIX/usr/lib/x86_64-linux-gnu/dri"
# glvnd (libEGL/libGLX) looks in /usr/share/glvnd by default; point it at the
# prefix's mesa ICD so the swrast EGL/GLX vendor loads from user-space.
export __EGL_VENDOR_LIBRARY_FILENAMES="$PREFIX/usr/share/glvnd/egl_vendor.d/50_mesa.json"
export __GLX_VENDOR_LIBRARY_FILENAMES="$PREFIX/usr/share/glvnd/egl_vendor.d/50_mesa.json"

echo "[screens] starting Xvfb :$DISPLAY_NUM ($SHA)"
XVFB_PID=""
cleanup() {
	if [ -n "$XVFB_PID" ] && kill -0 "$XVFB_PID" 2>/dev/null; then
		kill "$XVFB_PID" 2>/dev/null || true
		wait "$XVFB_PID" 2>/dev/null || true
	fi
	rm -f "/tmp/.X${DISPLAY_NUM}-lock" "/tmp/.X11-unix/X${DISPLAY_NUM}"
}
trap cleanup EXIT

"$PREFIX/usr/bin/Xvfb" ":$DISPLAY_NUM" -screen 0 1920x1080x24 \
	+extension GLX +extension RANDR -nolisten tcp &
XVFB_PID=$!
sleep 2
if ! kill -0 "$XVFB_PID" 2>/dev/null; then
	echo "[screens] ERROR: Xvfb failed to start" >&2
	exit 1
fi

export DISPLAY=":$DISPLAY_NUM"

# 4. run the tour (NOT headless — we need rendering; the SceneTree script
#    drives the root window and captures its viewport).
echo "[screens] running Godot screenshot tour → $OUT"
cd "$REPO/client"
timeout 240 "$GODOT" --path . --rendering-method gl_compatibility \
	--resolution 1920x1080 --script res://tools/ScreenshotTour.gd \
	-- --screenshot-tour --out "$OUT" --sha "$SHA" "$@"
TOUR_RC=$?
echo "[screens] tour exit=$TOUR_RC"

if [ "$TOUR_RC" -ne 0 ]; then
	echo "[screens] ERROR: tour failed (rc=$TOUR_RC)" >&2
	exit "$TOUR_RC"
fi

# 5. publish: copy this sha's set to the gallery dir the admin server serves
#    (bind-mounted into the admin container; see deploy/docker-compose.yml).
PUBLISH_DIR="${AETHERIA_SCREEN_GALLERY:-/home/agency/aetheria/screens}"
if mkdir -p "$PUBLISH_DIR/$SHA" 2>/dev/null; then
	cp -a "$OUT/." "$PUBLISH_DIR/$SHA/"
	# refresh 'latest' symlink
	ln -sfn "$SHA" "$PUBLISH_DIR/latest" 2>/dev/null || true
	echo "[screens] published → $PUBLISH_DIR/$SHA"
else
	echo "[screens] WARNING: could not publish to $PUBLISH_DIR (perms?) — gallery will read docs/screens directly" >&2
fi

echo "[screens] DONE sha=$SHA captured=$(ls "$OUT"/*.png 2>/dev/null | wc -l) out=$OUT"
