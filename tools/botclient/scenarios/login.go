// Scenario: login (M1-2). Verifies a valid credential pair returns a signed
// token, wrong password / unknown email returns 401, and a banned account is
// rejected. Used by the botclient `login` profile.
package scenarios

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// LoginResult carries the outcome of a single login attempt.
type LoginResult struct {
	Token     string
	AccountID int64
	ExpiresAt string
	Status    int
	Error     string
}

// Login attempts one login against the authserver.
func Login(baseURL, email, password string) (LoginResult, error) {
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	req, err := http.NewRequest(http.MethodPost, baseURL+"/auth/login", bytes.NewReader(body))
	if err != nil {
		return LoginResult{}, fmt.Errorf("scenarios: build login req: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "203.0."+randOctet()+".1")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return LoginResult{}, fmt.Errorf("scenarios: login req: %w", err)
	}
	defer resp.Body.Close()

	res := LoginResult{Status: resp.StatusCode}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil {
		if s, ok := payload["error"].(string); ok {
			res.Error = s
		}
		if f, ok := payload["token"].(string); ok {
			res.Token = f
		}
		if f, ok := payload["expires_at"].(string); ok {
			res.ExpiresAt = f
		}
		if f, ok := payload["id"].(float64); ok {
			res.AccountID = int64(f)
		}
	}
	return res, nil
}

func randOctet() string {
	return fmt.Sprintf("%d", 1+time.Now().Nanosecond()%240)
}
