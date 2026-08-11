// Scenario: full M1 acceptance flow — register → login → create character →
// authenticated WS session, plus the rejection paths (wrong password, no
// token, garbage token, banned account). Used by `make bottest` (M1-6).
package scenarios

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// AuthSessionProbe is the outcome of an attempted WS connection.
type AuthSessionProbe struct {
	DialErr error
	// GotHello is true when the server sent ServerHello (accepted).
	GotHello bool
	// CloseReason is the policy-violation close reason on rejection.
	CloseReason string
}

// ConnectAuthed attempts a gameserver WS connection with an optional Bearer
// token. When wantAccept is true the caller expects ServerHello; when false
// the caller expects a policy-violation close before any hello.
func ConnectAuthed(wsURL, token string, wantAccept bool) (AuthSessionProbe, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	opts := &websocket.DialOptions{}
	if token != "" {
		opts.HTTPHeader = http.Header{"Authorization": {"Bearer " + token}}
	}
	conn, _, err := websocket.Dial(ctx, wsURL, opts)
	if err != nil {
		return AuthSessionProbe{DialErr: err}, nil
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	_, _, readErr := conn.Read(ctx)
	probe := AuthSessionProbe{}
	if readErr == nil {
		probe.GotHello = true
	} else {
		// The CloseError is wrapped by the reader; unwrap to get status/reason.
		var ce websocket.CloseError
		if errors.As(readErr, &ce) {
			probe.CloseReason = ce.Reason
		} else {
			probe.CloseReason = readErr.Error()
		}
	}
	if wantAccept != probe.GotHello {
		return probe, fmt.Errorf("scenarios: wantAccept=%v got=%v (reason=%s)",
			wantAccept, probe.GotHello, probe.CloseReason)
	}
	return probe, nil
}
