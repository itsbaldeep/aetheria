// portal — public website: registration, client downloads, rankings,
// voting + donations. See docs/BRIEF.md §3 and §6. M1 (registration) and
// M9 (downloads/voting/donations) land here.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/itsbaldeep/aetheria/server/internal/platform"
)

func main() {
	s := &platform.Service{Name: "portal"}
	addr := "127.0.0.1:" + platform.Env("AETHERIA_PORTAL_PORT", "3018")
	authURL := platform.Env("AETHERIA_AUTH_URL", "http://127.0.0.1:3016")

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.Healthz())
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<!doctype html><html><head><title>Aetheria</title></head><body><h1>Aetheria</h1><p><a href="/register">Create account</a></p></body></html>`))
	})
	mux.HandleFunc("/register", handleRegister(s, authURL))

	s.Log("info", "portal listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		s.Log("fatal", "server exited", "error", err)
	}
}

// handleRegister serves the server-rendered registration form (GET) and
// proxies the POST to the authserver (/auth/register). Credential logic
// stays in one place (authserver); the portal only renders + relays.
func handleRegister(s *platform.Service, authURL string) http.HandlerFunc {
	const formHTML = `<!doctype html><html><head><title>Register — Aetheria</title></head><body><h1>Create your Aetheria account</h1><form method="post" action="/register"><label>Email <input type="email" name="email" required></label><br><label>Password <input type="password" name="password" minlength="8" required></label><br><button type="submit">Register</button></form></body></html>`

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(formHTML))
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			writeResult(w, "Invalid form submission.")
			return
		}
		body, _ := json.Marshal(map[string]string{
			"email":    r.FormValue("email"),
			"password": r.FormValue("password"),
		})
		req, err := http.NewRequest(http.MethodPost, authURL+"/auth/register", bytes.NewReader(body))
		if err != nil {
			s.Log("error", "build register request failed", "error", err)
			writeResult(w, "Internal error; try again later.")
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", clientIP(r))

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			s.Log("error", "authserver register failed", "error", err)
			writeResult(w, "Registration service unavailable; try again later.")
			return
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)

		switch resp.StatusCode {
		case http.StatusCreated:
			writeResult(w, "Account created! You can now log in.")
		default:
			msg := "Registration failed."
			if len(raw) > 0 {
				var e struct {
					Error string `json:"error"`
				}
				if json.Unmarshal(raw, &e) == nil && e.Error != "" {
					msg = friendlyError(e.Error)
				}
			}
			writeResult(w, msg)
		}
	}
}

func friendlyError(e string) string {
	switch e {
	case "email_taken":
		return "That email is already registered."
	case "rate_limited":
		return "Too many attempts. Wait a few minutes and retry."
	default:
		return e
	}
}

func writeResult(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html><head><title>Aetheria</title></head><body><h1>Registration</h1><p>%s</p><p><a href="/register">Try again</a></p></body></html>`, msg)
}

func clientIP(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip != "" {
		return ip
	}
	host := r.RemoteAddr
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			return host[:i]
		}
	}
	return host
}
