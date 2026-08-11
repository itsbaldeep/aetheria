// HTTP handlers for the authserver (/auth/register, /auth/login, and the
// character endpoints). JSON in/out; every handler is rate-limited per IP.
package auth

import (
	"encoding/json"
	"net/http"
	"time"
)

// Server bundles the account store + rate limiter for the auth HTTP API.
type Server struct {
	store   *Store
	limiter *Limiter
	// Logf receives internal errors for diagnosis (never sent to clients).
	Logf func(level, msg string, kv ...any)
	// hashes bounds concurrent argon2 hashing. argon2 uses ~64 MiB each, so
	// this must never scale with request count — 2 concurrent keeps worst
	// case ~128 MiB inside the 256 MiB container and blocks hash-storm DoS.
	hashes chan struct{}
	// RegisterLimit / LoginLimit are events allowed per IP per window.
	RegisterLimit int
	LoginLimit    int
	Window        time.Duration
}

func NewServer(store *Store, limiter *Limiter) *Server {
	return &Server{
		store:         store,
		limiter:       limiter,
		hashes:        make(chan struct{}, 2),
		RegisterLimit: 5,
		LoginLimit:    10,
		Window:        15 * time.Minute,
	}
}

// withHash runs fn while holding one of the hash budget slots. It keeps
// memory bounded: the hashing stage is the only concurrent argon2 section.
func (s *Server) withHash(fn func() error) error {
	s.hashes <- struct{}{}
	defer func() { <-s.hashes }()
	return fn()
}

type registerReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// HandleRegister validates, rate-limits, hashes, and inserts an account.
//
//	201 + {"id":…}          success
//	400 {"error":…}         bad email/password
//	409 {"error":"email_taken"}
//	429 {"error":"rate_limited"}
func (s *Server) HandleRegister(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	ok, err := s.limiter.Allow(r.Context(), "register:"+ip, s.RegisterLimit, s.Window)
	if err != nil {
		s.srvErr(w, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
		return
	}

	var req registerReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_request"})
		return
	}
	email, err := ValidateEmail(req.Email)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := ValidatePassword(req.Password); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var id int64
	if herr := s.withHash(func() (err error) {
		var hash string
		hash, err = HashPassword(req.Password)
		if err != nil {
			return err
		}
		id, err = s.store.Register(r.Context(), email, hash)
		return err
	}); herr != nil {
		switch {
		case herr == ErrEmailTaken:
			writeJSON(w, http.StatusConflict, map[string]string{"error": "email_taken"})
		default:
			s.srvErr(w, herr)
		}
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// HandleLogin validates credentials and returns a session token. M1 scope:
// token issues live in authserver; gameserver validates on WS handshake (M1-5).
func (s *Server) HandleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	ok, err := s.limiter.Allow(r.Context(), "login:"+ip, s.LoginLimit, s.Window)
	if err != nil {
		s.srvErr(w, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
		return
	}

	var req loginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad_request"})
		return
	}
	email, err := ValidateEmail(req.Email)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	id, hash, bannedUntil, err := s.store.Credentials(r.Context(), email)
	if err != nil {
		s.srvErr(w, err)
		return
	}
	if id == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "bad_credentials"})
		return
	}
	if bannedUntil != nil && time.Now().Before(*bannedUntil) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "account_banned"})
		return
	}
	var valid bool
	if herr := s.withHash(func() error {
		valid = VerifyPassword(hash, req.Password)
		return nil
	}); herr != nil {
		s.srvErr(w, herr)
		return
	}
	if !valid {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "bad_credentials"})
		return
	}
	// M1-2 replaces this stub with a signed token. Return account id for now.
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
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

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func (s *Server) srvErr(w http.ResponseWriter, err error) {
	if s.Logf != nil {
		s.Logf("error", "internal error", "error", err.Error())
	}
	// Internal errors never leak details to the client (brief §10).
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
}
