// Package platform provides tiny shared bootstrap helpers for all four
// Aetheria services: env config, JSON logging, and a /healthz endpoint.
package platform

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

// Service identifies which binary is running. Keep names in sync with the
// systemd units (deploy/systemd).
type Service struct {
	Name string
}

// Log writes one JSON line to stdout: ts, service, level, msg, plus optional
// key/value fields. systemd/journald captures stdout, so this is the single
// log path for the whole platform.
func (s *Service) Log(level, msg string, kv ...any) {
	e := map[string]any{
		"ts":      time.Now().UTC().Format(time.RFC3339),
		"service": s.Name,
		"level":   level,
		"msg":     msg,
	}
	for i := 0; i+1 < len(kv); i += 2 {
		if k, ok := kv[i].(string); ok {
			e[k] = kv[i+1]
		}
	}
	b, err := json.Marshal(e)
	if err != nil {
		log.Printf("log-encode-error: %v", err)
		return
	}
	log.Println(string(b))
}

// Env reads an env var with a default. All config flows through env vars set
// by the systemd EnvironmentFile (/etc/aetheria/env); nothing is hardcoded.
func Env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Healthz returns a handler that reports "ok" with the service name.
// Caddy and the health-check watchdog probe these endpoints.
func (s *Service) Healthz() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"service": s.Name,
			"status":  "ok",
			"time":    time.Now().UTC().Format(time.RFC3339),
		})
	}
}
