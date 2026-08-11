package wire

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	aet "github.com/itsbaldeep/aetheria/server/gen"
	"github.com/itsbaldeep/aetheria/server/internal/platform"
)

// TestPingPong drives a full WS round trip: connect, read ServerHello,
// send Ping, require matching Pong.
func TestPingPong(t *testing.T) {
	hub := NewHub(&platform.Service{Name: "gameserver"})
	ts := httptest.NewServer(http.HandlerFunc(hub.HandleWS))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	// Greeting.
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read hello: %v", err)
	}
	hello := &aet.ServerHello{}
	if err := proto.Unmarshal(data, hello); err != nil {
		t.Fatalf("unmarshal hello: %v", err)
	}
	if hello.TickRateHz != 20 {
		t.Errorf("tick rate = %d, want 20", hello.TickRateHz)
	}

	// Ping.
	sent := time.Now().UnixMilli()
	ping := &aet.Envelope{
		Seq:         1,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.Ping",
		Payload:     mustMarshal(t, &aet.Ping{SentAtUnixMs: sent}),
	}
	if err := conn.Write(ctx, websocket.MessageBinary, mustMarshal(t, ping)); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	// Pong.
	_, data, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	pong := &aet.Pong{}
	if err := proto.Unmarshal(data, pong); err != nil {
		t.Fatalf("unmarshal pong: %v", err)
	}
	if pong.SentAtUnixMs != sent {
		t.Errorf("pong echo = %d, want %d", pong.SentAtUnixMs, sent)
	}
	if pong.ServerTimeUnixMs < sent {
		t.Errorf("pong server time %d < sent %d", pong.ServerTimeUnixMs, sent)
	}
}

// TestUnknownPayload logs a warning but keeps the connection alive and
// continues to serve later pings (malformed/unknown input must not crash).
func TestUnknownPayloadThenPing(t *testing.T) {
	hub := NewHub(&platform.Service{Name: "gameserver"})
	ts := httptest.NewServer(http.HandlerFunc(hub.HandleWS))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	// consume hello
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("read hello: %v", err)
	}

	// Unknown payload type.
	bad := &aet.Envelope{Seq: 2, Kind: aet.Envelope_KIND_REQUEST, PayloadType: "aetheria.Nope"}
	if err := conn.Write(ctx, websocket.MessageBinary, mustMarshal(t, bad)); err != nil {
		t.Fatalf("write unknown: %v", err)
	}

	// Server must still answer a following ping.
	sent := time.Now().UnixMilli()
	ping := &aet.Envelope{
		Seq:         3,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.Ping",
		Payload:     mustMarshal(t, &aet.Ping{SentAtUnixMs: sent}),
	}
	if err := conn.Write(ctx, websocket.MessageBinary, mustMarshal(t, ping)); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	pong := &aet.Pong{}
	if err := proto.Unmarshal(data, pong); err != nil {
		t.Fatalf("unmarshal pong: %v", err)
	}
	if pong.SentAtUnixMs != sent {
		t.Errorf("pong echo = %d, want %d", pong.SentAtUnixMs, sent)
	}
}

func mustMarshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
