extends Node3D

# Aetheria world scene: connects the WorldSession to a themed procedurally-
# built 3D world and the in-game HUD. Server-authoritative: snapshots drive
# every position; this node only turns the wire into visuals + inputs.

var session: Session
var character: Dictionary = {}
var ws_url := ""

var world: WorldSession
var hud: AetheriaHUD
var theme_src: AetheriaTheme

# player rig + camera
var rig: Node3D
var cam: Camera3D
var cam_yaw := 0.0
var cam_pitch := 0.35
var cam_dist := 9.0

# entities
var ents: Dictionary = {}
var entity_root: Node3D
var current_target := 0
var auto_attack_on := false

# quests
var quests: Dictionary = {}

const UNSET_ID := -1
var _last_rig_target := Vector3.INF
var _move_sent_at := 0
var _skill_send := {}
var _aura_t := 0.0

func _ready() -> void:
	process_mode = Node.PROCESS_MODE_ALWAYS
	theme_src = AetheriaTheme.new()

	_build_environment()
	_build_player()

	hud = AetheriaHUD.new()
	hud.name = "HUD"
	hud.theme_src = theme_src
	hud.player_class = str(character.get("class", "blade_dancer"))
	add_child(hud)
	hud.set_skill_keys()
	_wire_hud()

	world = WorldSession.new()
	add_child(world)
	_wire_world()

	var cfg := ClientConfig.load_default()
	if ws_url == "":
		ws_url = cfg.ws_url
	if session == null or not session.authenticated or character.is_empty():
		# No session (editor / headless smoke test): render the world and HUD
		# but never attempt a connection.
		hud.set_connection("Awaiting session — open the client to play.")
		return
	hud.set_connection("Connecting to Aetheria…")
	world.connect_to_server(ws_url, session.token, int(character.get("id", 0)))

	_set_mouse_capture(true)

func _exit_tree() -> void:
	if world != null:
		world.leave_world()

# --- world building ---

func _build_environment() -> void:
	var sun := DirectionalLight3D.new()
	sun.name = "EmberSun"
	sun.light_color = Color("#ffc9a3")
	sun.light_energy = 1.15
	sun.rotation_degrees = Vector3(-38, -42, 0)
	sun.shadow_enabled = true
	sun.directional_shadow_max_distance = 260
	sun.light_color = Color("#ffd9af")
	add_child(sun)

	var env := Environment.new()
	env.background_mode = Environment.BG_SKY
	var sky_mat := ProceduralSkyMaterial.new()
	sky_mat.sky_top_color = Color("#0c1030")
	sky_mat.sky_horizon_color = Color("#c05a33")
	sky_mat.ground_horizon_color = Color("#35200f")
	sky_mat.ground_bottom_color = Color("#0a0813")
	sky_mat.sun_angle_max = 18.0
	sky_mat.sun_curve = 0.12
	var sky := Sky.new()
	sky.sky_material = sky_mat
	env.sky = sky
	env.ambient_light_source = Environment.AMBIENT_SOURCE_COLOR
	env.ambient_light_color = Color("#2a3350")
	env.ambient_light_energy = 0.65
	env.fog_enabled = true
	env.fog_light_color = Color("#8a4a33")
	env.fog_light_energy = 0.9
	env.fog_density = 0.0016
	env.fog_height = 0.0
	var we := WorldEnvironment.new()
	we.environment = env
	add_child(we)

	# Terrain pieces (centered on the havenport pocket inside emberfield).
	_ground_plane(Vector3(0, -0.05, 0), Vector2(1200, 1200), Vector3(0.40, 0.28, 0.10), Vector3(0.24, 0.19, 0.14), 30.0)
	_ground_plane(Vector3(0, -0.02, 0), Vector2(120, 120), Vector3(0.20, 0.32, 0.13), Vector3(0.14, 0.24, 0.10), 40.0)

	_shrine()

	# Trees around the field.
	var seed := 1337
	for i in range(64):
		var ang := (i * 2.399963) + 0.3
		var rad := 90.0 + float(i) * 3.1
		var x := cos(ang) * rad
		var z := sin(ang) * rad
		if sqrt(x * x + z * z) < 55.0:
			z += 70.0
		_tree(Vector3(x, 0, z), 1.5 + float(int(seed + i) % 12) / 8.0, 0.7 + float(int(seed + i) % 5) / 5.0)
	for i in range(22):
		var ang := (i * 2.094)
		var rad := 96.0 + float(int(seed + i * 7) % 20)
		var x := cos(ang) * rad
		var z := sin(ang) * rad
		_rock(Vector3(x, 0, z), 0.6 + float(int(seed + i * 3) % 5) / 4.0)

	# Town houses hugging the plaza.
	_house(Vector3(22, 0, -16), 1.0, 195)
	_house(Vector3(-24, 0, 10), 1.15, 15)
	_house(Vector3(-14, 0, -26), 0.9, 300)
	_house(Vector3(26, 0, 18), 1.05, 20)

	# Ember drift (3 emitters far from town).
	_embers(Vector3(140, 2, -60))
	_embers(Vector3(-120, 2, 90))
	_embers(Vector3(60, 2, 160))

func _ground_plane(pos: Vector3, size: Vector2, base: Vector3, tint: Vector3, cells: float) -> void:
	var mat := ShaderMaterial.new()
	mat.shader = load("res://shaders/terrain.gdshader")
	mat.set_shader_parameter("base", base)
	mat.set_shader_parameter("tint", tint)
	mat.set_shader_parameter("cells", cells)
	var mesh := MeshInstance3D.new()
	var plane := PlaneMesh.new()
	plane.size = size
	plane.subdivide_width = 64
	plane.subdivide_depth = 64
	mesh.mesh = plane
	mesh.material_override = mat
	mesh.position = pos
	mesh.rotation.x = -PI / 2.0
	add_child(mesh)

func _shrine() -> void:
	var plaza := MeshInstance3D.new()
	plaza.mesh = CylinderMesh.new()
	(plaza.mesh as CylinderMesh).top_radius = 7.0
	(plaza.mesh as CylinderMesh).bottom_radius = 7.5
	(plaza.mesh as CylinderMesh).height = 0.24
	var pm := StandardMaterial3D.new()
	pm.albedo_color = Color("#2a2a33")
	pm.roughness = 0.9
	pm.metallic = 0.1
	plaza.material_override = pm
	plaza.position.y = 0.1
	add_child(plaza)

	var ped := MeshInstance3D.new()
	ped.mesh = CylinderMesh.new()
	(ped.mesh as CylinderMesh).top_radius = 1.4
	(ped.mesh as CylinderMesh).bottom_radius = 1.8
	(ped.mesh as CylinderMesh).height = 1.0
	ped.position.y = 0.62
	add_child(ped)

	var crystal := MeshInstance3D.new()
	crystal.mesh = SphereMesh.new()
	(crystal.mesh as SphereMesh).radius = 0.6
	(crystal.mesh as SphereMesh).height = 2.6
	(crystal.mesh as SphereMesh).radial_segments = 24
	(crystal.mesh as SphereMesh).rings = 16
	var cm := StandardMaterial3D.new()
	cm.albedo_color = Color("#ffcf6e")
	cm.emission_enabled = true
	cm.emission = Color("#ffb14d")
	cm.emission_energy_multiplier = 2.0
	cm.metallic = 0.4
	crystal.material_override = cm
	crystal.position.y = 2.9
	add_child(crystal)

	var l := OmniLight3D.new()
	l.name = "ShrineLight"
	l.light_color = Color("#ffcf6e")
	l.light_energy = 6.0
	l.omni_range = 22.0
	l.position = Vector3(0, 3.4, 0)
	add_child(l)

func _tree(pos: Vector3, scale: float, spread: float) -> void:
	var t := Node3D.new()
	t.position = pos
	var trunk := MeshInstance3D.new()
	trunk.mesh = CylinderMesh.new()
	(trunk.mesh as CylinderMesh).top_radius = 0.16 * scale
	(trunk.mesh as CylinderMesh).bottom_radius = 0.26 * scale
	(trunk.mesh as CylinderMesh).height = 1.6 * scale
	var tm := StandardMaterial3D.new()
	tm.albedo_color = Color("#5a4630")
	tm.roughness = 0.95
	trunk.material_override = tm
	trunk.position.y = 0.8 * scale
	t.add_child(trunk)
	for c in range(3):
		var cone := MeshInstance3D.new()
		cone.mesh = CylinderMesh.new()
		(cone.mesh as CylinderMesh).top_radius = 0.0
		(cone.mesh as CylinderMesh).bottom_radius = 1.6 * scale * (1.0 - c * 0.2)
		(cone.mesh as CylinderMesh).height = 2.4 * scale
		(cone.mesh as CylinderMesh).radial_segments = 9
		var cm := StandardMaterial3D.new()
		var shade := 0.62 + 0.18 * c + 0.05 * spread
		cm.albedo_color = Color(0.13 * shade, 0.30 * shade, 0.16 * shade)
		cm.roughness = 1.0
		cone.material_override = cm
		cone.position.y = 1.7 * scale + c * 1.15 * scale
		t.add_child(cone)
	t.rotation.y = pos.x * 0.7
	add_child(t)

func _rock(pos: Vector3, scale: float) -> void:
	var r := MeshInstance3D.new()
	r.mesh = SphereMesh.new()
	(r.mesh as SphereMesh).radius = 0.5 * scale
	(r.mesh as SphereMesh).height = 0.7 * scale
	var rm := StandardMaterial3D.new()
	rm.albedo_color = Color(0.42, 0.40, 0.40)
	rm.roughness = 0.96
	r.material_override = rm
	r.position = pos + Vector3(0, 0.25 * scale, 0)
	r.scale = Vector3(1.0, 0.6, 0.85)
	add_child(r)

func _house(pos: Vector3, scale: float, rot: float) -> void:
	var h := Node3D.new()
	h.position = pos
	h.rotation.y = deg_to_rad(rot)
	var body := MeshInstance3D.new()
	body.mesh = BoxMesh.new()
	(body.mesh as BoxMesh).size = Vector3(4.2, 2.6, 3.6)
	var bm := StandardMaterial3D.new()
	bm.albedo_color = Color("#c9b28a")
	bm.roughness = 0.9
	body.material_override = bm
	body.position.y = 1.3
	h.add_child(body)
	var roof := MeshInstance3D.new()
	roof.mesh = PrismMesh.new()
	(roof.mesh as PrismMesh).size = Vector3(4.8, 1.6, 4.2)
	var rm := StandardMaterial3D.new()
	rm.albedo_color = Color("#a3492e")
	rm.roughness = 0.85
	roof.material_override = rm
	roof.position.y = 3.15
	roof.rotation.y = PI / 4
	h.add_child(roof)
	var door := MeshInstance3D.new()
	door.mesh = PlaneMesh.new()
	(door.mesh as PlaneMesh).size = Vector2(0.7, 1.5)
	var dm := StandardMaterial3D.new()
	dm.albedo_color = Color("#5a4630")
	door.material_override = dm
	door.position = Vector3(0, 0.75, 1.82)
	h.add_child(door)
	var glow := MeshInstance3D.new()
	glow.mesh = PlaneMesh.new()
	(glow.mesh as PlaneMesh).size = Vector2(0.42, 0.42)
	var gm := StandardMaterial3D.new()
	gm.albedo_color = Color("#ffb14d")
	gm.emission_enabled = true
	gm.emission = Color("#ffb14d")
	gm.emission_energy_multiplier = 2.6
	gm.shading_mode = BaseMaterial3D.SHADING_MODE_UNSHADED
	glow.material_override = gm
	glow.position = Vector3(0.95, 1.7, 1.82)
	h.add_child(glow)
	add_child(h)

func _embers(pos: Vector3) -> void:
	var p := CPUParticles3D.new()
	p.amount = 110
	p.lifetime = 7.0
	p.one_shot = false
	p.emitting = true
	p.emission_shape = CPUParticles3D.EMISSION_SHAPE_SPHERE
	p.emission_sphere_radius = 2.0
	p.direction = Vector3(0, 1, 0)
	p.spread = 40
	p.initial_velocity_min = 0.6
	p.initial_velocity_max = 1.4
	p.gravity = Vector3(0, -0.25, 0)
	p.scale_amount_min = 0.15
	p.scale_amount_max = 0.42
	p.color = Color("#ff9a4d")
	var mesh := QuadMesh.new()
	mesh.size = Vector2(0.18, 0.18)
	var pm := StandardMaterial3D.new()
	pm.albedo_color = Color(1, 0.6, 0.3)
	pm.emission_enabled = true
	pm.emission = Color("#ff7a30")
	pm.emission_energy_multiplier = 3.0
	pm.shading_mode = BaseMaterial3D.SHADING_MODE_UNSHADED
	pm.transparency = BaseMaterial3D.TRANSPARENCY_ALPHA
	p.material_override = pm
	p.mesh = mesh
	p.position = pos
	add_child(p)

# --- player rig + camera ---

func _build_player() -> void:
	rig = Node3D.new()
	rig.name = "PlayerRig"
	add_child(rig)

	var vis := Node3D.new()
	vis.name = "Visual"
	rig.add_child(vis)
	var body := MeshInstance3D.new()
	body.mesh = CapsuleMesh.new()
	(body.mesh as CapsuleMesh).radius = 0.36
	(body.mesh as CapsuleMesh).height = 1.8
	(body.mesh as CapsuleMesh).radial_segments = 16
	(body.mesh as CapsuleMesh).rings = 8
	var klass := str(character.get("class", "blade_dancer"))
	var cm := StandardMaterial3D.new()
	cm.albedo_color = AetheriaTheme.BLADE if klass == "blade_dancer" else AetheriaTheme.SPELL
	cm.roughness = 0.5
	cm.metallic = 0.15
	body.material_override = cm
	body.position.y = 0.9
	vis.add_child(body)

	var sigil := MeshInstance3D.new()
	sigil.mesh = SphereMesh.new()
	(sigil.mesh as SphereMesh).radius = 0.11
	(sigil.mesh as SphereMesh).height = 0.2
	var sm := StandardMaterial3D.new()
	sm.albedo_color = Color("#ffe08a")
	sm.emission_enabled = true
	sm.emission = Color("#ffd06e")
	sm.emission_energy_multiplier = 2.2
	sm.shading_mode = BaseMaterial3D.SHADING_MODE_UNSHADED
	sigil.material_override = sm
	sigil.position = Vector3(0, 1.25, 0)
	vis.add_child(sigil)

	var plate := Label3D.new()
	plate.text = str(character.get("name", "?"))
	plate.billboard = BaseMaterial3D.BILLBOARD_ENABLED
	plate.no_depth_test = true
	plate.outline_size = 10
	plate.outline_modulate = Color(0.02, 0.03, 0.06, 0.9)
	plate.font_size = 28
	plate.font = theme_src.title_font
	plate.position.y = 2.2
	rig.add_child(plate)

	cam = Camera3D.new()
	cam.fov = 66
	rig.add_child(cam)

	var shadow := MeshInstance3D.new()
	shadow.mesh = PlaneMesh.new()
	(shadow.mesh as PlaneMesh).size = Vector2(0.8, 0.8)
	var sh_mat := StandardMaterial3D.new()
	sh_mat.albedo_color = Color(0, 0, 0, 0.3)
	sh_mat.transparency = BaseMaterial3D.TRANSPARENCY_ALPHA
	sh_mat.shading_mode = BaseMaterial3D.SHADING_MODE_UNSHADED
	shadow.material_override = sh_mat
	shadow.rotation.x = -PI / 2.0
	shadow.position.y = 0.02
	vis.add_child(shadow)

	_update_camera()

func _update_camera() -> void:
	var target := rig.global_position + Vector3(0, 1.3, 0)
	var dir := Vector3(sin(cam_yaw) * cos(cam_pitch), sin(cam_pitch), cos(cam_yaw) * cos(cam_pitch))
	cam.global_position = target - dir * cam_dist
	cam.global_position.y = max(cam.global_position.y, 0.35)
	cam.look_at(target)

# --- session wiring ---

func _wire_world() -> void:
	world.server_hello.connect(_on_server_hello)
	world.entered_world.connect(_on_entered_world)
	world.disconnected.connect(_on_disconnected)
	world.snapshot.connect(_on_snapshot)
	world.combat_event.connect(_on_combat)
	world.chat_message.connect(_on_chat)
	world.loot_event.connect(_on_loot)
	world.npc_dialog.connect(_on_npc_dialog)
	world.quest_event.connect(_on_quest_event)
	world.quest_status.connect(_on_quest_status)
	world.respawn_ack.connect(_on_respawn_ack)

func _on_server_hello(hello: Dictionary) -> void:
	hud.set_connection("Entering %s…" % str(hello.get("game_name", "the world")))

func _on_entered_world(ack: Dictionary) -> void:
	if not ack.get("ok", false):
		hud.set_connection("Enter failed: %s" % str(ack.get("error", "?")))
		return
	hud.set_connection("")
	world.quest_status_request()

func _on_disconnected(reason: String, _code: int) -> void:
	hud.set_connection("Disconnected: %s" % reason)

func _on_snapshot(_tick: int, self_entries: Array, entities: Array, despawns: Array) -> void:
	if not self_entries.is_empty():
		var stl: Dictionary = self_entries[0]
		rig.global_position = Vector3(
			float(stl.get("position", Vector3.ZERO).x),
			float(stl.get("position", Vector3.ZERO).y),
			float(stl.get("position", Vector3.ZERO).z))
		if stl.get("rot_y", 0.0) != 0.0:
			rig.rotation.y = float(stl["rot_y"])
		hud.set_self(stl, int(stl.get("max_hp", 100)), int(stl.get("max_mp", 50)),
			int(stl.get("xp_for_level", 40)), int(_gold))
	# Ensure the rig stays flat for 2.5D-ish feel.
	rig.global_position.y = max(rig.global_position.y, 0.0)
	for eid in despawns:
		if ents.has(eid):
			ents[eid].queue_free()
			ents.erase(eid)
		if current_target == int(eid):
			_select(-1)
	for d in entities:
		var eid := int(d["entity_id"])
		if eid == world.self_id:
			continue
		var node: WorldEntity = ents.get(eid)
		if node == null:
			node = _spawn_entity(d)
			ents[eid] = node
		node.refresh(d)
	hud.update_minimap(world.entities.values())

func _spawn_entity(d: Dictionary) -> WorldEntity:
	var etype := str(d.get("entity_type", ""))
	var name := str(d.get("name", ""))
	var ref := str(d.get("ref_id", ""))
	var lv := int(d.get("level", 1))
	var color := _entity_color(d)
	return WorldEntity.create(int(d["entity_id"]), etype, name, ref, theme_src.ui_bold_font, color, lv)

func _entity_color(d: Dictionary) -> Color:
	match str(d.get("entity_type", "")):
		"npc":
			return AetheriaTheme.EDGE
		"drop":
			return AetheriaTheme.GOLD
		"mob":
			var lv := int(d.get("level", 1))
			if lv >= 5:
				return AetheriaTheme.MOB_B3
			if lv >= 3:
				return AetheriaTheme.MOB_B2
			return AetheriaTheme.MOB_B1
		_:
			return AetheriaTheme.PLAYER_OTH

func _on_combat(ev: Dictionary) -> void:
	var etype := str(ev.get("event_type", ""))
	var sid := int(ev.get("source_id", 0))
	var tid := int(ev.get("target_id", 0))
	var msg := str(ev.get("message", ""))
	match etype:
		"hit", "crit", "miss":
			var amount := int(ev.get("amount", 0))
			var who := tid if tid != world.self_id else sid
			_damage_text(who, amount, etype == "crit", ev.get("source_id", 0) == world.self_id and tid != world.self_id)
			hud.add_chat_line("combat", "", msg)
		"xp":
			hud.add_chat_line("combat", "", msg)
			_toast_xp(int(ev.get("amount", 0)))
		"level_up":
			hud.toast("LEVEL UP! " + msg)
			hud.add_chat_line("combat", "", msg)
		"kill":
			hud.add_chat_line("combat", "", msg)
		"death":
			if tid == world.self_id:
				hud.show_death()
				_select(-1)
				_set_mouse_capture(false)
			hud.add_chat_line("combat", "", msg)
		"respawn":
			hud.hide_death()
			hud.toast("You have been returned to Havenport.")
		_:
			hud.add_chat_line("combat", "", msg)

func _toast_xp(amount: int) -> void:
	var t := theme_src.styled_label("+%d XP" % amount, theme_src.title_font, 24, AetheriaTheme.XP)
	t.set_anchors_preset(Control.PRESET_CENTER_TOP)
	t.position.y = 130
	add_child(t)
	var tw := create_tween()
	tw.tween_property(t, "position:y", 178, 1.4).set_trans(Tween.TRANS_QUAD).set_ease(Tween.EASE_OUT)
	tw.parallel().tween_property(t, "modulate:a", 0.0, 1.4)
	tw.tween_callback(func(): t.queue_free())

func _damage_text(entity_id: int, amount: int, crit: bool, mine: bool) -> void:
	var e: WorldEntity = ents.get(entity_id)
	if e == null:
		return
	var l := Label3D.new()
	if crit:
		l.text = "%d!" % amount
		l.font_size = 40
		l.modulate = AetheriaTheme.GOLD
	elif mine:
		l.text = str(amount)
		l.font_size = 26
		l.modulate = AetheriaTheme.XP
	else:
		l.text = str(amount)
		l.font_size = 22
		l.modulate = AetheriaTheme.HEALTH_HI
	l.billboard = BaseMaterial3D.BILLBOARD_ENABLED
	l.no_depth_test = true
	l.outline_size = 8
	l.outline_modulate = Color(0.02, 0.03, 0.06, 0.9)
	l.font = theme_src.ui_bold_font
	l.position = e.global_position + Vector3(0, 2.2, 0)
	get_tree().current_scene.add_child(l)
	var tw := create_tween()
	tw.tween_property(l, "global_position:y", l.global_position.y + 1.6, 0.9).set_trans(Tween.TRANS_QUAD).set_ease(Tween.EASE_OUT)
	tw.parallel().tween_property(l, "modulate:a", 0.0, 0.9)
	tw.tween_callback(func(): l.queue_free())

func _on_chat(msg: Dictionary) -> void:
	hud.add_chat_line(str(msg.get("channel", "say")), str(msg.get("sender_name", "")), str(msg.get("text", "")))

var _gold := 0

func _on_loot(ev: Dictionary) -> void:
	if ev.get("ok", false):
		var gold: int = int(ev.get("gold", 0))
		var item := str(ev.get("item_def_id", ""))
		if gold != 0:
			_gold = int(ev.get("balance", _gold))
			hud.toast(str((("+%d gold" % gold) if gold > 0 else ("%d gold" % gold))))
		elif item != "":
			hud.toast("Picked up %s" % item)
	else:
		hud.add_chat_line("system", "", "Loot failed: %s" % str(ev.get("error", "?")))

func _on_npc_dialog(ev: Dictionary) -> void:
	if not ev.get("ok", false):
		hud.add_chat_line("system", "", str(ev.get("error", "?")))
		return
	hud.show_npc_dialog(ev)
	_set_mouse_capture(false)

func _on_quest_event(ev: Dictionary) -> void:
	var qid := str(ev.get("quest_id", ""))
	if not ev.get("ok", false):
		hud.add_chat_line("system", "", "Quest: %s" % str(ev.get("error", "?")))
		return
	var state := str(ev.get("state", ""))
	var q := {
		"quest_id": qid, "name": str(ev.get("name", qid)), "state": state,
		"objectives": ev.get("objectives", []),
	}
	if state == "active":
		quests[qid] = q
		hud.toast("Quest accepted: %s" % q["name"])
	elif state == "complete":
		quests[qid] = q
		hud.toast("Quest complete: %s" % q["name"])
	elif state == "abandoned":
		quests.erase(qid)
		hud.toast("Quest abandoned")
	_refresh_tracker()
	if state == "complete" or int(ev.get("new_level", 0)) > 0:
		world.quest_status_request()

func _on_quest_status(ev: Dictionary) -> void:
	quests.clear()
	for q in ev.get("quests", []):
		quests[str(q.get("quest_id", ""))] = q
	_refresh_tracker()

func _refresh_tracker() -> void:
	var active: Array = []
	var complete: Array = []
	for q in quests.values():
		if str(q.get("state", "")) == "active":
			active.append(q)
		elif str(q.get("state", "")) == "complete":
			complete.append(q)
	hud.set_quests(active, complete)

func _on_respawn_ack(ev: Dictionary) -> void:
	if ev.get("ok", false):
		hud.hide_death()
	else:
		hud.add_chat_line("system", "", "Respawn rejected: %s" % str(ev.get("error", "?")))

# --- hud wiring ---

func _wire_hud() -> void:
	hud.skill_pressed.connect(_on_skill_pressed)
	hud.auto_attack_toggled.connect(_on_auto_attack_toggled)
	hud.chat_sent.connect(func(ch, text): world.chat(ch, text))
	hud.respawn_pressed.connect(func(): world.respawn_request())
	hud.quest_accept_pressed.connect(func(qid): world.quest_accept(qid))
	hud.quest_turnin_pressed.connect(func(qid): world.quest_turnin(qid))
	hud.quest_abandon_pressed.connect(func(qid): world.quest_abandon(qid))
	hud.dialog_closed.connect(func(): _set_mouse_capture(true))

func _on_skill_pressed(skill_id: String) -> void:
	if not _target_in_range(skill_id):
		return
	world.cast_skill(skill_id, current_target if _target_live_mob() else 0)

func _on_auto_attack_toggled(active: bool) -> void:
	auto_attack_on = active
	_apply_auto_attack()

func _target_live_mob() -> bool:
	if current_target <= 0 or not world.entities.has(current_target):
		return false
	var d: Dictionary = world.entities[current_target]
	return str(d.get("entity_type", "")) == "mob"

func _target_in_range(skill_id: String) -> bool:
	if not current_target or not world.entities.has(current_target):
		return true
	var d: Dictionary = world.entities[current_target]
	var dist := rig.global_position.distance_to(d.get("position", Vector3.ZERO))
	# Server validates exactly; here we only gate obviously-out-of-range aimed casts.
	return true

# --- selection / interaction ---

func _apply_auto_attack() -> void:
	if auto_attack_on and _target_live_mob():
		world.auto_attack(current_target, true)
	elif not auto_attack_on:
		world.auto_attack(0, false)
	hud.set_auto_attack(auto_attack_on)

func _select(eid: int) -> void:
	for e in ents.values():
		if e is WorldEntity:
			e.set_selected(int(e.entity_id) == eid and eid > 0)
	if eid > 0:
		current_target = eid
		var d: Dictionary = world.entities.get(eid, {})
		if str(d.get("entity_type", "")) == "mob":
			hud.set_auto_attack(true)
			auto_attack_on = true
			_apply_auto_attack()
		else:
			hud.set_auto_attack(false)
			auto_attack_on = false
			world.auto_attack(0, false)
			hud.set_target(d)
	else:
		current_target = 0
		world.auto_attack(0, false)
		hud.set_auto_attack(false)
		hud.set_target({})

func _interact() -> void:
	if current_target <= 0 or not ents.has(current_target):
		return
	var d: Dictionary = world.entities.get(current_target, {})
	var etype := str(d.get("entity_type", ""))
	match etype:
		"npc":
			world.npc_interact(str(d.get("ref_id", "")))
		"drop":
			world.pickup(current_target)
		"mob":
			_select(current_target)

# --- input ---

func _set_mouse_capture(on: bool) -> void:
	Input.mouse_mode = Input.MOUSE_MODE_CAPTURED if on else Input.MOUSE_MODE_VISIBLE

func _ui_blocking() -> bool:
	return hud.chat_focused() or hud.quest_log_visible() or hud.dialog_visible()

func _unhandled_input(event: InputEvent) -> void:
	if event is InputEventMouseMotion and Input.mouse_mode == Input.MOUSE_MODE_CAPTURED:
		cam_yaw -= event.relative.x * 0.003
		cam_pitch = clampf(cam_pitch - event.relative.y * 0.003, -0.1, 1.2)
		_update_camera()
		return
	if event is InputEventMouseButton:
		if event.button_index == MOUSE_BUTTON_WHEEL_UP and event.pressed:
			cam_dist = max(3.0, cam_dist - 1.2)
			_update_camera()
		elif event.button_index == MOUSE_BUTTON_WHEEL_DOWN and event.pressed:
			cam_dist = min(18.0, cam_dist + 1.2)
			_update_camera()
		elif event.button_index == MOUSE_BUTTON_LEFT and event.pressed:
			_pick()
		elif event.button_index == MOUSE_BUTTON_RIGHT and event.pressed:
			_pick(true)
		return
	if event is InputEventKey and event.pressed and not event.echo:
		if _ui_blocking():
			return
		match event.keycode:
			KEY_E:
				_interact()
			KEY_J:
				hud.show_quest_log(not hud.quest_log_visible())
				_set_mouse_capture(not hud.quest_log_visible())
			KEY_R:
				auto_attack_on = not auto_attack_on
				_apply_auto_attack()
			KEY_TAB:
				_next_mob()
			KEY_ESCAPE:
				_select(-1)
			KEY_ENTER:
				hud.set_chat_focus(true)
				_set_mouse_capture(false)
			_: pass
		var key: int = event.keycode
		if KEY_1 <= key and key <= KEY_6:
			var idx := key - KEY_1
			var klass := str(character.get("class", "blade_dancer"))
			var list: Array = AetheriaHUD.SKILLS.get(klass, [])
			if idx < list.size():
				_on_skill_pressed(str(list[idx]["id"]))

func _pick(attack := false) -> void:
	if _ui_blocking():
		return
	var space := get_world_3d().direct_space_state
	var vp := get_viewport()
	var center := vp.get_visible_rect().size / 2.0
	var origin := cam.project_ray_origin(center)
	var dir := cam.project_ray_normal(center)
	var query := PhysicsRayQueryParameters3D.create(origin, origin + dir * 220.0, 2)
	var hit := space.intersect_ray(query)
	if hit.is_empty():
		if attack:
			_select(-1)
		return
	var e: WorldEntity = hit.collider
	if e is WorldEntity:
		_select(int(e.entity_id))
		if attack and str(e.entity_type) == "mob":
			_apply_auto_attack()
		elif attack:
			_interact()

func _next_mob() -> void:
	var best := -1
	var best_dist := 1e9
	for id in ents:
		var d: Dictionary = world.entities.get(id, {})
		if str(d.get("entity_type", "")) != "mob":
			continue
		var dist := rig.global_position.distance_to(d.get("position", Vector3.ZERO))
		if dist < best_dist:
			best_dist = dist
			best = int(id)
	if best >= 0:
		_select(best)

func _process(delta: float) -> void:
	var move := Vector3.ZERO
	var fwd := Vector3(-sin(cam_yaw), 0, -cos(cam_yaw))
	var rgt := Vector3(fwd.z, 0, -fwd.x)
	var f := Input.get_action_strength("move_forward") - Input.get_action_strength("move_back")
	var r := Input.get_action_strength("move_right") - Input.get_action_strength("move_left")
	if f != 0.0 or r != 0.0:
		move = (fwd * f + rgt * r).normalized()
	var now := Time.get_ticks_msec()
	if _ui_blocking():
		if world != null:
			world.stop()
	elif not move.is_zero_approx():
		var rot := atan2(move.x, move.z)
		if now - _move_sent_at > 60:
			world.move_intent(move, world.max_speed, rot)
			_move_sent_at = now
	elif now - _move_sent_at > 120:
		world.stop()
		_move_sent_at = now
	_aura_t += delta
	if _aura_t > 1.5:
		_aura_t = 0.0
		var look := cam.global_position
		var dist := look.distance_to(rig.global_position)
		if dist - cam_dist > 1.5 or cam_dist > dist + 3.0:
			_update_camera()