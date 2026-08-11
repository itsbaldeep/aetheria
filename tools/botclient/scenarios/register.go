// Scenario: concurrent registration (M1 acceptance: "20 concurrent
// registrations succeed"). Hits the live authserver /auth/register over the
// public API base, verifies every HTTP status, then the caller (or a follow
// up) checks the argon2id prefix in Postgres.
package scenarios

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RegisterConfig drives a batch of concurrent registrations.
type RegisterConfig struct {
	BaseURL   string // e.g. https://api.aetheria.apps.deployden.tech or http://127.0.0.1:3016
	Count     int
	EmailFmt  string // fmt with %d, e.g. "reg-%d@aetheria.test"
	Password  string
	BatchSize int // per-IP budget for the rate limiter (RegisterLimit)
}

// RegisterBatch performs `Count` concurrent registrations using `BatchSize`
// distinct client IPs so the per-IP rate limit does not false-positive.
// Returns the number that succeeded (201) and the first failure, if any.
func RegisterBatch(cfg RegisterConfig) (ok int, err error) {
	if cfg.Count <= 0 {
		return 0, fmt.Errorf("scenarios: Count must be > 0")
	}
	ips := cfg.BatchSize
	if ips < 1 {
		ips = 1
	}
	addr := cfg.BaseURL + "/auth/register"
	// Random third octet per run so the per-IP rate limiter (5 / 15 min)
	// does not false-positive on repeat runs within the window.
	ipBase := 1 + time.Now().Nanosecond()%240

	var (
		mu   sync.Mutex
		good int
		wg   sync.WaitGroup
	)
	for i := 1; i <= cfg.Count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			email := cfg.EmailFmt
			if strings.Contains(email, "%d") {
				email = fmt.Sprintf(email, i)
			}
			body, _ := json.Marshal(map[string]string{
				"email":    email,
				"password": cfg.Password,
			})
			req, rerr := http.NewRequest(http.MethodPost, addr, bytes.NewReader(body))
			if rerr != nil {
				mu.Lock()
				err = fmt.Errorf("build req %d: %w", i, rerr)
				mu.Unlock()
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.%d.%d", ipBase, 1+(i%ips)))
			resp, herr := (&http.Client{Timeout: 10 * time.Second}).Do(req)
			if herr != nil {
				mu.Lock()
				err = fmt.Errorf("req %d: %w", i, herr)
				mu.Unlock()
				return
			}
			defer resp.Body.Close()
			io.Copy(io.Discard, resp.Body)
			if resp.StatusCode == http.StatusCreated {
				mu.Lock()
				good++
				mu.Unlock()
				return
			}
			mu.Lock()
			if err == nil {
				err = fmt.Errorf("req %d: status %d (want 201)", i, resp.StatusCode)
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	return good, err
}
