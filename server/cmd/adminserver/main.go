// adminserver — GM web dashboard (server-rendered + HTMX), TOTP 2FA, audit
// log, health/economy panels. See docs/BRIEF.md §3 and §8. M8 scope.
package main

import (
	"net/http"

	"github.com/itsbaldeep/aetheria/server/internal/platform"
)

func main() {
	s := &platform.Service{Name: "adminserver"}
	addr := "127.0.0.1:" + platform.Env("AETHERIA_ADMIN_PORT", "3017")

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.Healthz())
	// M8: dashboard routes, auth + TOTP, audit log.

	s.Log("info", "adminserver listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		s.Log("fatal", "server exited", "error", err)
	}
}
