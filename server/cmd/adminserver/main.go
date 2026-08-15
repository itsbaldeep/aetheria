// adminserver — GM web dashboard (server-rendered + HTMX), TOTP 2FA, audit
// log, health/economy panels. See docs/BRIEF.md §3 and §8. M8 scope.
package main

import (
	"net/http"

	"github.com/itsbaldeep/aetheria/server/internal/platform"
	"github.com/itsbaldeep/aetheria/server/internal/screens"
)

func main() {
	s := &platform.Service{Name: "adminserver"}
	addr := "127.0.0.1:" + platform.Env("AETHERIA_ADMIN_PORT", "3017")

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.Healthz())
	// M5.5 §1: screenshot review gallery (gated by AETHERIA_SCREENS_TOKEN
	// until M8 lands full TOTP admin auth).
	screens.Mount(mux, s, platform.Env("AETHERIA_SCREEN_GALLERY", "/srv/screens"),
		platform.Env("AETHERIA_SCREENS_TOKEN", ""))
	// M8: dashboard routes, auth + TOTP, audit log.

	s.Log("info", "adminserver listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		s.Log("fatal", "server exited", "error", err)
	}
}
