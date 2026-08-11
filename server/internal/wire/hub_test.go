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
	"github.com/itsbaldeep/aetheria/server/internal/auth"
	"github.com/itsbaldeep/aetheria/server/internal/platform"
	"github.com/itsbaldeep/aetheria/server/internal/world"
)

// newTestSim builds a sim with one 600×600 emberfield zone for wire tests.
func newTestSim(t *testing.T) *world.Sim {
	t.Helper()
	return world.New(world.Options{
		Zones: []*world.Zone{{ID: "emberfield", Name: "Emberfield", SizeX: 600, SizeZ: 600}},
		Logf:  func(f string, a ...any) { t.Logf(f, a...) },
	})
}

func worldVec(x, y, z float64) world.Vec3 { return world.Vec3{X: x, Y: y, Z: z} }

// TestPingPong drives a full WS round trip: connect, read ServerHello,
// send Ping, require matching Pong.
func TestPingPong(t *testing.T) {
	hub := NewHub(&platform.Service{Name: "gameserver"}, nil, nil, nil)
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
	hub := NewHub(&platform.Service{Name: "gameserver"}, nil, nil, nil)
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

// fakeLoader returns a fixed character for EnterWorld.
type fakeLoader struct {
	spawn *CharacterSpawn
	err   error
}

func (f *fakeLoader) LoadCharacter(ctx context.Context, accountID, charID int64) (*CharacterSpawn, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.spawn == nil {
		return nil, nil
	}
	return f.spawn, nil
}

// TestEnterWorldRoundTrip exercises the M2 wire path end-to-end: connect,
// EnterWorld, receive EnterWorldAck, then a WorldSnapshot stream for a moving
// nearby player.
func TestEnterWorldRoundTrip(t *testing.T) {
	sim := newTestSim(t)
	simCtx, simCancel := context.WithCancel(context.Background())
	defer simCancel()
	go sim.Run(simCtx)
	fv := &fakeValidator{id: 42}
	loader := &fakeLoader{spawn: &CharacterSpawn{
		ID: 7, Name: "Aria", Class: auth.ClassBladeDancer,
		ZoneID: "emberfield", Pos: worldVec(0, 0, 0),
		Level: 1, HP: 100, MaxHP: 100,
	}}
	hub := NewHub(&platform.Service{Name: "gameserver"}, fv, loader, sim)
	ts := httptest.NewServer(http.HandlerFunc(hub.HandleWS))
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer good-token"}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	// Hello.
	if _, _, err := conn.Read(ctx); err != nil {
		t.Fatalf("read hello: %v", err)
	}

	// EnterWorld.
	ew := &aet.Envelope{
		Seq:         1,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.EnterWorld",
		Payload:     mustMarshal(t, &aet.EnterWorld{CharacterId: 7}),
	}
	if err := conn.Write(ctx, websocket.MessageBinary, mustMarshal(t, ew)); err != nil {
		t.Fatalf("write enter: %v", err)
	}

	// EnterWorldAck.
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read ack: %v", err)
	}
	ack := &aet.EnterWorldAck{}
	if err := proto.Unmarshal(data, ack); err != nil {
		t.Fatalf("unmarshal ack: %v", err)
	}
	if !ack.Ok {
		t.Fatalf("enter world not ok: %s", ack.Error)
	}
	if ack.EntityId == 0 || ack.ZoneId != "emberfield" || ack.MaxSpeed != 8 {
		t.Fatalf("ack fields wrong: %+v", ack)
	}

	// Move the player via intent; expect a WorldSnapshot self echo.
	mv := &aet.Envelope{
		Seq:         2,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.MoveIntent",
		Payload:     mustMarshal(t, &aet.MoveIntent{Direction: &aet.Vec3{X: 1}, Speed: 8}),
	}
	if err := conn.Write(ctx, websocket.MessageBinary, mustMarshal(t, mv)); err != nil {
		t.Fatalf("write move: %v", err)
	}

	// The sim pushes Envelope-wrapped WorldSnapshots to the player's outbox;
	// the writer goroutine forwards them. Read a few and require a self echo
	// whose position advances as the player moves (+x).
	deadline := time.Now().Add(4 * time.Second)
	gotSelf := false
	for !gotSelf {
		if time.Now().After(deadline) {
			t.Fatal("no WorldSnapshot self echo within 4s")
		}
		_, data, err := conn.Read(ctx)
		if err != nil {
			continue
		}
		env := &aet.Envelope{}
		if err := proto.Unmarshal(data, env); err != nil {
			t.Fatalf("unmarshal snapshot env: %v", err)
		}
		if env.PayloadType != "aetheria.WorldSnapshot" {
			continue
		}
		snap := &aet.WorldSnapshot{}
		if err := proto.Unmarshal(env.Payload, snap); err != nil {
			t.Fatalf("unmarshal snapshot: %v", err)
		}
		if snap.SelfId != ack.EntityId || len(snap.Self) == 0 {
			continue
		}
		if snap.Self[0].Position.X > 0 {
			gotSelf = true
		}
	}
}

// TestEnterWorldWrongAccount rejects a character that isn't the caller's.
func TestEnterWorldWrongAccount(t *testing.T) {
	fv := &fakeValidator{id: 42}
	loader := &fakeLoader{spawn: nil} // loader returns nil → not found
	hub := NewHub(&platform.Service{Name: "gameserver"}, fv, loader, newTestSim(t))
	ts := httptest.NewServer(http.HandlerFunc(hub.HandleWS))
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer good-token"}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	if _, _, err := conn.Read(ctx); err != nil { // hello
		t.Fatalf("read hello: %v", err)
	}
	ew := &aet.Envelope{Kind: aet.Envelope_KIND_REQUEST, PayloadType: "aetheria.EnterWorld", Payload: mustMarshal(t, &aet.EnterWorld{CharacterId: 99})}
	if err := conn.Write(ctx, websocket.MessageBinary, mustMarshal(t, ew)); err != nil {
		t.Fatalf("write enter: %v", err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read ack: %v", err)
	}
	ack := &aet.EnterWorldAck{}
	if err := proto.Unmarshal(data, ack); err != nil {
		t.Fatalf("unmarshal ack: %v", err)
	}
	if ack.Ok {
		t.Fatal("expected enter world rejection for wrong account")
	}
	if ack.Error != "character_not_found" {
		t.Fatalf("ack error = %q, want character_not_found", ack.Error)
	}
}

// fakeValidator lets tests control handshake auth outcomes without a DB.
type fakeValidator struct {
	id    int64
	err   error
	calls int
}

func (f *fakeValidator) Validate(ctx context.Context, token string) (int64, error) {
	f.calls++
	if f.err != nil {
		return 0, f.err
	}
	return f.id, nil
}

// TestHandshakeRequiresToken asserts that a missing Authorization header is
// rejected with a policy-violation close before any ServerHello is sent.
func TestHandshakeRequiresToken(t *testing.T) {
	hub := NewHub(&platform.Service{Name: "gameserver"}, &fakeValidator{}, nil, nil)
	ts := httptest.NewServer(http.HandlerFunc(hub.HandleWS))
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial (should succeed then close): %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	// Server must close without sending ServerHello.
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("expected close after missing token, got a message")
	}
}

// TestHandshakeBanned asserts a banned account is rejected on handshake.
func TestHandshakeBanned(t *testing.T) {
	hub := NewHub(&platform.Service{Name: "gameserver"}, &fakeValidator{err: auth.ErrAccountBanned}, nil, nil)
	ts := httptest.NewServer(http.HandlerFunc(hub.HandleWS))
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer x"}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("expected close for banned account, got a message")
	}
}

// TestHandshakeValid asserts an authenticated handshake proceeds to hello.
func TestHandshakeValid(t *testing.T) {
	fv := &fakeValidator{id: 77}
	hub := NewHub(&platform.Service{Name: "gameserver"}, fv, nil, nil)
	ts := httptest.NewServer(http.HandlerFunc(hub.HandleWS))
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer good-token"}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")
	if fv.calls != 1 {
		t.Fatalf("validator calls = %d, want 1", fv.calls)
	}
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read hello: %v", err)
	}
	hello := &aet.ServerHello{}
	if err := proto.Unmarshal(data, hello); err != nil {
		t.Fatalf("unmarshal hello: %v", err)
	}
	if hello.GameName != "Aetheria" {
		t.Fatalf("hello game = %q", hello.GameName)
	}
}
