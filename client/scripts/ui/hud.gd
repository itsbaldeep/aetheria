class_name AetheriaHUD
extends CanvasLayer

# Full in-game HUD built in code from AetheriaTheme tokens: self/target
# frames, HP/MP/XP bars, skill bar, chat log, quest tracker + log, NPC dialog,
# minimap-lite, death overlay. Pure presentation — the World scene feeds state
# through public methods and reacts to the signals.

signal skill_pressed(skill_id: String)
signal auto_attack_toggled(active: bool)
signal chat_sent(channel: String, text: String)
signal interact_pressed
signal respawn_pressed
signal quest_accept_pressed(quest_id: String)
signal quest_turnin_pressed(quest_id: String)
signal quest_abandon_pressed(quest_id: String)
signal dialog_closed

const SKILLS := {
	"blade_dancer": [
		{"id": "blade_strike", "name": "Strike", "key": "1"},
		{"id": "twin_slash", "name": "Twin Slash", "key": "2"},
		{"id": "charge", "name": "Charge", "key": "3"},
		{"id": "whirlwind", "name": "Whirlwind", "key": "4"},
		{"id": "rend", "name": "Rend", "key": "5"},
		{"id": "execute", "name": "Execute", "key": "6"},
	],
	"spellweaver": [
		{"id": "arcane_bolt", "name": "Arcane Bolt", "key": "1"},
		{"id": "fireball", "name": "Fireball", "key": "2"},
		{"id": "lightning_bolt", "name": "Lightning", "key": "3"},
		{"id": "arcane_volley", "name": "Volley", "key": "4"},
		{"id": "frost_nova", "name": "Frost Nova", "key": "5"},
		{"id": "mana_shield", "name": "Mana Shield", "key": "6"},
	],
}

var theme_src: AetheriaTheme
var player_class := "blade_dancer"
var chat_lines: Array = []

var _root: Control
var _minimap: Control
var _tracker_box: VBoxContainer
var _self_name: Label
var _self_hp: ProgressBar
var _self_hp_text: Label
var _self_mp: ProgressBar
var _self_mp_text: Label
var _self_xp: ProgressBar
var _self_level: Label
var _self_gold: Label
var _target_panel: PanelContainer
var _target_name: Label
var _target_hp: ProgressBar
var _target_hp_text: Label
var _skill_buttons: Dictionary = {}
var _skill_cooldowns: Dictionary = {}
var _chat: RichTextLabel
var _chat_input: LineEdit
var _dialog_panel: PanelContainer
var _dialog_body: VBoxContainer
var _log_panel: PanelContainer
var _log_body: VBoxContainer
var _death_overlay: ColorRect
var _death_btn: Button
var _conn_label: Label
var _toast: Label
var _vignette: TextureRect
var _auto_btn: Button
var _mouse_visible := false
var _ai := 0
var _tick := 0

func _ready() -> void:
	layer = 10
	process_mode = Node.PROCESS_MODE_ALWAYS
	_build()

func _build() -> void:
	_root = Control.new()
	_root.set_anchors_preset(Control.PRESET_FULL_RECT)
	_root.name = "HUD"
	add_child(_root)

	# Vignette (atmosphere): dark edges over the 3D view.
	_vignette = TextureRect.new()
	_vignette.set_anchors_preset(Control.PRESET_FULL_RECT)
	_vignette.mouse_filter = Control.MOUSE_FILTER_IGNORE
	var vig := GradientTexture2D.new()
	vig.width = 256
	vig.height = 256
	vig.fill = GradientTexture2D.FILL_RADIAL
	vig.fill_from = Vector2(0.5, 0.5)
	vig.fill_to = Vector2(0.5, 0.0)
	var grad := Gradient.new()
	grad.offsets = PackedFloat32Array([0.0, 0.62, 1.0])
	grad.colors = PackedColorArray([Color(0, 0, 0, 0.0), Color(0, 0, 0, 0.0), Color(0.02, 0.01, 0.03, 0.55)])
	vig.gradient = grad
	_vignette.texture = vig
	_root.add_child(_vignette)

	_conn_label = _centered_label("", 18, AetheriaTheme.TEXT_DIM)
	_conn_label.set_anchors_preset(Control.PRESET_CENTER_TOP)
	_conn_label.position.y = 24
	_root.add_child(_conn_label)

	_build_minimap()
	_build_tracker()
	_build_target()
	_build_self()
	_build_skills()
	_build_chat()
	_build_dialog()
	_build_quest_log()
	_build_death()
	_build_toast()

func _centered_label(text: String, size: int, color: Color) -> Label:
	var l := Label.new()
	l.text = text
	l.horizontal_alignment = HORIZONTAL_ALIGNMENT_CENTER
	l.add_theme_font_override("font", theme_src.ui_bold_font)
	l.add_theme_font_size_override("font_size", size)
	l.add_theme_color_override("font_color", color)
	return l

func _build_minimap() -> void:
	_minimap = MinimapDraw.new()
	_minimap.custom_minimum_size = Vector2(176, 176)
	_minimap.position = Vector2(16, 16)
	_root.add_child(_minimap)

func _build_tracker() -> void:
	var wrap := PanelContainer.new()
	wrap.set_anchors_preset(Control.PRESET_TOP_RIGHT)
	wrap.position = Vector2(-16, 16)
	wrap.custom_minimum_size = Vector2(320, 0)
	var sb := StyleBoxFlat.new()
	sb.bg_color = Color(0.03, 0.04, 0.08, 0.72)
	sb.border_color = Color("#d4af37", 0.3)
	sb.set_border_width_all(1)
	sb.set_corner_radius_all(8)
	wrap.add_theme_stylebox_override("panel", sb)
	_root.add_child(wrap)

	var box := VBoxContainer.new()
	box.name = "QuestTracker"
	wrap.add_child(box)

	var title := theme_src.styled_label("QUESTS", theme_src.title_font, 16, AetheriaTheme.EDGE)
	title.horizontal_alignment = HORIZONTAL_ALIGNMENT_CENTER
	box.add_child(title)

	_tracker_box = VBoxContainer.new()
	box.add_child(_tracker_box)
	var hint := theme_src.styled_label("No active quests", theme_src.ui_font, 14, AetheriaTheme.TEXT_FAINT)
	hint.horizontal_alignment = HORIZONTAL_ALIGNMENT_CENTER
	_tracker_box.add_child(hint)

func _build_target() -> void:
	_target_panel = PanelContainer.new()
	_target_panel.position = Vector2(16, 200)
	_target_panel.custom_minimum_size = Vector2(260, 0)
	var sb := StyleBoxFlat.new()
	sb.bg_color = Color(0.03, 0.04, 0.08, 0.7)
	sb.border_color = Color("#d4af37", 0.25)
	sb.set_border_width_all(1)
	sb.set_corner_radius_all(8)
	_target_panel.add_theme_stylebox_override("panel", sb)
	_target_panel.visible = false
	_root.add_child(_target_panel)

	var box := VBoxContainer.new()
	box.name = "TargetFrame"
	box.add_theme_constant_override("separation", 5)
	_target_panel.add_child(box)

	_target_name = theme_src.styled_label("", theme_src.title_font, 17, AetheriaTheme.TEXT)
	_target_name.clip_text = true
	box.add_child(_target_name)

	var hp_row := Control.new()
	hp_row.custom_minimum_size = Vector2(240, 16)
	box.add_child(hp_row)
	_target_hp = ProgressBar.new()
	_target_hp.set_anchors_preset(Control.PRESET_FULL_RECT)
	_target_hp.show_percentage = false
	_target_hp.add_theme_stylebox_override("background", theme_src.bar_bg())
	_target_hp.add_theme_stylebox_override("fill", theme_src.bar_fill(AetheriaTheme.HEALTH))
	_target_hp.max_value = 1
	hp_row.add_child(_target_hp)
	_target_hp_text = _centered_label("", 12, Color.WHITE)
	_target_hp_text.set_anchors_preset(Control.PRESET_FULL_RECT)
	_target_hp_text.mouse_filter = Control.MOUSE_FILTER_IGNORE
	hp_row.add_child(_target_hp_text)

func _build_self() -> void:
	var wrap := PanelContainer.new()
	wrap.position = Vector2(16, 388)
	wrap.custom_minimum_size = Vector2(330, 0)
	var sb := StyleBoxFlat.new()
	sb.bg_color = Color(0.03, 0.04, 0.08, 0.72)
	sb.border_color = Color("#d4af37", 0.3)
	sb.set_border_width_all(1)
	sb.set_corner_radius_all(8)
	wrap.add_theme_stylebox_override("panel", sb)
	_root.add_child(wrap)

	var box := VBoxContainer.new()
	box.name = "SelfFrame"
	box.add_theme_constant_override("separation", 5)
	wrap.add_child(box)

	var head := HBoxContainer.new()
	box.add_child(head)

	_self_level = theme_src.styled_label("1", theme_src.title_font, 24, AetheriaTheme.EDGE)
	_self_level.custom_minimum_size = Vector2(34, 0)
	head.add_child(_self_level)

	var name_col := VBoxContainer.new()
	name_col.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	head.add_child(name_col)
	_self_name = theme_src.styled_label("", theme_src.title_font, 18, AetheriaTheme.TEXT)
	name_col.add_child(_self_name)
	_self_gold = theme_src.styled_label("", theme_src.ui_font, 14, AetheriaTheme.GOLD)
	name_col.add_child(_self_gold)

	_self_hp = _bar_hr(20, AetheriaTheme.HEALTH)
	box.add_child(_self_hp)
	_self_hp_text = _bar_overlay()
	_self_hp.add_child(_self_hp_text)

	_self_mp = _bar_hr(16, AetheriaTheme.MANA)
	box.add_child(_self_mp)
	_self_mp_text = _bar_overlay()
	_self_mp.add_child(_self_mp_text)

	_self_xp = _bar_hr(12, AetheriaTheme.XP)
	box.add_child(_self_xp)

func _bar_hr(h: int, color: Color) -> ProgressBar:
	var b := ProgressBar.new()
	b.custom_minimum_size = Vector2(0, h)
	b.show_percentage = false
	b.add_theme_stylebox_override("background", theme_src.bar_bg())
	b.add_theme_stylebox_override("fill", theme_src.bar_fill(color))
	return b

func _bar_overlay() -> Label:
	var l := _centered_label("", 11, Color.WHITE)
	l.set_anchors_preset(Control.PRESET_FULL_RECT)
	l.mouse_filter = Control.MOUSE_FILTER_IGNORE
	return l

func _build_skills() -> void:
	var wrap := HBoxContainer.new()
	wrap.set_anchors_preset(Control.PRESET_CENTER_BOTTOM)
	wrap.position = Vector2(-250, -64)
	wrap.add_theme_constant_override("separation", 8)
	_root.add_child(wrap)

	_auto_btn = _skill_button("auto", "Auto", "RMB", AetheriaTheme.BLADE)
	_auto_btn.toggle_mode = true
	wrap.add_child(_auto_btn)

	for sk in SKILLS.get(player_class, []):
		var id: String = sk["id"]
		var b := _skill_button(id, sk["name"], sk["key"], AetheriaTheme.GOLD if id.contains("strike") or id.contains("bolt") else AetheriaTheme.SPELL)
		b.pressed.connect(func(): emit_signal("skill_pressed", id))
		wrap.add_child(b)
		_skill_buttons[id] = b
		_skill_cooldowns[id] = 0

func _skill_button(id: String, name: String, hint: String, color: Color) -> Button:
	var b := Button.new()
	b.custom_minimum_size = Vector2(64, 64)
	b.tooltip_text = name
	var sb := StyleBoxFlat.new()
	sb.bg_color = Color(0.04, 0.06, 0.12, 0.88)
	sb.border_color = Color("#d4af37", 0.35)
	sb.set_border_width_all(1)
	sb.set_corner_radius_all(6)
	var sb_h := sb.duplicate()
	sb_h.bg_color = Color(0.10, 0.14, 0.26, 0.95)
	sb_h.border_color = Color("#d4af37", 0.85)
	var sb_p := sb.duplicate()
	sb_p.bg_color = Color(0.16, 0.20, 0.36, 1.0)
	b.add_theme_stylebox_override("normal", sb)
	b.add_theme_stylebox_override("hover", sb_h)
	b.add_theme_stylebox_override("pressed", sb_p)
	var v := VBoxContainer.new()
	v.alignment = BoxContainer.ALIGNMENT_CENTER
	b.add_child(v)
	var n := theme_src.styled_label(name, theme_src.ui_bold_font, 13, Color.WHITE)
	n.horizontal_alignment = HORIZONTAL_ALIGNMENT_CENTER
	v.add_child(n)
	var h := theme_src.styled_label(hint, theme_src.ui_font, 11, color)
	h.horizontal_alignment = HORIZONTAL_ALIGNMENT_CENTER
	v.add_child(h)
	b.text = ""
	return b

func _build_chat() -> void:
	var wrap := PanelContainer.new()
	wrap.set_anchors_preset(Control.PRESET_BOTTOM_RIGHT)
	wrap.position = Vector2(-16, -16)
	wrap.custom_minimum_size = Vector2(560, 210)
	var sb := StyleBoxFlat.new()
	sb.bg_color = Color(0.02, 0.03, 0.06, 0.62)
	sb.border_color = Color("#d4af37", 0.22)
	sb.set_border_width_all(1)
	sb.set_corner_radius_all(8)
	wrap.add_theme_stylebox_override("panel", sb)
	_root.add_child(wrap)

	var box := VBoxContainer.new()
	wrap.add_child(box)

	var tabs := HBoxContainer.new()
	box.add_child(tabs)
	var say := theme_src.styled_label("Say", theme_src.ui_bold_font, 13, AetheriaTheme.TEXT)
	tabs.add_child(say)
	tabs.add_child(theme_src.styled_label("  ·  ", theme_src.ui_font, 13, AetheriaTheme.TEXT_FAINT))
	var world := theme_src.styled_label("World", theme_src.ui_bold_font, 13, AetheriaTheme.EDGE)
	tabs.add_child(world)

	_chat = RichTextLabel.new()
	_chat.bbcode_enabled = true
	_chat.fit_content = false
	_chat.scroll_active = true
	_chat.custom_minimum_size = Vector2(0, 130)
	_chat.size_flags_vertical = Control.SIZE_EXPAND_FILL
	box.add_child(_chat)

	_chat_input = LineEdit.new()
	_chat_input.placeholder_text = "Press Enter to chat…"
	_chat_input.add_theme_font_size_override("font_size", 14)
	_chat_input.text_submitted.connect(_on_chat_submitted)
	box.add_child(_chat_input)

func _on_chat_submitted(text: String) -> void:
	var t := text.strip_edges()
	if t.is_empty():
		_chat_input.release_focus()
		return
	var channel := "say"
	if t.begins_with("/w ") or t.begins_with("/world "):
		channel = "world"
		t = t.substr(t.find(" ") + 1).strip_edges()
	emit_signal("chat_sent", channel, t)
	_chat_input.text = ""
	_chat_input.release_focus()

func _build_dialog() -> void:
	_dialog_panel = PanelContainer.new()
	_dialog_panel.set_anchors_preset(Control.PRESET_CENTER)
	_dialog_panel.position = Vector2(-320, -220)
	_dialog_panel.custom_minimum_size = Vector2(640, 0)
	_dialog_panel.visible = false
	var sb := StyleBoxFlat.new()
	sb.bg_color = Color(0.05, 0.07, 0.14, 0.98)
	sb.border_color = Color("#d4af37", 0.6)
	sb.set_border_width_all(2)
	sb.set_corner_radius_all(10)
	sb.shadow_color = Color(0, 0, 0, 0.5)
	sb.shadow_size = 12
	_dialog_panel.add_theme_stylebox_override("panel", sb)
	_root.add_child(_dialog_panel)

	_dialog_body = VBoxContainer.new()
	_dialog_body.name = "DialogBody"
	_dialog_body.add_theme_constant_override("separation", 10)
	_dialog_panel.add_child(_dialog_body)

func _build_quest_log() -> void:
	_log_panel = PanelContainer.new()
	_log_panel.set_anchors_preset(Control.PRESET_CENTER)
	_log_panel.position = Vector2(-420, -260)
	_log_panel.custom_minimum_size = Vector2(840, 0)
	_log_panel.visible = false
	var sb := StyleBoxFlat.new()
	sb.bg_color = Color(0.05, 0.07, 0.14, 0.98)
	sb.border_color = Color("#d4af37", 0.6)
	sb.set_border_width_all(2)
	sb.set_corner_radius_all(10)
	sb.shadow_color = Color(0, 0, 0, 0.5)
	sb.shadow_size = 12
	_log_panel.add_theme_stylebox_override("panel", sb)
	_root.add_child(_log_panel)

	var box := VBoxContainer.new()
	box.add_theme_constant_override("separation", 10)
	_log_panel.add_child(box)
	var title := theme_src.styled_label("Quest Log", theme_src.title_font, 22, AetheriaTheme.EDGE)
	title.horizontal_alignment = HORIZONTAL_ALIGNMENT_CENTER
	box.add_child(title)
	_log_body = VBoxContainer.new()
	box.add_child(_log_body)
	var close := Button.new()
	close.text = "Close  (J)"
	close.pressed.connect(func(): _log_panel.visible = false)
	box.add_child(close)

func _build_death() -> void:
	_death_overlay = ColorRect.new()
	_death_overlay.set_anchors_preset(Control.PRESET_FULL_RECT)
	_death_overlay.color = Color(0.12, 0.0, 0.02, 0.55)
	_death_overlay.visible = false
	_death_overlay.mouse_filter = Control.MOUSE_FILTER_STOP
	_root.add_child(_death_overlay)

	var center := VBoxContainer.new()
	center.set_anchors_preset(Control.PRESET_CENTER)
	center.position = Vector2(-160, -70)
	center.custom_minimum_size = Vector2(320, 0)
	center.alignment = BoxContainer.ALIGNMENT_CENTER
	center.add_theme_constant_override("separation", 14)
	_death_overlay.add_child(center)
	var t := theme_src.styled_label("You have fallen", theme_src.title_font, 30, AetheriaTheme.HEALTH_HI)
	t.horizontal_alignment = HORIZONTAL_ALIGNMENT_CENTER
	center.add_child(t)
	var sub := theme_src.styled_label("The embers carry you back to the shrine.", theme_src.ui_font, 14, AetheriaTheme.TEXT_DIM)
	sub.horizontal_alignment = HORIZONTAL_ALIGNMENT_CENTER
	center.add_child(sub)
	_death_btn = Button.new()
	_death_btn.text = "Respawn at Shrine"
	_death_btn.pressed.connect(func(): emit_signal("respawn_pressed"))
	center.add_child(_death_btn)

func _build_toast() -> void:
	_toast = _centered_label("", 26, AetheriaTheme.GOLD)
	_toast.set_anchors_preset(Control.PRESET_CENTER_TOP)
	_toast.position.y = 96
	_toast.visible = false
	_root.add_child(_toast)

# --- public update API ---

func set_self(d: Dictionary, max_hp: int, max_mp: int, max_xp: int, gold: int) -> void:
	_self_name.text = str(d.get("name", ""))
	var lv := int(d.get("level", 1))
	_self_level.text = str(lv)
	_self_gold.text = "⛁ %d gold" % gold
	_self_hp.max_value = max(max_hp, 1)
	_self_hp.value = int(d.get("hp", 0))
	_self_hp_text.text = "%d / %d" % [int(d.get("hp", 0)), max_hp]
	_self_mp.max_value = max(max_mp, 1)
	_self_mp.value = int(d.get("mp", 0))
	_self_mp_text.text = "%d / %d" % [int(d.get("mp", 0)), max_mp]
	_self_xp.max_value = max(max_xp, 1)
	_self_xp.value = int(d.get("xp", 0))
	if _minimap != null:
		_minimap.set_self_pos(d.get("position", Vector3.ZERO))

func set_target(d: Dictionary) -> void:
	if d.is_empty():
		_target_panel.visible = false
		return
	_target_panel.visible = true
	_target_name.text = str(d.get("name", "?"))
	if d.get("entity_type", "") == "mob":
		_target_name.add_theme_color_override("font_color", AetheriaTheme.HEALTH_HI)
	else:
		_target_name.remove_theme_color_override("font_color")
	var hp := int(d.get("hp", 0))
	var max_hp := int(d.get("max_hp", 1))
	_target_hp.max_value = max(max_hp, 1)
	_target_hp.value = hp
	_target_hp_text.text = "%d / %d" % [hp, max_hp]

func set_skill_cooldown(skill_id: String, remaining_ms: int) -> void:
	_skill_cooldowns[skill_id] = remaining_ms

func set_connection(text: String) -> void:
	_conn_label.text = text

func toast(text: String) -> void:
	_toast.text = text
	_toast.visible = true
	var tw := create_tween()
	tw.tween_interval(1.6)
	tw.tween_property(_toast, "modulate:a", 0.0, 0.6)
	tw.tween_callback(func(): _toast.visible = false; _toast.modulate.a = 1.0)

func add_chat_line(channel: String, sender: String, text: String) -> void:
	chat_lines.append([channel, sender, text])
	if chat_lines.size() > 60:
		chat_lines.pop_front()
	var color := AetheriaTheme.TEXT
	var prefix := ""
	match channel:
		"world":
			color = AetheriaTheme.EDGE
			prefix = "[World] "
		"say":
			color = AetheriaTheme.TEXT
		"combat", "system":
			color = AetheriaTheme.TEXT_DIM
			prefix = "· "
	if sender != "":
		prefix += sender + ": "
	_chat.push_color(color)
	_chat.append_text(prefix + text)
	_chat.push_color(AetheriaTheme.TEXT_DIM)
	_chat.add_text("\n")
	_chat.pop_all()

func set_quests(active: Array, complete: Array) -> void:
	for c in _tracker_box.get_children():
		c.queue_free()
	for q in active:
		var lbl := theme_src.styled_label(str(q.get("name", "?")), theme_src.ui_bold_font, 15, AetheriaTheme.EDGE)
		_tracker_box.add_child(lbl)
		var objs: Array = q.get("objectives", [])
		for o in objs:
			var line := theme_src.styled_label(
				"%s %d/%d" % [str(o.get("target_name", "?")), int(o.get("current", 0)), int(o.get("required", 1))],
				theme_src.ui_font, 13, AetheriaTheme.TEXT)
			line.add_theme_color_override("font_color", AetheriaTheme.XP if int(o.get("current", 0)) >= int(o.get("required", 1)) else AetheriaTheme.TEXT)
			var pad := MarginContainer.new()
			pad.add_theme_constant_override("margin_left", 12)
			pad.add_child(line)
			_tracker_box.add_child(pad)
	if active.is_empty():
		_tracker_box.add_child(theme_src.styled_label("No active quests", theme_src.ui_font, 14, AetheriaTheme.TEXT_FAINT))
	if not _log_panel.visible:
		return
	for c in _log_body.get_children():
		c.queue_free()
	for q in active:
		_log_body.add_child(_quest_row(q, true))
	for q in complete:
		_log_body.add_child(_quest_row(q, false))
	if active.is_empty() and complete.is_empty():
		_log_body.add_child(theme_src.styled_label("You have no quests yet.", theme_src.ui_font, 14, AetheriaTheme.TEXT_FAINT))

func _quest_row(q: Dictionary, active: bool) -> Control:
	var row := HBoxContainer.new()
	var name := theme_src.styled_label(str(q.get("name", "?")), theme_src.ui_bold_font, 15, AetheriaTheme.EDGE if active else AetheriaTheme.XP)
	name.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	row.add_child(name)
	if active:
		var ab := Button.new()
		ab.text = "Abandon"
		ab.add_theme_font_size_override("font_size", 12)
		ab.pressed.connect(func(): emit_signal("quest_abandon_pressed", str(q.get("quest_id", ""))))
		row.add_child(ab)
	var col := VBoxContainer.new()
	for o in q.get("objectives", []):
		col.add_child(theme_src.styled_label(
			"  %s %d/%d" % [str(o.get("target_name", "?")), int(o.get("current", 0)), int(o.get("required", 1))],
			theme_src.ui_font, 13, AetheriaTheme.TEXT_DIM))
	row.add_child(col)
	return row

func show_npc_dialog(ev: Dictionary) -> void:
	for c in _dialog_body.get_children():
		c.queue_free()
	var head := HBoxContainer.new()
	var portrait := PanelContainer.new()
	portrait.custom_minimum_size = Vector2(64, 64)
	var psb := StyleBoxFlat.new()
	psb.bg_color = Color("#b0563a")
	psb.border_color = AetheriaTheme.EDGE
	psb.set_border_width_all(2)
	psb.set_corner_radius_all(6)
	portrait.add_theme_stylebox_override("panel", psb)
	head.add_child(portrait)
	var init := theme_src.styled_label(str(ev.get("npc_name", "?")).substr(0, 1), theme_src.title_font, 34, Color.WHITE)
	init.set_anchors_preset(Control.PRESET_CENTER)
	portrait.add_child(init)

	var name_col := VBoxContainer.new()
	name_col.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	head.add_child(name_col)
	var nm := theme_src.styled_label(str(ev.get("npc_name", "Unknown")), theme_src.title_font, 20, AetheriaTheme.EDGE)
	name_col.add_child(nm)
	var kind := theme_src.styled_label("Havenport", theme_src.ui_font, 12, AetheriaTheme.TEXT_FAINT)
	name_col.add_child(kind)
	_dialog_body.add_child(head)

	var hr := HSeparator.new()
	_dialog_body.add_child(hr)

	var body := theme_src.styled_label(str(ev.get("dialog", "")), theme_src.ui_font, 15, AetheriaTheme.TEXT)
	body.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	body.custom_minimum_size = Vector2(580, 0)
	_dialog_body.add_child(body)

	for qid in ev.get("available", []):
		var q := _find_quest(qid)
		var row := HBoxContainer.new()
		var lbl := theme_src.styled_label(str(q.get("name", qid)), theme_src.ui_font, 14, AetheriaTheme.TEXT)
		lbl.size_flags_horizontal = Control.SIZE_EXPAND_FILL
		row.add_child(lbl)
		var acc := Button.new()
		acc.text = "Accept"
		acc.pressed.connect(func(): emit_signal("quest_accept_pressed", qid))
		row.add_child(acc)
		_dialog_body.add_child(row)
	for qid in ev.get("turnin", []):
		var q := _find_quest(qid)
		var row := HBoxContainer.new()
		var lbl := theme_src.styled_label(str(q.get("name", qid)), theme_src.ui_font, 14, AetheriaTheme.XP)
		lbl.size_flags_horizontal = Control.SIZE_EXPAND_FILL
		row.add_child(lbl)
		var tn := Button.new()
		tn.text = "Turn In"
		tn.pressed.connect(func(): emit_signal("quest_turnin_pressed", qid))
		row.add_child(tn)
		_dialog_body.add_child(row)
	if ev.get("available", []).is_empty() and ev.get("turnin", []).is_empty():
		_dialog_body.add_child(theme_src.styled_label("(This NPC has no quests for you.)", theme_src.ui_font, 13, AetheriaTheme.TEXT_FAINT))
	var close := Button.new()
	close.text = "Goodbye"
	close.pressed.connect(func(): _dialog_panel.visible = false; emit_signal("dialog_closed"))
	_dialog_body.add_child(close)
	_dialog_panel.visible = true

func _find_quest(qid: String) -> Dictionary:
	for q in _quests_all:
		if str(q.get("quest_id", "")) == qid:
			return q
	return {"name": qid}

var _quests_all: Array = []

func set_quests_all(list: Array) -> void:
	_quests_all = list

func show_quest_log(visible: bool) -> void:
	_log_panel.visible = visible

func quest_log_visible() -> bool:
	return _log_panel.visible

func dialog_visible() -> bool:
	return _dialog_panel.visible

func death_visible() -> bool:
	return _death_overlay.visible

func show_death() -> void:
	_death_overlay.visible = true
	_death_btn.disabled = false

func hide_death() -> void:
	_death_overlay.visible = false

func set_auto_attack(on: bool) -> void:
	_auto_btn.button_pressed = on

func set_chat_focus(focus: bool) -> void:
	if focus:
		_chat_input.grab_focus()
	else:
		_chat_input.release_focus()

func chat_focused() -> bool:
	return _chat_input.has_focus()

func update_minimap(entities: Array) -> void:
	if _minimap != null:
		_minimap.set_entities(entities)

func _process(_delta: float) -> void:
	_tick += 1
	if _tick % 10 != 0:
		return
	# Cooldown countdown on skill buttons.
	for id in _skill_buttons:
		if not _skill_cooldowns.has(id):
			continue
		var ms: int = _skill_cooldowns[id]
		if ms > 0:
			_skill_cooldowns[id] = max(0, ms - 100)
		var b: Button = _skill_buttons[id]
		b.disabled = _skill_cooldowns[id] > 0
		if b.disabled:
			b.tooltip_text = "%.1fs" % (_skill_cooldowns[id] / 1000.0)
		elif _skill_names.has(id):
			b.tooltip_text = _skill_names[id]

var _skill_names := {}
var _skill_keys := {}

# Called by World after build() to register skill name/key for tooltips.
func set_skill_keys() -> void:
	_skill_names.clear()
	for sk in SKILLS.get(player_class, []):
		_skill_names[sk["id"]] = sk["name"]


# --- minimap ---

class MinimapDraw:
	extends Control

	# Lite minimap: north-up dot map of nearby entities around the player.

	const RADIUS := 150.0

	var self_id := 0
	var self_pos := Vector3.ZERO
	var entities: Array = []

	func set_self_id(id: int) -> void:
		self_id = id

	func set_self_pos(p: Vector3) -> void:
		self_pos = p
		queue_redraw()

	func set_entities(list: Array) -> void:
		entities = list
		queue_redraw()

	func _draw() -> void:
		var size := get_size()
		var center := size / 2.0
		# Panel backing.
		draw_rect(Rect2(Vector2.ZERO, size), Color(0.02, 0.03, 0.06, 0.78))
		draw_arc(center, size.x / 2.0 - 2.0, 0, TAU, 48, Color("#d4af37", 0.4), 1.4)
		draw_arc(center, size.x / 2.0 - 2.0 - 14.0, 0, TAU, 48, Color("#d4af37", 0.12), 1.0)
		# North tick.
		draw_line(center + Vector2(0, -size.y / 2.0 + 4), center + Vector2(0, -size.y / 2.0 + 10), AetheriaTheme.EDGE, 1.6)
		var scale_f := (size.x / 2.0 - 14.0) / RADIUS
		# Entities.
		for e in entities:
			var p: Vector3 = e.get("position", Vector3.ZERO)
			var rel := Vector2(p.x - self_pos.x, p.z - self_pos.z)
			var dot_pos := center + rel * scale_f
			if dot_pos.distance_to(center) > size.x / 2.0 - 16.0:
				continue
			var col := _color_for(e)
			draw_circle(dot_pos, 2.6, col)
			draw_circle(dot_pos, 2.6, Color(1, 1, 1, 0.35))
		# Player arrow.
		var tri := PackedVector2Array([
			center + Vector2(0, -7),
			center + Vector2(-5, 5),
			center + Vector2(5, 5),
		])
		draw_colored_polygon(tri, Color("#ffe08a"))

	func _color_for(e: Dictionary) -> Color:
		match str(e.get("entity_type", "")):
			"npc":
				return AetheriaTheme.EDGE
			"drop":
				return AetheriaTheme.XP
			"mob":
				var lv := int(e.get("level", 1))
				if lv >= 5:
					return AetheriaTheme.MOB_B3
				if lv >= 3:
					return AetheriaTheme.MOB_B2
				return AetheriaTheme.MOB_B1
			_:
				return AetheriaTheme.PLAYER_OTH
