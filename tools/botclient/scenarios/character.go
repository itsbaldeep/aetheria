// Scenario: character create + list (M1-3). Authenticated with a session
// token, creates a character and lists the roster, asserting name/class rules
// and ownership. Used by the botclient `create-char` profile.
package scenarios

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// CreateCharacter posts a character creation with the given Bearer token.
// Returns HTTP status + server error string.
func CreateCharacter(baseURL, token, name, class string) (int, string, error) {
	body, _ := json.Marshal(map[string]string{"name": name, "class": class})
	req, err := http.NewRequest(http.MethodPost, baseURL+"/auth/characters/create", bytes.NewReader(body))
	if err != nil {
		return 0, "", fmt.Errorf("scenarios: build create-char req: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Forwarded-For", "203.0."+randOctet()+".2")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("scenarios: create-char req: %w", err)
	}
	defer resp.Body.Close()
	var payload struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&payload)
	return resp.StatusCode, payload.Error, nil
}

// ListCharacters returns the roster for a token, or an error.
func ListCharacters(baseURL, token string) ([]map[string]any, int, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/auth/characters", nil)
	if err != nil {
		return nil, 0, fmt.Errorf("scenarios: build list req: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Forwarded-For", "203.0."+randOctet()+".3")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("scenarios: list req: %w", err)
	}
	defer resp.Body.Close()
	var payload struct {
		Characters []map[string]any `json:"characters"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("scenarios: list decode: %w", err)
	}
	return payload.Characters, resp.StatusCode, nil
}
