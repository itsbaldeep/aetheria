// Package wire implements the gameserver's WebSocket protocol layer:
// accepts connections, frames Envelope messages, dispatches by payload type,
// and bridges authenticated connections into the world simulation (M2).
package wire

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	aet "github.com/itsbaldeep/aetheria/server/gen"
	"github.com/itsbaldeep/aetheria/server/internal/auth"
	"github.com/itsbaldeep/aetheria/server/internal/platform"
	"github.com/itsbaldeep/aetheria/server/internal/world"
)

// SessionValidator checks a handshake token and returns the account id.
// Satisfied by auth.Guard.
type SessionValidator interface {
	Validate(ctx context.Context, token string) (int64, error)
}

// CharacterLoader loads a character's spawn state for EnterWorld.
type CharacterLoader interface {
	// LoadCharacter returns the character's spawn state if it belongs to the
	// account. nil, nil means "not found".
	LoadCharacter(ctx context.Context, accountID, charID int64) (*CharacterSpawn, error)
}

// ItemLoader loads a character's persisted item instances at EnterWorld (M4).
// Optional: when absent, players enter with an empty inventory.
type ItemLoader interface {
	LoadItems(ctx context.Context, charID int64) ([]world.Item, error)
}

// ItemSaver persists a character's item instances (M4). Optional: when
// absent, items are only held in memory for the session.
type ItemSaver interface {
	SaveItems(ctx context.Context, charID int64, items []world.Item) error
}

// CharacterSpawn is the subset of character data the world needs to spawn.
type CharacterSpawn struct {
	ID     int64
	Name   string
	Class  string
	ZoneID string
	Pos    world.Vec3
	Level  int32
	HP     int64
	MaxHP  int64
	MP     int64
	MaxMP  int64
	XP     int64
	Gold   int64
}

// connState is per-connection mutable state, owned by the HandleWS goroutine
// (the writer goroutine only reads outbox, which is fixed after creation).
type connState struct {
	conn      *websocket.Conn
	accountID int64
	outbox    chan []byte   // fixed for the connection lifetime
	session   *world.Player // nil until EnterWorld succeeds
}

// Hub owns all live connections and routes envelopes to handlers.
type Hub struct {
	s   *platform.Service
	v   SessionValidator
	cl  CharacterLoader
	sim *world.Sim

	maxSpeeds map[string]float64
}

// NewHub builds the gameserver hub. sim may be nil (M0/M1 tests).
func NewHub(s *platform.Service, v SessionValidator, cl CharacterLoader, sim *world.Sim) *Hub {
	return &Hub{
		s:   s,
		v:   v,
		cl:  cl,
		sim: sim,
		maxSpeeds: map[string]float64{
			auth.ClassBladeDancer: 8.0,
			auth.ClassSpellweaver: 7.0,
		},
	}
}

// Run is a placeholder lifecycle loop (future: connection registry, sessions).
func (h *Hub) Run() {}

// HandleWS upgrades an HTTP request to a WebSocket connection, validates the
// session token from the handshake, then sends ServerHello and dispatches.
func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		h.s.Log("warn", "ws accept failed", "error", err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "bye")
	remote := r.RemoteAddr

	accountID, err := h.validateHandshake(r)
	if err != nil {
		reason := "unauthorized"
		switch {
		case errors.Is(err, auth.ErrSessionInvalid):
			reason = "invalid_session"
		case errors.Is(err, auth.ErrAccountBanned):
			reason = "account_banned"
		}
		h.s.Log("info", "ws auth rejected", "remote", remote, "reason", reason)
		conn.Close(websocket.StatusPolicyViolation, reason)
		return
	}
	h.s.Log("info", "client connected", "remote", remote, "account_id", accountID)

	st := &connState{conn: conn, accountID: accountID}
	if h.sim != nil {
		st.outbox = h.sim.NewPlayerOutbox()
	} else {
		st.outbox = make(chan []byte, 64)
	}
	defer func() {
		if st.session != nil {
			h.leaveWorld(st.session)
		}
	}()

	// Writer goroutine: the single writer on this socket. It drains the
	// connection's outbox channel; every outgoing frame (hello, pong, ack,
	// snapshots) goes through this channel so there is never a concurrent
	// write on the coder/websocket Conn.
	writeErr := make(chan error, 1)
	stopWriter := make(chan struct{})
	outbox := st.outbox
	go func() {
		for {
			select {
			case <-stopWriter:
				return
			case frame := <-outbox:
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				err := conn.Write(ctx, websocket.MessageBinary, frame)
				cancel()
				if err != nil {
					h.s.Log("warn", "socket write failed",
						"remote", remote, "account_id", accountID, "error", err.Error())
					select {
					case writeErr <- err:
					default:
					}
					return
				}
			}
		}
	}()

	// Greeting after authentication (M0/M1 protocol handshake).
	hello := &aet.ServerHello{
		ProtocolVersion: "0.1.0",
		GameName:        "Aetheria",
		TickRateHz:      20,
	}
	if err := h.enqueue(st, hello); err != nil {
		h.s.Log("warn", "hello send failed", "remote", remote, "error", err)
		close(stopWriter)
		return
	}

	// Read loop.
	for {
		env, err := h.receive(conn)
		if err != nil {
			charID := ""
			if st.session != nil {
				charID = strconv.FormatInt(st.session.CharacterID, 10)
			}
			h.s.Log("info", "conn read loop end",
				"remote", remote, "account_id", accountID, "char_id", charID, "reason", err.Error())
			break
		}
		done := h.dispatch(r.Context(), st, env)
		if done {
			close(stopWriter)
			return
		}
	}

	close(stopWriter)
	select {
	case <-writeErr:
	default:
	}
}

// leaveWorld removes a player from the sim and saves the final position.
func (h *Hub) leaveWorld(p *world.Player) {
	if h.sim == nil {
		return
	}
	if h.sim.SavePos != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = h.sim.SavePos(ctx, p.CharacterID, p.Pos)
		cancel()
	}
	// Persist items + flush the gold ledger on disconnect (M4).
	if is, ok := h.cl.(ItemSaver); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = is.SaveItems(ctx, p.CharacterID, h.sim.PersistItems(p.CharacterID))
		cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	h.sim.FlushGoldLedger(ctx)
	cancel()
	h.sim.Despawn(p.CharacterID)
	h.s.Log("info", "player left world", "char_id", p.CharacterID)
}

// validateHandshake extracts the Bearer token from the WS handshake request
// and validates it with the session guard.
func (h *Hub) validateHandshake(r *http.Request) (int64, error) {
	if h.v == nil {
		return 0, nil
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" || token == r.Header.Get("Authorization") {
		return 0, auth.ErrSessionInvalid
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	return h.v.Validate(ctx, token)
}

// enqueue marshals a message and pushes it into the connection's outbox for
// the single writer goroutine. It blocks up to 10 s for backpressure rather
// than dropping a pong/ack (snapshots in the sim drop on overflow instead).
func (h *Hub) enqueue(st *connState, msg proto.Message) error {
	b, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	t := time.NewTimer(10 * time.Second)
	defer t.Stop()
	select {
	case st.outbox <- b:
		return nil
	case <-t.C:
		return errors.New("outbox send timeout")
	}
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

// dispatch routes an envelope. Returns true when the connection should close
// (explicit LeaveWorld or fatal error).
func (h *Hub) dispatch(ctx context.Context, st *connState, env *aet.Envelope) bool {
	switch env.PayloadType {
	case "aetheria.Ping":
		p := &aet.Ping{}
		if err := proto.Unmarshal(env.Payload, p); err != nil {
			h.s.Log("warn", "bad ping payload", "error", err)
			return false
		}
		pong := &aet.Pong{
			SentAtUnixMs:     p.SentAtUnixMs,
			ServerTimeUnixMs: time.Now().UnixMilli(),
		}
		if err := h.enqueue(st, pong); err != nil {
			h.s.Log("warn", "pong send failed", "error", err)
		}
		return false
	case "aetheria.EnterWorld":
		return h.handleEnterWorld(ctx, st, env)
	case "aetheria.MoveIntent":
		if st.session == nil {
			h.s.Log("warn", "move before enter world")
			return false
		}
		m := &aet.MoveIntent{}
		if err := proto.Unmarshal(env.Payload, m); err != nil {
			h.s.Log("warn", "bad move payload", "error", err)
			return false
		}
		h.handleMove(st.session, m)
		return false
	case "aetheria.CastSkill":
		if st.session == nil {
			h.s.Log("warn", "cast before enter world")
			return false
		}
		cs := &aet.CastSkill{}
		if err := proto.Unmarshal(env.Payload, cs); err != nil {
			h.s.Log("warn", "bad cast payload", "error", err)
			return false
		}
		req := world.CastRequest{SkillID: cs.SkillId, TargetID: cs.TargetEntityId}
		if cs.AimPosition != nil {
			ap := world.Vec3{X: float64(cs.AimPosition.X), Y: float64(cs.AimPosition.Y), Z: float64(cs.AimPosition.Z)}
			req.AimPos = &ap
		}
		if err := h.sim.CastSkill(st.session.CharacterID, req); err != nil {
			h.s.Log("info", "cast rejected", "char_id", st.session.CharacterID, "skill", cs.SkillId, "error", err)
		}
		return false
	case "aetheria.AutoAttack":
		if st.session == nil {
			h.s.Log("warn", "autoattack before enter world")
			return false
		}
		aa := &aet.AutoAttack{}
		if err := proto.Unmarshal(env.Payload, aa); err != nil {
			h.s.Log("warn", "bad autoattack payload", "error", err)
			return false
		}
		if err := h.sim.SetAutoAttack(st.session.CharacterID, aa.TargetEntityId, aa.Active); err != nil {
			h.s.Log("warn", "autoattack rejected", "char_id", st.session.CharacterID, "error", err)
		}
		return false
	case "aetheria.ChatMessage":
		if st.session == nil {
			h.s.Log("warn", "chat before enter world")
			return false
		}
		cm := &aet.ChatMessage{}
		if err := proto.Unmarshal(env.Payload, cm); err != nil {
			h.s.Log("warn", "bad chat payload", "error", err)
			return false
		}
		if err := h.sim.SendChat(st.session.CharacterID, cm.Channel, cm.Text); err != nil {
			h.s.Log("info", "chat rejected", "char_id", st.session.CharacterID, "channel", cm.Channel, "error", err)
		}
		return false
	case "aetheria.RespawnRequest":
		if st.session == nil {
			h.s.Log("warn", "respawn before enter world")
			return false
		}
		if err := h.sim.RespawnPlayer(st.session.CharacterID); err != nil {
			h.s.Log("info", "respawn rejected", "char_id", st.session.CharacterID, "error", err)
			h.enqueue(st, &aet.RespawnAck{Ok: false, Error: err.Error()})
			return false
		}
		pos := h.sim.PlayerPos(st.session.CharacterID)
		h.enqueue(st, &aet.RespawnAck{
			Ok:       true,
			ZoneId:   st.session.Zone,
			Position: &aet.Vec3{X: float32(pos.X), Y: float32(pos.Y), Z: float32(pos.Z)},
		})
		return false
	case "aetheria.PickupItem":
		if st.session == nil {
			h.s.Log("warn", "pickup before enter world")
			return false
		}
		pi := &aet.PickupItem{}
		if err := proto.Unmarshal(env.Payload, pi); err != nil {
			h.s.Log("warn", "bad pickup payload", "error", err)
			return false
		}
		if err := h.sim.PickupItem(st.session.CharacterID, pi.DropEntityId); err != nil {
			h.s.Log("info", "pickup rejected", "char_id", st.session.CharacterID, "drop", pi.DropEntityId, "error", err)
			h.sendLootError(st, pi.DropEntityId, err)
		}
		return false
	case "aetheria.EquipItem":
		if st.session == nil {
			h.s.Log("warn", "equip before enter world")
			return false
		}
		ei := &aet.EquipItem{}
		if err := proto.Unmarshal(env.Payload, ei); err != nil {
			h.s.Log("warn", "bad equip payload", "error", err)
			return false
		}
		if err := h.sim.EquipItem(st.session.CharacterID, ei.ItemId); err != nil {
			h.s.Log("info", "equip rejected", "char_id", st.session.CharacterID, "item", ei.ItemId, "error", err)
			h.sendLootError(st, ei.ItemId, err)
		}
		return false
	case "aetheria.UnequipItem":
		if st.session == nil {
			h.s.Log("warn", "unequip before enter world")
			return false
		}
		ui := &aet.UnequipItem{}
		if err := proto.Unmarshal(env.Payload, ui); err != nil {
			h.s.Log("warn", "bad unequip payload", "error", err)
			return false
		}
		if err := h.sim.UnequipItem(st.session.CharacterID, ui.Slot); err != nil {
			h.s.Log("info", "unequip rejected", "char_id", st.session.CharacterID, "slot", ui.Slot, "error", err)
			h.sendLootError(st, 0, err)
		}
		return false
	case "aetheria.SellItem":
		if st.session == nil {
			h.s.Log("warn", "sell before enter world")
			return false
		}
		si := &aet.SellItem{}
		if err := proto.Unmarshal(env.Payload, si); err != nil {
			h.s.Log("warn", "bad sell payload", "error", err)
			return false
		}
		if err := h.sim.SellItem(st.session.CharacterID, si.ItemId, si.Quantity); err != nil {
			h.s.Log("info", "sell rejected", "char_id", st.session.CharacterID, "item", si.ItemId, "error", err)
			h.sendLootError(st, si.ItemId, err)
		}
		return false
	case "aetheria.BuyItem":
		if st.session == nil {
			h.s.Log("warn", "buy before enter world")
			return false
		}
		bi := &aet.BuyItem{}
		if err := proto.Unmarshal(env.Payload, bi); err != nil {
			h.s.Log("warn", "bad buy payload", "error", err)
			return false
		}
		if err := h.sim.BuyItem(st.session.CharacterID, bi.VendorId, bi.ItemDefId, bi.Quantity); err != nil {
			h.s.Log("info", "buy rejected", "char_id", st.session.CharacterID, "vendor", bi.VendorId, "item", bi.ItemDefId, "error", err)
			h.sendLootError(st, 0, err)
		}
		return false
	case "aetheria.LeaveWorld":
		return true
	default:
		h.s.Log("warn", "unknown payload type", "type", env.PayloadType)
	}
	return false
}

// sendLootError relays a rejected economy mutation to the client as a
// LootEvent with ok=false and the rejection reason (proto contract §M4).
func (h *Hub) sendLootError(st *connState, itemID uint64, err error) {
	if st.session == nil || st.session.Outbox == nil {
		return
	}
	payload, perr := proto.Marshal(&aet.LootEvent{Ok: false, Error: err.Error(), ItemId: itemID})
	if perr != nil {
		return
	}
	frame, merr := proto.Marshal(&aet.Envelope{
		Kind:        aet.Envelope_KIND_EVENT,
		PayloadType: "aetheria.LootEvent",
		Payload:     payload,
	})
	if merr != nil {
		return
	}
	select {
	case st.session.Outbox <- frame:
	default:
	}
}

// handleEnterWorld validates the character, spawns it in the sim, and replies
// EnterWorldAck. On success st.session is set; its outbox feeds the writer.
func (h *Hub) handleEnterWorld(ctx context.Context, st *connState, env *aet.Envelope) bool {
	req := &aet.EnterWorld{}
	if err := proto.Unmarshal(env.Payload, req); err != nil {
		h.s.Log("warn", "bad enter payload", "error", err)
		h.enqueue(st, &aet.EnterWorldAck{Ok: false, Error: "bad_request"})
		return false
	}
	if h.sim == nil || h.cl == nil {
		h.enqueue(st, &aet.EnterWorldAck{Ok: false, Error: "world_unavailable"})
		return false
	}

	spawn, err := h.cl.LoadCharacter(ctx, st.accountID, req.CharacterId)
	if err != nil {
		h.s.Log("warn", "enter world load failed", "error", err)
		h.enqueue(st, &aet.EnterWorldAck{Ok: false, Error: "load_failed"})
		return false
	}
	if spawn == nil {
		h.enqueue(st, &aet.EnterWorldAck{Ok: false, Error: "character_not_found"})
		return false
	}

	maxSpeed := h.maxSpeeds[spawn.Class]
	if maxSpeed == 0 {
		maxSpeed = 8
	}
	p := &world.Player{
		Entity: world.Entity{
			Type:  world.TypePlayer,
			Name:  spawn.Name,
			Zone:  spawn.ZoneID,
			Pos:   spawn.Pos,
			HP:    spawn.HP,
			MaxHP: spawn.MaxHP,
			Level: spawn.Level,
		},
		AccountID:   st.accountID,
		CharacterID: spawn.ID,
		Class:       spawn.Class,
		MaxSpeed:    maxSpeed,
		MP:          spawn.MP,
		MaxMP:       spawn.MaxMP,
		XP:          spawn.XP,
		Gold:        spawn.Gold,
		Outbox:      st.outbox,
	}
	if err := h.sim.Spawn(p); err != nil {
		h.enqueue(st, &aet.EnterWorldAck{Ok: false, Error: "already_in_world"})
		return false
	}
	// Restore persisted items (M4). Failure is non-fatal: log + continue with
	// whatever loaded.
	if il, ok := h.cl.(ItemLoader); ok {
		items, err := il.LoadItems(ctx, spawn.ID)
		if err != nil {
			h.s.Log("warn", "item load failed", "char_id", spawn.ID, "error", err)
		} else if err := h.sim.RestoreItems(spawn.ID, items); err != nil {
			h.s.Log("warn", "item restore failed", "char_id", spawn.ID, "error", err)
		}
	}
	st.session = p
	h.s.Log("info", "player entered world", "char_id", spawn.ID, "zone", spawn.ZoneID)
	h.enqueue(st, &aet.EnterWorldAck{
		Ok:       true,
		EntityId: p.ID,
		ZoneId:   spawn.ZoneID,
		Position: &aet.Vec3{X: float32(p.Pos.X), Y: float32(p.Pos.Y), Z: float32(p.Pos.Z)},
		MaxSpeed: float32(maxSpeed),
	})
	// Ack is enqueued; unblock snapshot emission.
	p.Ready.Store(true)
	return false
}

// handleMove forwards a validated MoveIntent into the sim.
func (h *Hub) handleMove(p *world.Player, m *aet.MoveIntent) {
	wi := world.MoveIntent{
		Speed: float64(m.Speed),
		RotY:  float64(m.RotY),
	}
	if m.Target != nil {
		t := world.Vec3{X: float64(m.Target.X), Y: float64(m.Target.Y), Z: float64(m.Target.Z)}
		wi.Target = &t
	}
	if m.Direction != nil {
		wi.Direction = world.Vec3{X: float64(m.Direction.X), Y: float64(m.Direction.Y), Z: float64(m.Direction.Z)}
	}
	if err := h.sim.SetMove(p.CharacterID, wi); err != nil {
		h.s.Log("warn", "move rejected", "char_id", p.CharacterID, "error", err)
	}
}
