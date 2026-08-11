// Package wire implements the gameserver's WebSocket protocol layer:
// accepts connections, frames Envelope messages, dispatches by payload type,
// and handles the M0 ping/pong + ServerHello handshake.
package wire

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	aet "github.com/itsbaldeep/aetheria/server/gen"
	"github.com/itsbaldeep/aetheria/server/internal/platform"
)

// Hub owns all live connections and routes envelopes to handlers.
type Hub struct {
	s *platform.Service
}

func NewHub(s *platform.Service) *Hub {
	return &Hub{s: s}
}

// Run is a placeholder lifecycle loop (future: connection registry, sessions).
func (h *Hub) Run() {}

// HandleWS upgrades an HTTP request to a WebSocket connection, sends the
// ServerHello greeting, then reads envelopes and dispatches them.
func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		h.s.Log("warn", "ws accept failed", "error", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "bye")
	remote := r.RemoteAddr
	h.s.Log("info", "client connected", "remote", remote)

	// Greeting first (M0 protocol handshake).
	hello := &aet.ServerHello{
		ProtocolVersion: "0.1.0",
		GameName:        "Aetheria",
		TickRateHz:      20,
	}
	if err := h.send(conn, hello); err != nil {
		h.s.Log("warn", "hello send failed", "remote", remote, "error", err)
		return
	}

	for {
		env, err := h.receive(conn)
		if err != nil {
			h.s.Log("info", "client disconnected", "remote", remote)
			return
		}
		h.dispatch(conn, env)
	}
}

func (h *Hub) send(conn *websocket.Conn, msg proto.Message) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	b, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageBinary, b)
}

// receive reads one binary frame and decodes it as an Envelope.
func (h *Hub) receive(conn *websocket.Conn) (*aet.Envelope, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	env := &aet.Envelope{}
	if err := proto.Unmarshal(data, env); err != nil {
		h.s.Log("warn", "bad envelope bytes", "len", len(data), "error", err)
		return nil, err
	}
	return env, nil
}

// dispatch routes an envelope by its payload_type. M0 handles Ping→Pong.
// Later milestones add Move, CastSkill, Trade, etc. here.
func (h *Hub) dispatch(conn *websocket.Conn, env *aet.Envelope) {
	switch env.PayloadType {
	case "aetheria.Ping":
		p := &aet.Ping{}
		if err := proto.Unmarshal(env.Payload, p); err != nil {
			h.s.Log("warn", "bad ping payload", "error", err)
			return
		}
		pong := &aet.Pong{
			SentAtUnixMs:     p.SentAtUnixMs,
			ServerTimeUnixMs: time.Now().UnixMilli(),
		}
		if err := h.send(conn, pong); err != nil {
			h.s.Log("warn", "pong send failed", "error", err)
		}
	default:
		h.s.Log("warn", "unknown payload type", "type", env.PayloadType)
	}
}
