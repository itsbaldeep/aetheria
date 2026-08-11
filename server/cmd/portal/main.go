// portal — public website: registration, client downloads, rankings,
// voting + donations. See docs/BRIEF.md §3 and §6. M1 (registration) and
// M9 (downloads/voting/donations) land here.
package main

import (
	"net/http"

	"github.com/itsbaldeep/aetheria/server/internal/platform"
)

func main() {
	s := &platform.Service{Name: "portal"}
	addr := "127.0.0.1:" + platform.Env("AETHERIA_PORTAL_PORT", "3018")

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.Healthz())
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!doctype html><html><head><title>Aetheria</title></head><body><h1>Aetheria</h1><p>Portal under construction (M1).</p></body></html>`))
	})

	s.Log("info", "portal listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		s.Log("fatal", "server exited", "error", err)
	}
}
