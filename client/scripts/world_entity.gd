class_name WorldEntity
extends StaticBody3D

# Renders one server-side entity (player/npc/mob/drop) as a themed placeholder
# until 3D models land: tinted capsule + blob shadow + floating nameplate +
# optional billboard HP bar. Purely visual — the World scene feeds it snapshot
# dicts and it positions itself.

var entity_id := 0
var entity_type := ""
var ref_id := ""
var display_name := ""
var max_hp := 1

var body: MeshInstance3D
var name_plate: Label3D
var hp_bar: Node3D
var hp_fill: MeshInstance3D
var ring: MeshInstance3D
var drop_light: OmniLight3D
var select_ring: MeshInstance3D

var _drop_t := 0.0
var _color := Color.WHITE
var _is_drop := false
var _theme_font: Font
var _bob_y := 0.0

static func create(eid: int, etype: String, name_text: String, ref: String, font: Font, ecolor: Color, lv: int) -> WorldEntity:
	var e := WorldEntity.new()
	e.entity_id = eid
	e.entity_type = etype
	e.ref_id = ref
	e.display_name = name_text
	e._theme_font = font
	e._color = ecolor
	e._level_hint = lv
	e._build()
	return e

func _build() -> void:
	_is_drop = entity_type == "drop"
	var scale := 1.0
	var height := 1.6
	if entity_type == "mob":
		var lv := int(_level_hint)
		if lv >= 5:
			scale = 1.25
			height = 2.0
		elif lv >= 3:
			scale = 1.1
			height = 1.8
	elif entity_type == "npc":
		scale = 1.15
		height = 1.9
		drop_light = OmniLight3D.new()
		drop_light.light_color = _color
		drop_light.light_energy = 1.6
		drop_light.omni_range = 3.0
		add_child(drop_light)

	# Click/cast selection shape (raycast mask 2).
	var col := CollisionShape3D.new()
	var shape := SphereShape3D.new()
	shape.radius = 1.1 * max(scale, 1.0)
	col.shape = shape
	col.position.y = 0.9 * scale
	collision_layer = 2
	collision_mask = 0
	add_child(col)

	# Ground blob shadow.
	var shadow_mat := StandardMaterial3D.new()
	shadow_mat.albedo_color = Color(0, 0, 0, 0.28)
	shadow_mat.transparency = BaseMaterial3D.TRANSPARENCY_ALPHA
	shadow_mat.shading_mode = BaseMaterial3D.SHADING_MODE_UNSHADED
	var shadow := MeshInstance3D.new()
	shadow.mesh = PlaneMesh.new()
	(shadow.mesh as PlaneMesh).size = Vector2(0.7 * scale, 0.7 * scale)
	shadow.material_override = shadow_mat
	shadow.rotation.x = -PI / 2
	shadow.position.y = 0.02
	add_child(shadow)

	if _is_drop:
		body = MeshInstance3D.new()
		body.mesh = SphereMesh.new()
		(body.mesh as SphereMesh).radius = 0.18
		(body.mesh as SphereMesh).height = 0.36
		var dm := StandardMaterial3D.new()
		dm.albedo_color = _color
		dm.emission_enabled = true
		dm.emission = _color
		dm.emission_energy_multiplier = 1.6
		body.material_override = dm
		body.position.y = 0.45
		add_child(body)
		drop_light = OmniLight3D.new()
		drop_light.light_color = _color
		drop_light.light_energy = 2.2
		drop_light.omni_range = 2.2
		add_child(drop_light)
		_bob_y = 0.0
	else:
		body = MeshInstance3D.new()
		body.mesh = CapsuleMesh.new()
		(body.mesh as CapsuleMesh).radius = 0.32 * scale
		(body.mesh as CapsuleMesh).height = height
		(body.mesh as CapsuleMesh).radial_segments = 12
		(body.mesh as CapsuleMesh).rings = 6
		var mat := StandardMaterial3D.new()
		mat.albedo_color = _color
		mat.roughness = 0.55
		mat.metallic = 0.05
		body.material_override = mat
		body.position.y = height / 2.0
		add_child(body)

		# An identity ring under NPCs (giver gold, vendor teal).
		if entity_type == "npc":
			ring = MeshInstance3D.new()
			ring.mesh = TorusMesh.new()
			(ring.mesh as TorusMesh).inner_radius = 0.34
			(ring.mesh as TorusMesh).outer_radius = 0.44
			(ring.mesh as TorusMesh).rings = 8
			(ring.mesh as TorusMesh).sides = 12
			var rm := StandardMaterial3D.new()
			rm.albedo_color = Color.WHITE
			rm.emission_enabled = true
			rm.emission = _color
			rm.emission_energy_multiplier = 1.2
			rm.shading_mode = BaseMaterial3D.SHADING_MODE_UNSHADED
			ring.material_override = rm
			ring.position.y = height + 0.15
			add_child(ring)
			ring.rotation.x = -PI / 2

	# Nameplate.
	if not display_name.is_empty() or entity_type == "npc" or entity_type == "mob":
		name_plate = Label3D.new()
		name_plate.text = display_name
		name_plate.billboard = BaseMaterial3D.BILLBOARD_ENABLED
		name_plate.no_depth_test = true
		name_plate.outline_size = 10
		name_plate.outline_modulate = Color(0.02, 0.03, 0.06, 0.9)
		name_plate.modulate = Color.WHITE
		name_plate.font_size = 26 if entity_type == "player" else 20
		name_plate.position.y = _plate_height(height, scale)
		if _theme_font != null:
			name_plate.font = _theme_font
		add_child(name_plate)

	# Mob HP bar (billboard quads).
	if entity_type == "mob":
		hp_bar = Node3D.new()
		hp_bar.position.y = _plate_height(height, scale) - 0.16
		add_child(hp_bar)

		var bg_mat := StandardMaterial3D.new()
		bg_mat.albedo_color = Color(0.02, 0.03, 0.06, 0.9)
		bg_mat.transparency = BaseMaterial3D.TRANSPARENCY_ALPHA
		bg_mat.shading_mode = BaseMaterial3D.SHADING_MODE_UNSHADED
		bg_mat.billboard_mode = BaseMaterial3D.BILLBOARD_ENABLED
		var bg := MeshInstance3D.new()
		bg.mesh = PlaneMesh.new()
		(bg.mesh as PlaneMesh).size = Vector2(1.0, 0.12)
		bg.material_override = bg_mat
		bg.position.z = 0.001
		hp_bar.add_child(bg)

		var fill_mat := StandardMaterial3D.new()
		fill_mat.albedo_color = _color
		fill_mat.shading_mode = BaseMaterial3D.SHADING_MODE_UNSHADED
		fill_mat.billboard_mode = BaseMaterial3D.BILLBOARD_ENABLED
		hp_fill = MeshInstance3D.new()
		hp_fill.mesh = PlaneMesh.new()
		(hp_fill.mesh as PlaneMesh).size = Vector2(0.96, 0.08)
		hp_fill.material_override = fill_mat
		hp_fill.position.z = 0.002
		hp_bar.add_child(hp_fill)

	# Selection ring (shown when targeted).
	select_ring = MeshInstance3D.new()
	select_ring.mesh = TorusMesh.new()
	(select_ring.mesh as TorusMesh).inner_radius = 0.42
	(select_ring.mesh as TorusMesh).outer_radius = 0.5
	(select_ring.mesh as TorusMesh).rings = 8
	(select_ring.mesh as TorusMesh).sides = 16
	var sm := StandardMaterial3D.new()
	sm.albedo_color = Color(1, 1, 1, 0.95)
	sm.emission_enabled = true
	sm.emission = Color("#ffe08a")
	sm.emission_energy_multiplier = 1.4
	sm.shading_mode = BaseMaterial3D.SHADING_MODE_UNSHADED
	sm.transparency = BaseMaterial3D.TRANSPARENCY_ALPHA
	select_ring.material_override = sm
	select_ring.position.y = 0.05
	select_ring.rotation.x = -PI / 2
	select_ring.visible = false
	add_child(select_ring)

# Level hint used at build time to scale band 3 mobs.
var _level_hint := 1

func _plate_height(height: float, scale: float) -> float:
	if _is_drop:
		return 0.9
	return height * scale + 0.28

func refresh(d: Dictionary) -> void:
	var p: Vector3 = d.get("position", Vector3.ZERO)
	global_position = p
	if d.get("rot_y", 0.0) != 0.0:
		rotation.y = d.get("rot_y", 0.0)
	_level_hint = int(d.get("level", 1))
	var hp: int = int(d.get("hp", 0))
	max_hp = int(d.get("max_hp", 1))
	if hp_fill != null and max_hp > 0:
		var ratio := clampf(float(hp) / float(max_hp), 0.0, 1.0)
		(hp_fill.mesh as PlaneMesh).size = Vector2(0.96 * ratio, 0.08)

func set_selected(on: bool) -> void:
	if select_ring != null:
		select_ring.visible = on

func _process(delta: float) -> void:
	if _is_drop:
		_drop_t += delta
		body.rotation.y = _drop_t * 1.6
		body.position.y = 0.45 + sin(_drop_t * 2.2) * 0.08
