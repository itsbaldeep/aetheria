extends SceneTree

# M5 headless test: drives WorldSession's decode pipeline with synthetic
# Envelope frames (built via the same generated codecs the client sends), and
# asserts the signals it would hand the World scene carry correct values —
# float positions, MP/XP, quest states, combat events. Guards the client's
# snapshot/quest path in CI without a live server.
# Run: godot --headless --script res://scripts/test_world_session.gd

var _got_self: Dictionary = {}
var _got_ents: Array = []
var _got_despawns: Array = []
var _got_tick := -1
var _got_combat: Dictionary = {}
var _got_quests: Array = []

func _init() -> void:
	var failures := 0
	failures += _test_snapshot()
	failures += _test_combat()
	failures += _test_quest_status()
	if failures == 0:
		print("test_world_session: ALL PASS")
		quit(0)
	else:
		printerr("test_world_session: %d FAILURES" % failures)
		quit(1)

func _env(payload_type: String, payload: PackedByteArray) -> PackedByteArray:
	var env := Envelope.new()
	env.seq = 1
	env.kind = 2
	env.payload_type = payload_type
	env.payload = payload
	return env.encode()

func _vec3(pos: Vector3) -> PackedByteArray:
	var v := Vec3.new()
	v.x = pos.x
	v.y = pos.y
	v.z = pos.z
	return v.encode()

func _es(eid: int, etype: String, pos: Vector3, hp: int, maxhp: int) -> PackedByteArray:
	var es := EntityState.new()
	es.entity_id = eid
	es.entity_type = etype
	es.name = "Test Mook"
	es.position = _vec3(pos)
	es.rot_y = 0.5
	es.speed = 3.0
	es.hp = hp
	es.max_hp = maxhp
	es.level = 2
	return es.encode()

func _test_snapshot() -> int:
	var ws := WorldSession.new()
	ws.snapshot.connect(_on_snap)
	ws.self_id = 7

	var snap := WorldSnapshot.new()
	snap.tick = 42
	snap.self_id = 7
	var self_es := EntityState.new()
	self_es.entity_id = 7
	self_es.entity_type = "player"
	self_es.name = "Kestrel"
	self_es.position = _vec3(Vector3(10.5, 0, -4.25))
	self_es.hp = 80
	self_es.max_hp = 100
	self_es.level = 3
	self_es.mp = 40
	self_es.max_mp = 60
	self_es.xp = 25
	self_es.xp_for_level = 120
	snap._self.append(self_es.encode())
	snap.entities.append(_es(9, "mob", Vector3(14, 0, -6), 50, 50))
	snap.entities.append(_es(11, "npc", Vector3(3, 0, 2), 0, 0))

	ws._handle_frame(_env("aetheria.WorldSnapshot", snap.encode()))

	if _got_tick != 42:
		printerr("snapshot tick %d != 42" % _got_tick)
		return 1
	if absf(float(_got_self.get("position", Vector3.ZERO).x) - 10.5) > 1e-5 \
			or absf(float(_got_self.get("position", Vector3.ZERO).z) + 4.25) > 1e-5:
		printerr("self position decode wrong: %s" % _got_self.get("position"))
		return 1
	if int(_got_self.get("mp", 0)) != 40 or int(_got_self.get("xp", 0)) != 25 \
			or int(_got_self.get("xp_for_level", 0)) != 120:
		printerr("self mp/xp decode wrong: %s" % _got_self)
		return 1
	if _got_ents.size() != 2:
		printerr("entities size %d != 2" % _got_ents.size())
		return 1
	if not ws.entities.has(9) or not ws.entities.has(11):
		printerr("entities 9/11 missing after first snapshot")
		return 1
	if ws.self_state.get("mp", 0) != 40:
		printerr("world self_state mp not updated: %s" % ws.self_state)
		return 1

	# Second snapshot: entity 9 leaves the AOI → must be despawned.
	var snap2 := WorldSnapshot.new()
	snap2.tick = 43
	snap2.self_id = 7
	snap2.despawn_ids = [9]
	ws._handle_frame(_env("aetheria.WorldSnapshot", snap2.encode()))
	if ws.entities.has(9):
		printerr("despawn did not remove entity 9")
		return 1
	if not ws.entities.has(11):
		printerr("despawn removed unrelated entity 11")
		return 1
	return 0

func _on_snap(tick: int, st: Array, ents: Array, despawns: Array) -> void:
	_got_tick = tick
	if not st.is_empty():
		_got_self = st[0]
	_got_ents = ents
	_got_despawns = despawns

func _test_combat() -> int:
	var ws := WorldSession.new()
	ws.combat_event.connect(func(ev): _got_combat = ev)
	var ce := CombatEvent.new()
	ce.event_type = "crit"
	ce.source_id = 7
	ce.target_id = 9
	ce.amount = 34
	ce.message = "Kestrel crits the Test Mook for 34"
	ws._handle_frame(_env("aetheria.CombatEvent", ce.encode()))
	if str(_got_combat.get("event_type", "")) != "crit" or int(_got_combat.get("amount", 0)) != 34:
		printerr("combat event decode wrong: %s" % _got_combat)
		return 1
	return 0

func _test_quest_status() -> int:
	var ws := WorldSession.new()
	ws.quest_status.connect(func(ev): _got_quests = ev.get("quests", []))
	var qs := QuestState.new()
	qs.quest_id = "q_welcome"
	qs.name = "A Warm Welcome"
	qs.state = "active"
	qs.turnin_npc = "aldric_questgiver"
	var obj := QuestObjectiveState.new()
	obj.type = "kill"
	obj.target = "forest_boar"
	obj.target_name = "Forest Boar"
	obj.current = 1
	obj.required = 3
	qs.objectives.append(obj.encode())
	var ev := QuestStatusEvent.new()
	ev.quests = [qs.encode()]
	ws._handle_frame(_env("aetheria.QuestStatusEvent", ev.encode()))
	if _got_quests.size() != 1:
		printerr("quest status size %d != 1" % _got_quests.size())
		return 1
	var q: Dictionary = _got_quests[0]
	if str(q.get("quest_id", "")) != "q_welcome" or str(q.get("state", "")) != "active":
		printerr("quest state decode wrong: %s" % q)
		return 1
	var objs: Array = q.get("objectives", [])
	if objs.size() != 1:
		printerr("objectives size %d != 1" % objs.size())
		return 1
	var o: Dictionary = objs[0]
	if int(o.get("current", 0)) != 1 or int(o.get("required", 0)) != 3:
		printerr("objective decode wrong: %s" % o)
		return 1
	return 0