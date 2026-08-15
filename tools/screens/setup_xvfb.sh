#!/usr/bin/env bash
# tools/screens/setup_xvfb.sh — build a user-space Xvfb prefix (no sudo).
#
# The VPS has no GPU/display and xvfb is not installed system-wide; this agent
# has no root. We download the .deb files (apt-get download needs no root) and
# extract them into a prefix, then compile a tiny LD_PRELOAD shim that
# redirects the X server's hardcoded /usr/bin/xkbcomp to the prefix. Result:
# a working Xvfb + Mesa software GL stack entirely under $HOME.
#
# Idempotent: if the prefix + shim already exist, exits 0 fast.
# Usage: ./tools/screens/setup_xvfb.sh [PREFIX]
set -euo pipefail

PREFIX="${1:-$HOME/.local/xvfb-prefix}"
SHIM="$PREFIX/libxkbshim.so"

if [ -x "$PREFIX/usr/bin/Xvfb" ] && [ -f "$SHIM" ]; then
	echo "[setup-xvfb] prefix ready at $PREFIX"
	exit 0
fi

mkdir -p "$PREFIX" /tmp/opencode/xvfbdl
cd /tmp/opencode/xvfbdl

PKGS=(
	xvfb xserver-common
	libxfont2 libpixman-1-0 libfontenc1
	libgl1 libglx-mesa0 libglx0 libglvnd0
	libxkbfile1 x11-xkb-utils xkb-data
	libgl1-mesa-dri mesa-libgallium
	libxcursor1 libfontconfig1 libxinerama1 libxi6 libxrandr2
	libxrender1 libxss1 libxxf86vm1
	libwayland-client0 libxkbcommon0 libegl1 libgles2
	libxfixes3 libwayland-cursor0 libxkbcommon-x11-0 libdrm2 libgbm1
	libwayland-egl1 libxcomposite1 libxdamage1 libxv1
	libx11-xcb1 libxcb1 libxcb-randr0 libxcb-xfixes0 libxcb-shm0
	libxcb-dri3-0 libxcb-present0 libxcb-sync1 libxcb-glx0 libxcb-dri2-0
	libxshmfence1 libdrm-intel1 libpciaccess0 libelf1t64
)
echo "[setup-xvfb] downloading packages…"
for p in "${PKGS[@]}"; do
	apt-get download "$p" >/dev/null 2>&1 || true
done
echo "[setup-xvfb] extracting into $PREFIX…"
for d in *.deb; do
	[ -e "$d" ] || continue
	dpkg-deb -x "$d" "$PREFIX"
done

# xkbcomp shim source
cat > "$PREFIX/xkbshim.c" <<'SRC'
#define _GNU_SOURCE
#include <dlfcn.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <stdio.h>
#include <errno.h>

static int (*real_execve)(const char *, char *const[], char *const[]) = NULL;
static void init_real(void) { if (!real_execve) real_execve = dlsym(RTLD_NEXT, "execve"); }

static char *redirect(const char *path) {
	const char *pfx = getenv("XKB_PREFIX");
	if (!pfx || !path || strncmp(path, "/usr/bin/", 9) != 0) return NULL;
	const char *rest = path + 9;
	if (strcmp(rest, "xkbcomp") != 0) return NULL;
	char *buf = malloc(strlen(pfx) + 32 + strlen(rest));
	if (!buf) return NULL;
	sprintf(buf, "%s/usr/bin/%s", pfx, rest);
	if (access(buf, X_OK) != 0) { free(buf); return NULL; }
	return buf;
}

int execve(const char *path, char *const argv[], char *const envp[]) {
	init_real();
	char *alt = redirect(path);
	if (alt) { int rc = real_execve(alt, argv, envp); int s = errno; free(alt); errno = s; return rc; }
	return real_execve(path, argv, envp);
}
SRC

echo "[setup-xvfb] compiling shim…"
gcc -shared -fPIC -O2 -o "$SHIM" "$PREFIX/xkbshim.c"

# Sanity: print xkb data dir presence
if [ -d "$PREFIX/usr/share/X11/xkb" ]; then
	echo "[setup-xvfb] xkb data present"
else
	echo "[setup-xvfb] WARNING: xkb data missing — keyboard init may fail" >&2
fi

echo "[setup-xvfb] DONE prefix=$PREFIX shim=$SHIM"
