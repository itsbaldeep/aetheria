// Scenario: M2 world presence — a bot session that connects to the gameserver
// with a real session token, enters the world, sends movement intents, and
// consumes 20 Hz WorldSnapshot frames. Used by the roamer / presence / soak
// profiles.
package scenarios

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	aet "github.com/itsbaldeep/aetheria/server/gen"
)

// WorldBot is an authenticated gameserver connection in the world.
type WorldBot struct {
	conn *websocket.Conn
	// writeMu serializes all outbound frames: a websocket allows a single
	// concurrent writer, and the combat scenario writes from both the main
	// loop and the heartbeat goroutine.
	writeMu sync.Mutex

	EntityID uint64
	ZoneID   string
	PosX     float64
	PosY     float64
	PosZ     float64
	MaxSpeed float64
	seq      uint64
	// autoTarget is the entity id the bot is currently auto-attacking.
	autoTarget uint64

	// Seen is the most recent EntityState per entity id (from snapshots).
	Seen map[uint64]*aet.EntityState
	// Despawned records entity ids that left our AOI.
	Despawned []uint64
	// SnapshotCount counts WorldSnapshots received.
	SnapshotCount int
	// Combat holds relayed CombatEvent frames read so far (combat log).
	Combat []*aet.CombatEvent
	// Loot holds relayed LootEvent frames read so far (M4 economy log).
	Loot []*aet.LootEvent
	// Chat holds relayed ChatMessage frames read so far.
	Chat []*aet.ChatMessage
	// LastRespawnAck is the most recent RespawnAck received.
	LastRespawnAck *aet.RespawnAck
	// LastSelfHP tracks the player's HP from the last `self` snapshot.
	LastSelfHP int64
	// LastSelfLevel tracks the player's level from the last `self` snapshot.
	LastSelfLevel int32
	// Quests holds relayed QuestEvent frames (M5 objective/state updates).
	Quests []*aet.QuestEvent
	// Dialogs holds relayed NpcDialogEvent frames (M5).
	Dialogs []*aet.NpcDialogEvent
	// LastQuestStatus holds the most recent full QuestStatusEvent (M5).
	LastQuestStatus *aet.QuestStatusEvent
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

// write serializes a frame onto the socket. All outbound frames go through
// this so the heartbeat goroutine and the main loop never write concurrently.
func (b *WorldBot) write(ctx context.Context, env *aet.Envelope) error {
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	return b.conn.Write(ctx, websocket.MessageBinary, mustMarshal(env))
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
	return b.write(ctx, mv)
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
	return b.write(ctx, mv)
}

// AutoAttack sets or clears auto-attack on a target entity.
func (b *WorldBot) AutoAttack(ctx context.Context, target uint64, active bool) error {
	b.seq++
	aa := &aet.Envelope{
		Seq:         b.seq,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.AutoAttack",
		Payload:     mustMarshal(&aet.AutoAttack{TargetEntityId: target, Active: active}),
	}
	return b.write(ctx, aa)
}

// Cast sends a CastSkill request. AimPos is optional (aimed/pbaoe kinds).
func (b *WorldBot) Cast(ctx context.Context, skillID string, target uint64, aim *aet.Vec3) error {
	b.seq++
	cs := &aet.Envelope{
		Seq:         b.seq,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.CastSkill",
		Payload:     mustMarshal(&aet.CastSkill{SkillId: skillID, TargetEntityId: target, AimPosition: aim}),
	}
	return b.write(ctx, cs)
}

// SendChat sends a chat message on a channel (say|world). Server fills sender.
func (b *WorldBot) SendChat(ctx context.Context, channel, text string) error {
	b.seq++
	cm := &aet.Envelope{
		Seq:         b.seq,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.ChatMessage",
		Payload:     mustMarshal(&aet.ChatMessage{Channel: channel, Text: text}),
	}
	return b.write(ctx, cm)
}

// Respawn sends a RespawnRequest after death.
func (b *WorldBot) Respawn(ctx context.Context) error {
	b.seq++
	rq := &aet.Envelope{
		Seq:         b.seq,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.RespawnRequest",
		Payload:     mustMarshal(&aet.RespawnRequest{}),
	}
	return b.write(ctx, rq)
}

// KeepAlive sends a Ping (fire-and-forget; its Pong is drained by the next
// ReadSnapshot). The server's read loop idles out after 10 s of no inbound
// frames, so idle phases must stay chatty.
func (b *WorldBot) KeepAlive(ctx context.Context) error {
	b.seq++
	p := &aet.Envelope{
		Seq:         b.seq,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.Ping",
		Payload:     mustMarshal(&aet.Ping{SentAtUnixMs: time.Now().UnixMilli()}),
	}
	return b.write(ctx, p)
}

// PickupItem asks the server to claim a ground drop by entity id (M4).
func (b *WorldBot) PickupItem(ctx context.Context, dropID uint64) error {
	b.seq++
	env := &aet.Envelope{
		Seq:         b.seq,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.PickupItem",
		Payload:     mustMarshal(&aet.PickupItem{DropEntityId: dropID}),
	}
	return b.write(ctx, env)
}

// EquipItem equips an inventory item by instance id (M4).
func (b *WorldBot) EquipItem(ctx context.Context, itemID uint64) error {
	b.seq++
	env := &aet.Envelope{
		Seq:         b.seq,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.EquipItem",
		Payload:     mustMarshal(&aet.EquipItem{ItemId: itemID}),
	}
	return b.write(ctx, env)
}

// UnequipItem unequips an equipment slot (M4).
func (b *WorldBot) UnequipItem(ctx context.Context, slot string) error {
	b.seq++
	env := &aet.Envelope{
		Seq:         b.seq,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.UnequipItem",
		Payload:     mustMarshal(&aet.UnequipItem{Slot: slot}),
	}
	return b.write(ctx, env)
}

// SellItem sells inventory items to a vendor (M4).
func (b *WorldBot) SellItem(ctx context.Context, itemID uint64, qty int32) error {
	b.seq++
	env := &aet.Envelope{
		Seq:         b.seq,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.SellItem",
		Payload:     mustMarshal(&aet.SellItem{ItemId: itemID, Quantity: qty}),
	}
	return b.write(ctx, env)
}

// BuyItem buys an item def from a vendor's stock (M4).
func (b *WorldBot) BuyItem(ctx context.Context, vendorID, itemDefID string, qty int32) error {
	b.seq++
	env := &aet.Envelope{
		Seq:         b.seq,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.BuyItem",
		Payload:     mustMarshal(&aet.BuyItem{VendorId: vendorID, ItemDefId: itemDefID, Quantity: qty}),
	}
	return b.write(ctx, env)
}

// NpcInteract talks to an NPC by def id (M5).
func (b *WorldBot) NpcInteract(ctx context.Context, npcID string) error {
	b.seq++
	env := &aet.Envelope{
		Seq:         b.seq,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.NpcInteract",
		Payload:     mustMarshal(&aet.NpcInteract{NpcId: npcID}),
	}
	return b.write(ctx, env)
}

// QuestAccept accepts a quest (M5).
func (b *WorldBot) QuestAccept(ctx context.Context, questID string) error {
	b.seq++
	env := &aet.Envelope{
		Seq:         b.seq,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.QuestAccept",
		Payload:     mustMarshal(&aet.QuestAccept{QuestId: questID}),
	}
	return b.write(ctx, env)
}

// QuestAbandon drops an active quest (M5).
func (b *WorldBot) QuestAbandon(ctx context.Context, questID string) error {
	b.seq++
	env := &aet.Envelope{
		Seq:         b.seq,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.QuestAbandon",
		Payload:     mustMarshal(&aet.QuestAbandon{QuestId: questID}),
	}
	return b.write(ctx, env)
}

// QuestTurnIn completes an active quest at its turn-in NPC (M5).
func (b *WorldBot) QuestTurnIn(ctx context.Context, questID string) error {
	b.seq++
	env := &aet.Envelope{
		Seq:         b.seq,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.QuestTurnIn",
		Payload:     mustMarshal(&aet.QuestTurnIn{QuestId: questID}),
	}
	return b.write(ctx, env)
}

// QuestStatus requests the character's full quest status (M5).
func (b *WorldBot) QuestStatus(ctx context.Context) error {
	b.seq++
	env := &aet.Envelope{
		Seq:         b.seq,
		Kind:        aet.Envelope_KIND_REQUEST,
		PayloadType: "aetheria.QuestStatus",
		Payload:     mustMarshal(&aet.QuestStatus{}),
	}
	return b.write(ctx, env)
}

// StartHeartbeat runs KeepAlive on a background ticker until ctx is done,
// then exits. The read loop may block for a long time (no snapshots when
// nothing changes), which would otherwise starve the server's inbound read
// loop and get the socket idled out.
func (b *WorldBot) StartHeartbeat(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := b.KeepAlive(ctx); err != nil {
					return
				}
			}
		}
	}()
}

// FindHostile returns the nearest hostile (mob) entity in AOI, or nil.
func (b *WorldBot) FindHostile() *aet.EntityState {
	var best *aet.EntityState
	var bestDist float64
	for _, e := range b.Seen {
		if e.EntityType != "mob" {
			continue
		}
		if e.Hp <= 0 {
			continue
		}
		dx := float64(e.Position.X) - b.PosX
		dz := float64(e.Position.Z) - b.PosZ
		d := dx*dx + dz*dz
		if best == nil || d < bestDist {
			best, bestDist = e, d
		}
	}
	return best
}

// DrainCombat returns and clears accumulated CombatEvents.
func (b *WorldBot) DrainCombat() []*aet.CombatEvent {
	out := b.Combat
	b.Combat = nil
	return out
}

// ReadFrame reads one frame (any payload type) and routes it into the bot's
// state. Returns an envelope (never nil on success).
func (b *WorldBot) ReadFrame(ctx context.Context) (*aet.Envelope, error) {
	_, data, err := b.conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	env := &aet.Envelope{}
	if err := proto.Unmarshal(data, env); err != nil {
		return env, fmt.Errorf("scenarios: unmarshal frame: %w", err)
	}
	switch env.PayloadType {
	case "aetheria.WorldSnapshot":
		snap := &aet.WorldSnapshot{}
		if err := proto.Unmarshal(env.Payload, snap); err != nil {
			return env, fmt.Errorf("scenarios: unmarshal snapshot: %w", err)
		}
		b.SnapshotCount++
		for _, e := range snap.Self {
			b.EntityID = e.EntityId
			b.PosX, b.PosY, b.PosZ = float64(e.Position.X), float64(e.Position.Y), float64(e.Position.Z)
			b.LastSelfHP = e.Hp
			b.LastSelfLevel = e.Level
			b.Seen[e.EntityId] = e
		}
		for _, e := range snap.Entities {
			b.Seen[e.EntityId] = e
		}
		for _, id := range snap.DespawnIds {
			b.Despawned = append(b.Despawned, id)
			delete(b.Seen, id)
		}
	case "aetheria.CombatEvent":
		ev := &aet.CombatEvent{}
		if err := proto.Unmarshal(env.Payload, ev); err == nil {
			b.Combat = append(b.Combat, ev)
		}
	case "aetheria.ChatMessage":
		cm := &aet.ChatMessage{}
		if err := proto.Unmarshal(env.Payload, cm); err == nil {
			b.Chat = append(b.Chat, cm)
		}
	case "aetheria.RespawnAck":
		ack := &aet.RespawnAck{}
		if err := proto.Unmarshal(env.Payload, ack); err == nil {
			b.LastRespawnAck = ack
		}
	case "aetheria.LootEvent":
		le := &aet.LootEvent{}
		if err := proto.Unmarshal(env.Payload, le); err == nil {
			b.Loot = append(b.Loot, le)
		}
	case "aetheria.QuestEvent":
		ev := &aet.QuestEvent{}
		if err := proto.Unmarshal(env.Payload, ev); err == nil {
			b.Quests = append(b.Quests, ev)
		}
	case "aetheria.NpcDialogEvent":
		d := &aet.NpcDialogEvent{}
		if err := proto.Unmarshal(env.Payload, d); err == nil {
			b.Dialogs = append(b.Dialogs, d)
		}
	case "aetheria.QuestStatusEvent":
		qs := &aet.QuestStatusEvent{}
		if err := proto.Unmarshal(env.Payload, qs); err == nil {
			b.LastQuestStatus = qs
		}
	}
	return env, nil
}

// DrainQuests returns and clears accumulated QuestEvents (M5).
func (b *WorldBot) DrainQuests() []*aet.QuestEvent {
	out := b.Quests
	b.Quests = nil
	return out
}

// DrainDialogs returns and clears accumulated NpcDialogEvents (M5).
func (b *WorldBot) DrainDialogs() []*aet.NpcDialogEvent {
	out := b.Dialogs
	b.Dialogs = nil
	return out
}

// FindNPC returns the nearest NPC entity in AOI by def id (M5).
func (b *WorldBot) FindNPC(npcID string) *aet.EntityState {
	var best *aet.EntityState
	var bestDist float64
	for _, e := range b.Seen {
		if e.EntityType != "npc" || e.RefId != npcID {
			continue
		}
		dx := float64(e.Position.X) - b.PosX
		dz := float64(e.Position.Z) - b.PosZ
		d := dx*dx + dz*dz
		if best == nil || d < bestDist {
			best, bestDist = e, d
		}
	}
	return best
}

// FindMobByRef returns the nearest living mob with the given def ref id (M5).
func (b *WorldBot) FindMobByRef(refID string) *aet.EntityState {
	var best *aet.EntityState
	var bestDist float64
	for _, e := range b.Seen {
		if e.EntityType != "mob" || e.Hp <= 0 || e.RefId != refID {
			continue
		}
		dx := float64(e.Position.X) - b.PosX
		dz := float64(e.Position.Z) - b.PosZ
		d := dx*dx + dz*dz
		if best == nil || d < bestDist {
			best, bestDist = e, d
		}
	}
	return best
}

// DrainLoot returns and clears accumulated LootEvents (M4).
func (b *WorldBot) DrainLoot() []*aet.LootEvent {
	out := b.Loot
	b.Loot = nil
	return out
}

// FindDrop returns the nearest living `drop` entity in Seen (M4).
func (b *WorldBot) FindDrop() *aet.EntityState {
	var best *aet.EntityState
	var bestDist float64
	for _, e := range b.Seen {
		if e.EntityType != "drop" {
			continue
		}
		dx := float64(e.Position.X) - b.PosX
		dz := float64(e.Position.Z) - b.PosZ
		d := dx*dx + dz*dz
		if best == nil || d < bestDist {
			best, bestDist = e, d
		}
	}
	return best
}

// ReadSnapshot blocks until the next WorldSnapshot envelope arrives and
// updates b.Seen/Despawned/positions. Returns false on connection error.
// Non-snapshot frames (Pong, CombatEvent, RespawnAck, chat) are drained
// without stopping the wait for the next snapshot.
func (b *WorldBot) ReadSnapshot(ctx context.Context) bool {
	ok, _ := b.ReadSnapshotErr(ctx)
	return ok
}

// ReadSnapshotErr is ReadSnapshot but also returns the underlying error.
func (b *WorldBot) ReadSnapshotErr(ctx context.Context) (bool, error) {
	for {
		env, err := b.ReadFrame(ctx)
		if err != nil {
			return false, err
		}
		if env.PayloadType == "aetheria.WorldSnapshot" {
			return true, nil
		}
	}
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
	if err := b.write(ctx, ping); err != nil {
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
	_ = b.write(ctx, lw)
	b.conn.Close(websocket.StatusNormalClosure, "done")
}

// RawWrite writes a raw frame (for the chaos fuzzer).
func (b *WorldBot) RawWrite(ctx context.Context, frame []byte) error {
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	return b.conn.Write(ctx, websocket.MessageBinary, frame)
}

// FindEntity returns the last EntityState seen for an entity id.
func (b *WorldBot) FindEntity(id uint64) *aet.EntityState { return b.Seen[id] }

// SelfLevel returns the character's current level from the last snapshot.
func (b *WorldBot) SelfLevel() int32 { return b.LastSelfLevel }

// mustMarshal panics on marshal failure (frames are fixed small shapes).
func mustMarshal(m proto.Message) []byte {
	b, err := proto.Marshal(m)
	if err != nil {
		panic(err)
	}
	return b
}
