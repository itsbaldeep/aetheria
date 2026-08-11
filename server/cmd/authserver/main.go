// authserver — accounts, registration, login (argon2id), session tokens,
// character list/create endpoints. See docs/BRIEF.md §3.
package main

import (
	"net/http"

	"github.com/itsbaldeep/aetheria/server/internal/platform"
)

func main() {
	s := &platform.Service{Name: "authserver"}
	addr := "127.0.0.1:" + platform.Env("AETHERIA_AUTH_PORT", "3016")

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.Healthz())
	// M1: register/login/character endpoints land here.

	s.Log("info", "authserver listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		s.Log("fatal", "server exited", "error", err)
	}
}
