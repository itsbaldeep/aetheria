// Scenario: M2 world presence — a bot session that connects to the gameserver
// with a real session token, enters the world, sends movement intents, and
// consumes 20 Hz WorldSnapshot frames. Used by the roamer / presence / soak
// profiles.
package scenarios

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	aet "github.com/itsbaldeep/aetheria/server/gen"
)

// WorldBot is an authenticated gameserver connection in the world.
type WorldBot struct {
	conn     *websocket.Conn
	EntityID uint64
	ZoneID   string
	PosX     float64
	PosY     float64
	PosZ     float64
	MaxSpeed float64
	seq      uint64

	// Seen is the most recent EntityState per entity id (from snapshots).
	Seen map[uint64]*aet.EntityState
	// Despawned records entity ids that left our AOI.
	Despawned []uint64
	// SnapshotCount counts WorldSnapshots received.
	SnapshotCount int
}

// ConnectWorld dials the gameserver WS with a Bearer token, reads ServerHello,
// sends EnterWorld, and waits for the EnterWorldAck.
func ConnectWorld(ctx context.Context, wsURL, token string, charID int64) (*WorldBot, error) {
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + token}},
	})
	if err != nil {
		return nil, fmt.Errorf("scenarios: dial: %w", err)
	}

	// ServerHello.
	if _, data, err := conn.Read(ctx); err != nil {
		conn.Close(websocket.StatusNormalClosure, "")
		return nil, fmt.Errorf("scenarios: read hello: %w", err)
	} else {
		hello := &aet.ServerHello{}
		if err := proto.Unmarshal(data, hello); err != nil {
			conn.Close(websocket.StatusNormalClosure, "")
			return nil, fmt.Errorf("scenarios: unmarshal hello: %w", err)
		}
	}

	// EnterWorld.
	ew := &aet.Envelope{
		Seq:         1,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.EnterWorld",
		Payload:     mustMarshal(&aet.EnterWorld{CharacterId: charID}),
	}
	if err := conn.Write(ctx, websocket.MessageBinary, mustMarshal(ew)); err != nil {
		conn.Close(websocket.StatusNormalClosure, "")
		return nil, fmt.Errorf("scenarios: write enter: %w", err)
	}

	// EnterWorldAck.
	_, data, err := conn.Read(ctx)
	if err != nil {
		conn.Close(websocket.StatusNormalClosure, "")
		return nil, fmt.Errorf("scenarios: read ack: %w", err)
	}
	ack := &aet.EnterWorldAck{}
	if err := proto.Unmarshal(data, ack); err != nil {
		conn.Close(websocket.StatusNormalClosure, "")
		return nil, fmt.Errorf("scenarios: unmarshal ack: %w", err)
	}
	if !ack.Ok {
		conn.Close(websocket.StatusNormalClosure, "")
		return nil, fmt.Errorf("scenarios: enter rejected: %s", ack.Error)
	}

	b := &WorldBot{
		conn:     conn,
		EntityID: ack.EntityId,
		ZoneID:   ack.ZoneId,
		MaxSpeed: float64(ack.MaxSpeed),
		Seen:     make(map[uint64]*aet.EntityState),
	}
	if ack.Position != nil {
		b.PosX, b.PosY, b.PosZ = float64(ack.Position.X), float64(ack.Position.Y), float64(ack.Position.Z)
	}
	return b, nil
}

// Move sends a MoveIntent (direction + speed). Speed is clamped server-side.
func (b *WorldBot) Move(ctx context.Context, dirX, dirZ, speed float64) error {
	b.seq++
	mv := &aet.Envelope{
		Seq:         b.seq,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.MoveIntent",
		Payload: mustMarshal(&aet.MoveIntent{
			Direction: &aet.Vec3{X: float32(dirX), Y: 0, Z: float32(dirZ)},
			Speed:     float32(speed),
		}),
	}
	return b.conn.Write(ctx, websocket.MessageBinary, mustMarshal(mv))
}

// Stop sends an empty MoveIntent (server interprets as stop).
func (b *WorldBot) Stop(ctx context.Context) error {
	b.seq++
	mv := &aet.Envelope{
		Seq:         b.seq,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.MoveIntent",
		Payload:     mustMarshal(&aet.MoveIntent{}),
	}
	return b.conn.Write(ctx, websocket.MessageBinary, mustMarshal(mv))
}

// ReadSnapshot blocks until the next WorldSnapshot envelope arrives and
// updates b.Seen/Despawned/positions. Returns false on connection error.
func (b *WorldBot) ReadSnapshot(ctx context.Context) bool {
	_, data, err := b.conn.Read(ctx)
	if err != nil {
		return false
	}
	env := &aet.Envelope{}
	if err := proto.Unmarshal(data, env); err != nil {
		return false
	}
	if env.PayloadType != "aetheria.WorldSnapshot" {
		return false
	}
	snap := &aet.WorldSnapshot{}
	if err := proto.Unmarshal(env.Payload, snap); err != nil {
		return false
	}
	b.SnapshotCount++
	for _, e := range snap.Self {
		b.EntityID = e.EntityId
		b.PosX, b.PosY, b.PosZ = float64(e.Position.X), float64(e.Position.Y), float64(e.Position.Z)
		b.Seen[e.EntityId] = e
	}
	for _, e := range snap.Entities {
		b.Seen[e.EntityId] = e
	}
	for _, id := range snap.DespawnIds {
		b.Despawned = append(b.Despawned, id)
		delete(b.Seen, id)
	}
	return true
}

// ReadSnapshots consumes up to n snapshots (or until ctx done).
func (b *WorldBot) ReadSnapshots(ctx context.Context, n int) int {
	read := 0
	for read < n {
		select {
		case <-ctx.Done():
			return read
		default:
		}
		if !b.ReadSnapshot(ctx) {
			return read
		}
		read++
	}
	return read
}

// PingRoundTrip sends a Ping and waits for the Pong (liveness check).
func (b *WorldBot) PingRoundTrip(ctx context.Context) (int64, error) {
	sent := time.Now().UnixMilli()
	ping := &aet.Envelope{
		Seq:         b.seq + 1,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.Ping",
		Payload:     mustMarshal(&aet.Ping{SentAtUnixMs: sent}),
	}
	if err := b.conn.Write(ctx, websocket.MessageBinary, mustMarshal(ping)); err != nil {
		return 0, err
	}
	for {
		_, data, err := b.conn.Read(ctx)
		if err != nil {
			return 0, err
		}
		// The server sends Pong as a raw aetheria.Pong (not Envelope-wrapped);
		// world frames arrive as Envelope-wrapped WorldSnapshots. A Pong proto
		// unmarshalled as an Envelope yields empty PayloadType, so accept a
		// frame as a candidate pong only when it is not an Envelope.
		env := &aet.Envelope{}
		if proto.Unmarshal(data, env) == nil && env.PayloadType != "" {
			continue
		}
		pong := &aet.Pong{}
		if err := proto.Unmarshal(data, pong); err != nil {
			continue
		}
		if pong.SentAtUnixMs != sent {
			continue
		}
		return time.Now().UnixMilli() - sent, nil
	}
}

// Close sends LeaveWorld (best effort) and closes the socket.
func (b *WorldBot) Close() {
	if b.conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	lw := &aet.Envelope{
		Seq:         b.seq + 1,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.LeaveWorld",
	}
	_ = b.conn.Write(ctx, websocket.MessageBinary, mustMarshal(lw))
	b.conn.Close(websocket.StatusNormalClosure, "done")
}

// RawWrite writes a raw frame (for the chaos fuzzer).
func (b *WorldBot) RawWrite(ctx context.Context, frame []byte) error {
	return b.conn.Write(ctx, websocket.MessageBinary, frame)
}

// FindEntity returns the last EntityState seen for an entity id.
func (b *WorldBot) FindEntity(id uint64) *aet.EntityState { return b.Seen[id] }

// mustMarshal panics on marshal failure (frames are fixed small shapes).
func mustMarshal(m proto.Message) []byte {
	b, err := proto.Marshal(m)
	if err != nil {
		panic(err)
	}
	return b
}
