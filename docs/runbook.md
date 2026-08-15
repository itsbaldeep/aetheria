# Aetheria — Ops Runbook

Living ops doc. Add a section per operational procedure. (Full backup/restore
drill lands in M10; this starts with the M5.5 screenshot pipeline.)

## Screenshot pipeline (M5.5 §1)

`make screenshots` regenerates the full UI screenshot tour on the VPS and
publishes a review gallery. It is the dev loop's "eyes" — run it after every
client-facing work block and before requesting human review.

### What it does
1. Ensures a user-space Xvfb + Mesa (llvmpipe) prefix at `~/.local/xvfb-prefix`
   (built idempotently by `tools/screens/setup_xvfb.sh` — no sudo; downloads
   `.deb`s and extracts them, plus an `LD_PRELOAD` shim that redirects the X
   server's hardcoded `/usr/bin/xkbcomp` into the prefix).
2. Starts `Xvfb :99` at 1920×1080×24 with `LIBGL_ALWAYS_SOFTWARE=1
   GALLIUM_DRIVER=llvmpipe`.
3. Runs the real Godot client (NOT headless) with
   `--rendering-method gl_compatibility --script res://tools/ScreenshotTour.gd`.
   The tour drives every reachable screen / UI state and captures the root
   viewport via `get_viewport().get_texture().get_image().save_png()`.
4. Writes `docs/screens/<git-short-sha>/NN_name.png` (+ `thumb/*.webp` +
   `index.txt`).
5. Publishes to `/home/agency/aetheria/screens/<sha>` (bind-mounted read-only
   into the `aetheria-adminserver` container at `/srv/screens`).

### Reviewing
- Gallery: `https://admin.aetheria.apps.deployden.tech/screens?t=<AETHERIA_SCREENS_TOKEN>`
  (token lives in `/home/agency/aetheria/env`; no token → 404, gallery hidden).
- A sha page lists thumbnails; click for full PNG. Add `&compare=<other-sha>`
  for a side-by-side against a previous run.
- The human writes verdicts in `FEEDBACK.md` as `screens <sha>: …`; the next
  work block folds them in. Silence ≠ approval.

### Server-gated stages
Stages `17_combat_live_snapshot` and `18_quest_live_data` need a live server +
the `screenshot_bot` account with GM teleport. Without `--api`/`--ws` they
skip (logged in `index.txt`). To enable:
```
make screenshots ARGS="--api http://127.0.0.1:3016 --ws ws://127.0.0.1:3015/ws"
```
(The `screenshot_bot` account + GM token wiring is an M5.5 follow-up — see
`HUMAN_TODO.md`.)

### Troubleshooting
- **Xvfb fails to start / "xkbcomp not found"**: re-run
  `tools/screens/setup_xvfb.sh ~/.local/xvfb-prefix`; the `LD_PRELOAD` shim
  must exist and `XKB_PREFIX` must point at the prefix (set by `run_tour.sh`).
- **Godot crashes in GL init**: confirm `LIBGL_DRIVERS_PATH` points at the
  prefix's `dri/` dir and that `mesa-libgallium` was extracted (it ships
  `libgallium-*.so` which `libEGL_mesa`/`libGLX_mesa` depend on).
- **Gallery 404 with token**: the admin container didn't pick up
  `AETHERIA_SCREENS_TOKEN` — `docker compose up -d --build adminserver` after
  editing `/home/agency/aetheria/env`.
- **First run is slow**: llvmpipe software rasterizer; the 16-stage tour takes
  ~2–3 min. Subsequent runs reuse the prefix.
