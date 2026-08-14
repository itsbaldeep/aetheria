class_name WorldSession
extends Node

# Aetheria game-session client (brief §7 headless-testability). Owns the
# WebSocket, the Envelope frame layer, and the entity/quest state the server
# streams. Purely transport + decode — no rendering nodes, so a headless test
# can drive it against a live gameserver. The World scene subscribes to the
# signals and maps state to visuals.

signal server_hello(hello: Dictionary)
signal entered_world(ack: Dictionary)
signal disconnected(reason: String, code: int)
signal snapshot(tick: int, self_states: Array, entities: Array, despawns: Array)
signal combat_event(ev: Dictionary)
signal chat_message(msg: Dictionary)
signal loot_event(ev: Dictionary)
signal npc_dialog(ev: Dictionary)
signal quest_event(ev: Dictionary)
signal quest_status(ev: Dictionary)
signal respawn_ack(ev: Dictionary)
signal pong(ms: int)

const KIND_REQUEST: int = 1
const KIND_EVENT: int = 2

var ws_url := ""
var token := ""
var char_id := 0

var ws: WebSocketPeer
var connected := false
var in_world := false
var hello: Dictionary = {}
var self_id := 0
var zone_id := ""
var max_speed := 8.0
var spawn_pos := Vector3.ZERO

# entity_id -> Dictionary of EntityState fields (position/rot normalized).
var entities: Dictionary = {}
var self_state: Dictionary = {}

var _seq := 0
var _ping_ms := 0
var _ping_sent_at := 0

func connect_to_server(url: String, bearer: String, character_id: int) -> void:
	ws_url = url
	token = bearer
	char_id = character_id
	ws = WebSocketPeer.new()
	ws.supported_protocols = PackedStringArray(["binary"])
	ws.set_handshake_headers(PackedStringArray(["Authorization: Bearer " + token]))
	ws.connect_to_url(url)

func _process(_delta: float) -> void:
	if ws == null:
		return
	ws.poll()
	match ws.get_ready_state():
		WebSocketPeer.STATE_OPEN:
			connected = true
			while ws.get_available_packet_count() > 0:
				var packet: PackedByteArray = ws.get_packet()
				_handle_frame(packet)
		WebSocketPeer.STATE_CLOSED:
			if connected or in_world or not hello.is_empty():
				connected = false
				var code := ws.get_close_code()
				var reason := "connection closed"
				if code != 1000:
					reason = _close_reason(code)
				emit_signal("disconnected", reason, code)
				ws = null

func _close_reason(code: int) -> String:
	if code == 1008:
		return "session rejected by server (invalid or expired)"
	if code == 1002:
		return "protocol violation"
	return "connection closed (%d)" % code

# --- outbound ---

func send_message(type_name: String, msg) -> bool:
	if ws == null or ws.get_ready_state() != WebSocketPeer.STATE_OPEN:
		return false
	_seq += 1
	var env := Envelope.new()
	env.seq = _seq
	env.kind = KIND_REQUEST
	env.payload_type = type_name
	env.payload = msg.encode()
	var err := ws.send(env.encode())
	return err == OK

func enter_world() -> void:
	var m := EnterWorld.new()
	m.character_id = char_id
	if send_message("aetheria.EnterWorld", m):
		in_world = true

func move_intent(dir: Vector3, speed: float, rot_y: float) -> void:
	var m := MoveIntent.new()
	var v := Vec3.new()
	v.x = dir.x
	v.y = dir.y
	v.z = dir.z
	m.direction = v.encode()
	m.speed = speed
	m.rot_y = rot_y
	send_message("aetheria.MoveIntent", m)

func stop() -> void:
	var m := MoveIntent.new()
	m.direction = Vec3.new().encode()
	m.speed = 0.0
	send_message("aetheria.MoveIntent", m)

func auto_attack(target_id: int, active: bool) -> void:
	var m := AutoAttack.new()
	m.target_entity_id = target_id
	m.active = active
	send_message("aetheria.AutoAttack", m)

func cast_skill(skill_id: String, target_id: int) -> void:
	var m := CastSkill.new()
	m.skill_id = skill_id
	m.target_entity_id = target_id
	send_message("aetheria.CastSkill", m)

func chat(channel: String, text: String) -> void:
	var m := ChatMessage.new()
	m.channel = channel
	m.text = text
	send_message("aetheria.ChatMessage", m)

func pickup(drop_id: int) -> void:
	var m := PickupItem.new()
	m.drop_entity_id = drop_id
	send_message("aetheria.PickupItem", m)

func npc_interact(npc_id: String) -> void:
	var m := NpcInteract.new()
	m.npc_id = npc_id
	send_message("aetheria.NpcInteract", m)

func quest_accept(quest_id: String) -> void:
	var m := QuestAccept.new()
	m.quest_id = quest_id
	send_message("aetheria.QuestAccept", m)

func quest_abandon(quest_id: String) -> void:
	var m := QuestAbandon.new()
	m.quest_id = quest_id
	send_message("aetheria.QuestAbandon", m)

func quest_turnin(quest_id: String) -> void:
	var m := QuestTurnIn.new()
	m.quest_id = quest_id
	send_message("aetheria.QuestTurnIn", m)

func quest_status_request() -> void:
	send_message("aetheria.QuestStatus", QuestStatus.new())

func respawn_request() -> void:
	send_message("aetheria.RespawnRequest", RespawnRequest.new())

func ping() -> void:
	var m := Ping.new()
	_ping_sent_at = Time.get_ticks_msec()
	m.sent_at_unix_ms = _ping_sent_at
	send_message("aetheria.Ping", m)

func leave_world() -> void:
	send_message("aetheria.LeaveWorld", LeaveWorld.new())
	if ws != null:
		ws.close()

# --- inbound ---

func _handle_frame(data: PackedByteArray) -> void:
	var env := Envelope.decode(data)
	match env.payload_type:
		"aetheria.ServerHello":
			var m := ServerHello.decode(env.payload)
			hello = {"protocol_version": m.protocol_version, "game_name": m.game_name, "tick_rate_hz": m.tick_rate_hz}
			emit_signal("server_hello", hello)
			enter_world()
		"aetheria.EnterWorldAck":
			var m := EnterWorldAck.decode(env.payload)
			var ack := {
				"ok": m.ok, "error": m.error, "entity_id": m.entity_id,
				"zone_id": m.zone_id, "position": _vec3(m.position), "max_speed": m.max_speed,
			}
			if m.ok:
				self_id = m.entity_id
				zone_id = m.zone_id
				max_speed = m.max_speed
				spawn_pos = ack["position"]
				entities.clear()
				self_state = {}
			else:
				in_world = false
				emit_signal("disconnected", ack["error"], 1000)
				return
			emit_signal("entered_world", ack)
		"aetheria.WorldSnapshot":
			var m := WorldSnapshot.decode(env.payload)
			var self_entries: Array = []
			var ent_entries: Array = []
			for raw in m._self:
				var es := EntityState.decode(raw)
				self_entries.append(_entity_dict(es))
			for raw in m.entities:
				var es := EntityState.decode(raw)
				ent_entries.append(_entity_dict(es))
			_apply_state(self_entries, ent_entries, m.despawn_ids)
			emit_signal("snapshot", m.tick, self_entries, ent_entries, m.despawn_ids)
		"aetheria.CombatEvent":
			var m := CombatEvent.decode(env.payload)
			emit_signal("combat_event", _combat_dict(m))
		"aetheria.ChatMessage":
			var m := ChatMessage.decode(env.payload)
			emit_signal("chat_message", {
				"channel": m.channel, "text": m.text, "sender_id": m.sender_id,
				"sender_name": m.sender_name, "sent_at": m.sent_at_unix_ms,
			})
		"aetheria.LootEvent":
			var m := LootEvent.decode(env.payload)
			emit_signal("loot_event", {
				"ok": m.ok, "error": m.error, "item_id": m.item_id,
				"item_def_id": m.item_def_id, "quantity": m.quantity,
				"gold": m.gold, "balance": m.balance,
			})
		"aetheria.NpcDialogEvent":
			var m := NpcDialogEvent.decode(env.payload)
			emit_signal("npc_dialog", {
				"ok": m.ok, "error": m.error, "npc_id": m.npc_id, "npc_name": m.npc_name,
				"dialog": m.dialog, "available": m.available_quests, "turnin": m.turnin_quests,
			})
		"aetheria.QuestEvent":
			var m := QuestEvent.decode(env.payload)
			emit_signal("quest_event", {
				"ok": m.ok, "error": m.error, "quest_id": m.quest_id, "name": m.name,
				"state": m.state, "turnin_npc": m.turnin_npc,
				"objectives": _objectives(m.objectives), "xp": m.xp_reward,
				"gold": m.gold_reward, "new_level": m.new_level,
			})
		"aetheria.QuestStatusEvent":
			var m := QuestStatusEvent.decode(env.payload)
			var quests: Array = []
			for raw in m.quests:
				var qs := QuestState.decode(raw)
				quests.append({
					"quest_id": qs.quest_id, "name": qs.name, "state": qs.state,
					"turnin_npc": qs.turnin_npc, "objectives": _objectives(qs.objectives),
				})
			emit_signal("quest_status", {"quests": quests, "error": m.error})
		"aetheria.RespawnAck":
			var m := RespawnAck.decode(env.payload)
			emit_signal("respawn_ack", {
				"ok": m.ok, "error": m.error, "zone_id": m.zone_id, "position": _vec3(m.position),
			})
		"aetheria.Pong":
			var m := Pong.decode(env.payload)
			_ping_ms = Time.get_ticks_msec() - _ping_sent_at
			emit_signal("pong", _ping_ms)
		_:
			push_warning("world_session: unhandled payload %s" % env.payload_type)

func _apply_state(self_entries: Array, ent_entries: Array, despawns: Array) -> void:
	if not self_entries.is_empty():
		self_state = self_entries[0]
	for d in despawns:
		entities.erase(int(d))
	for e in ent_entries:
		entities[int(e["entity_id"])] = e

func _entity_dict(es) -> Dictionary:
	return {
		"entity_id": es.entity_id, "entity_type": es.entity_type, "name": es.name,
		"zone_id": es.zone_id, "position": _vec3(es.position), "rot_y": es.rot_y,
		"speed": es.speed, "hp": es.hp, "max_hp": es.max_hp, "level": es.level,
		"is_moving": es.is_moving, "ref_id": es.ref_id,
		"mp": es.mp, "max_mp": es.max_mp, "xp": es.xp, "xp_for_level": es.xp_for_level,
	}

func _combat_dict(m) -> Dictionary:
	return {
		"event_type": m.event_type, "source_id": m.source_id, "target_id": m.target_id,
		"skill_id": m.skill_id, "amount": m.amount, "message": m.message, "new_level": m.new_level,
	}

func _objectives(raw_list: Array) -> Array:
	var out: Array = []
	for raw in raw_list:
		var o := QuestObjectiveState.decode(raw)
		out.append({
			"type": o.type, "target": o.target, "target_name": o.target_name,
			"current": o.current, "required": o.required,
		})
	return out

func _vec3(raw: PackedByteArray) -> Vector3:
	if raw.is_empty():
		return Vector3.ZERO
	var v := Vec3.decode(raw)
	return Vector3(v.x, v.y, v.z)
