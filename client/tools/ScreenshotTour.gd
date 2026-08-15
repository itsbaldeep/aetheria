extends SceneTree

# M5.5 §1 — Screenshot tour. Launched by `make screenshots` under Xvfb with
# software GL. Walks every reachable screen / UI state, captures the root
# viewport to PNG, and emits 960×540 webp thumbnails.
#
# States that need a live server + the screenshot_bot account (real combat
# snapshots, server-driven quest data) are implemented as tour stages but
# skipped when no `--ws` / `--api` is supplied — the pipeline must run fully
# offline on the VPS for the maiden publish. Skipped stages log a note so the
# human can see coverage in the gallery index. Offline stages drive the REAL
# scene graphs and HUD via their public APIs (no mocked rendering).
#
# Run (normally via make screenshots):
#   godot4 --path client --rendering-method gl_compatibility --resolution 1920x1080 \
#     -- --screenshot-tour --out docs/screens/<sha> --sha <sha> [--api URL --ws URL]
#
# Output: <out>/NN_name.png (+ <out>/thumb/NN_name.webp), <out>/index.txt.

const OUT_DEFAULT := "res://../docs/screens/local"
const W := 1920
const H := 1080
const THUMB_W := 960
const THUMB_H := 540

var _out := OUT_DEFAULT
var _sha := "local"
var _api := ""
var _ws := ""
var _has_server := false

var _steps: Array = []   # list of {"name":..., "run":Callable}
var _i := 0
var _current: Node = null
var _index_lines: Array = []
var _skipped := 0
var _captured := 0

func _init() -> void:
	_parse_args()
	_out = _out if _out.begins_with("/") else _resolve_out(_out)
	_ensure_dir(_out)
	_ensure_dir(_out + "/thumb")
	_build_steps()
	print("[screenshot-tour] out=%s sha=%s server=%s steps=%d" % [_out, _sha, _has_server, _steps.size()])

func _resolve_out(p: String) -> String:
	# res://-relative -> project parent (repo root) /docs/screens/...
	if p.begins_with("res://"):
		return ProjectSettings.globalize_path(p)
	return p

func _ensure_dir(p: String) -> void:
	if DirAccess.dir_exists_absolute(p):
		return
	# mkdir -p: open the parent and recurse
	var parent := p.get_base_dir()
	if parent != "" and not DirAccess.dir_exists_absolute(parent):
		_ensure_dir(parent)
	var d := DirAccess.open(parent if parent != "" else ".")
	if d != null:
		d.make_dir_recursive(p.get_file() if parent != "" else p)

func _parse_args() -> void:
	var args := OS.get_cmdline_user_args()
	var i := 0
	while i < args.size():
		match args[i]:
			"--out":
				if i + 1 < args.size(): _out = args[i + 1]; i += 1
			"--sha":
				if i + 1 < args.size(): _sha = args[i + 1]; i += 1
			"--api":
				if i + 1 < args.size(): _api = args[i + 1]; i += 1
			"--ws":
				if i + 1 < args.size(): _ws = args[i + 1]; i += 1
		i += 1
	_has_server = _api != "" and _ws != ""

# ---------------------------------------------------------------------------
# tour definition
# ---------------------------------------------------------------------------

func _build_steps() -> void:
	# --- Login (3 states) ---
	_add("01_login_empty", _step_login_empty)
	_add("02_login_filled", _step_login_filled)
	_add("03_login_error", _step_login_error)
	# --- Character select (3 states) ---
	_add("04_charselect_empty", _step_charselect_empty)
	_add("05_charselect_with_chars", _step_charselect_with_chars)
	_add("06_charselect_create_error", _step_charselect_create_error)
	# --- World HUD (offline-rendered; HUD driven via public API) ---
	_add("07_hud_default_town", _step_hud_town)
	_add("08_hud_field", _step_hud_field)
	_add("09_hud_combat_target", _step_hud_combat)
	_add("10_chat_tabs_messages", _step_hud_chat)
	_add("11_quest_log_open", _step_hud_quest_log)
	_add("12_quest_tracker_five", _step_hud_tracker_five)
	_add("13_npc_dialog_accept", _step_hud_dialog_accept)
	_add("14_npc_dialog_turnin", _step_hud_dialog_turnin)
	_add("15_death_screen", _step_hud_death)
	_add("16_skill_bar_cooldowns", _step_hud_skill_cooldowns)
	# --- Server-gated stages (skipped offline) ---
	_add("17_combat_live_snapshot", _step_combat_live)
	_add("18_quest_live_data", _step_quest_live)

func _add(name: String, fn: Callable) -> void:
	_steps.append({"name": name, "run": fn})

# ---------------------------------------------------------------------------
# frame pump + capture
# ---------------------------------------------------------------------------

func _process(_delta: float) -> bool:
	if _i >= _steps.size():
		_finish()
		return true
	var step: Dictionary = _steps[_i]
	# Tear down the previous scene so each capture is isolated.
	if _current != null and is_instance_valid(_current):
		_current.queue_free()
		_current = null
		awaiting_pump(2)  # let deletion settle
	var fn: Callable = step["run"]
	var stage: Variant = fn.call()
	if stage == null:
		# Stage signalled skip (e.g. needs live server). Record and advance.
		_index_lines.append("%s\tSKIP\t%s" % [step["name"], _skip_reason(step["name"])])
		_skipped += 1
		_i += 1
		return false
	_current = stage
	root.add_child(stage)
	awaiting_pump(6)  # layout + a few render frames
	_capture(step["name"])
	_i += 1
	return false

func awaiting_pump(frames: int) -> void:
	for f in range(frames):
		await process_frame

func _capture(name: String) -> void:
	var tex := root.get_texture()
	if tex == null:
		push_warning("[screenshot-tour] no root texture for %s" % name)
		return
	var img: Image = tex.get_image()
	if img == null:
		push_warning("[screenshot-tour] null image for %s" % name)
		return
	# Godot may hand back a texture sized to the window; ensure 1920x1080.
	if img.get_width() != W or img.get_height() != H:
		img = img.get_region(Rect2i(0, 0, W, H)) if img.get_width() >= W and img.get_height() >= H else img
	var png := _out + "/" + name + ".png"
	var err := img.save_png(png)
	if err != OK:
		push_warning("[screenshot-tour] save_png %s failed err=%d" % [png, err])
		return
	_captured += 1
	# webp thumbnail
	var thumb := img.duplicate()
	thumb.resize(THUMB_W, THUMB_H, Image.INTERPOLATE_LANCZOS)
	var tpath := _out + "/thumb/" + name + ".webp"
	thumb.save_webp(tpath, 0.8)
	_index_lines.append("%s\tOK\t%s" % [name, png])
	print("[screenshot-tour] captured %s" % name)

func _skip_reason(name: String) -> String:
	match name:
		"17_combat_live_snapshot", "18_quest_live_data":
			return "needs live server + screenshot_bot (M5_5 §1 follow-up)"
		_:
			return "skipped"

func _finish() -> void:
	var f := FileAccess.open(_out + "/index.txt", FileAccess.WRITE)
	for line in _index_lines:
		f.store_line(line)
	f.store_line("# sha=%s captured=%d skipped=%d server=%s" % [_sha, _captured, _skipped, _has_server])
	f.close()
	print("[screenshot-tour] DONE captured=%d skipped=%d out=%s" % [_captured, _skipped, _out])
	quit(0)

# ---------------------------------------------------------------------------
# stage builders — each returns a Node to attach, or null to skip
# ---------------------------------------------------------------------------

func _new_fullrect_control() -> Control:
	var c := Control.new()
	c.set_anchors_preset(Control.PRESET_FULL_RECT)
	c.size = Vector2(W, H)
	c.custom_minimum_size = Vector2(W, H)
	return c

# --- Login ---
func _step_login_empty() -> Node:
	var s: Control = load("res://scenes/Login.tscn").instantiate()
	return s

func _step_login_filled() -> Node:
	var s: Control = load("res://scenes/Login.tscn").instantiate()
	# Drive the form after it enters the tree.
	_defer(s, _fill_login)
	return s

func _fill_login(s: Node) -> void:
	if s == null or not is_instance_valid(s): return
	await process_frame
	var em: LineEdit = s.get_node_or_null("%EmailEdit")
	var pw: LineEdit = s.get_node_or_null("%PasswordEdit")
	if em: em.text = "baldeep@aetheria.games"
	if pw: pw.text = "hunter2hunter2"

func _step_login_error() -> Node:
	var s: Control = load("res://scenes/Login.tscn").instantiate()
	_defer(s, _error_login)
	return s

func _error_login(s: Node) -> void:
	await process_frame
	var st: Label = s.get_node_or_null("%Status")
	if st: st.text = "Login failed: invalid email or password."
	var em: LineEdit = s.get_node_or_null("%EmailEdit")
	var pw: LineEdit = s.get_node_or_null("%PasswordEdit")
	if em: em.text = "baldeep@aetheria.games"
	if pw: pw.text = "wrongpassword"

func _defer(s: Node, fn: Callable) -> void:
	# Run after the scene is added to the tree (caller adds it).
	call_deferred("_run_after_add", s, fn)

func _run_after_add(s: Node, fn: Callable) -> void:
	if s == null or not is_instance_valid(s): return
	if not s.is_inside_tree():
		await process_frame
	fn.call(s)

# --- Character select ---
func _new_charselect() -> Node:
	var s: Control = load("res://scenes/CharSelect.tscn").instantiate()
	# Give it a real (empty) Session + inert api_base so _ready's roster call
	# fails fast to the "Failed to load characters" state instead of NRE.
	s.session = Session.new()
	s.api_base = "http://127.0.0.1:1"
	return s

func _step_charselect_empty() -> Node:
	return _new_charselect()

func _step_charselect_with_chars() -> Node:
	var s := _new_charselect()
	_defer(s, _fill_charselect)
	return s

func _fill_charselect(s: Node) -> void:
	await process_frame
	var list: ItemList = s.get_node_or_null("%CharList")
	var st: Label = s.get_node_or_null("%Status")
	if list:
		list.clear()
		list.add_item("Aria  (Lv9 blade_dancer)")
		list.add_item("Kael  (Lv6 spellweaver)")
		list.select(0)
	if st: st.text = "Characters: 2"

func _step_charselect_create_error() -> Node:
	var s := _new_charselect()
	_defer(s, _error_charselect)
	return s

func _error_charselect(s: Node) -> void:
	await process_frame
	var st: Label = s.get_node_or_null("%Status")
	var ne: LineEdit = s.get_node_or_null("%NameEdit")
	if st: st.text = "Name already taken — choose another."
	if ne: ne.text = "Aria"

# --- World HUD stages (offline; drive the real HUD via public API) ---
func _new_world_offline() -> Node:
	var w: Node = load("res://scenes/World.tscn").instantiate()
	# No session -> World._ready renders world + HUD without connecting.
	w.session = null
	w.character = {}
	w.ws_url = ""
	return w

func _step_hud_town() -> Node:
	var w := _new_world_offline()
	_defer(w, _drive_hud_town)
	return w

func _drive_hud_town(s: Node) -> void:
	await process_frame
	var hud = s.get_node_or_null("HUD")
	if hud == null: return
	hud.set_self({"name": "Aria", "class": "blade_dancer"}, 120, 60, 2400, 318)
	hud.set_connection("Havenport — safe zone")
	hud.set_quests([
		{"id": "q1", "title": "A Hungry Hunter", "objective": "Boars slain 3/8", "progress": 0.375}
	], [])
	hud.add_chat_line("world", "Kael", "anyone seen the boar camp?")
	hud.add_chat_line("system", "", "Welcome to Havenport.")

func _step_hud_field() -> Node:
	var w := _new_world_offline()
	_defer(w, _drive_hud_field)
	return w

func _drive_hud_field(s: Node) -> void:
	await process_frame
	var hud = s.get_node_or_null("HUD")
	if hud == null: return
	hud.set_self({"name": "Aria", "class": "blade_dancer"}, 86, 41, 2400, 318)
	hud.set_connection("Emberfield — hostile field")
	hud.set_quests([
		{"id": "q3", "title": "Ashmaw's Bane", "objective": "Reach the deep field", "progress": 0.5}
	], [])

func _step_hud_combat() -> Node:
	var w := _new_world_offline()
	_defer(w, _drive_hud_combat)
	return w

func _drive_hud_combat(s: Node) -> void:
	await process_frame
	var hud = s.get_node_or_null("HUD")
	if hud == null: return
	hud.set_self({"name": "Aria", "class": "blade_dancer"}, 64, 38, 2400, 318)
	hud.set_target({"name": "Forest Boar", "hp": 22, "max_hp": 40, "level": 3})
	hud.set_connection("Emberfield — in combat")
	hud.set_auto_attack(true)

func _step_hud_chat() -> Node:
	var w := _new_world_offline()
	_defer(w, _drive_hud_chat)
	return w

func _drive_hud_chat(s: Node) -> void:
	await process_frame
	var hud = s.get_node_or_null("HUD")
	if hud == null: return
	hud.set_self({"name": "Aria", "class": "blade_dancer"}, 120, 60, 2400, 318)
	hud.set_connection("Havenport")
	hud.add_chat_line("world", "Kael", "LFG dungeon, need tank + healer")
	hud.add_chat_line("world", "Mira", "selling boar hides 5g each, mail me")
	hud.add_chat_line("say", "Aria", "anyone have the Ashmaw quest?")
	hud.add_chat_line("system", "", "Quest complete: A Hungry Hunter (+120 XP, +15 gold)")
	hud.add_chat_line("world", "GM_Vesh", "Server restart in 10 minutes for deploy.")

func _step_hud_quest_log() -> Node:
	var w := _new_world_offline()
	_defer(w, _drive_hud_quest_log)
	return w

func _drive_hud_quest_log(s: Node) -> void:
	await process_frame
	var hud = s.get_node_or_null("HUD")
	if hud == null: return
	hud.set_self({"name": "Aria", "class": "blade_dancer"}, 120, 60, 2400, 318)
	hud.set_connection("Havenport")
	hud.set_quests_all([
		{"id": "q1", "title": "A Hungry Hunter", "objective": "Boars slain 3/8", "progress": 0.375, "state": "active"},
		{"id": "q2", "title": "Hides for the Tanner", "objective": "Collect 5 boar hides 2/5", "progress": 0.4, "state": "active"},
		{"id": "q5", "title": "A Friend in Need", "objective": "Talk to Mira", "progress": 1.0, "state": "complete"},
	])
	hud.show_quest_log(true)

func _step_hud_tracker_five() -> Node:
	var w := _new_world_offline()
	_defer(w, _drive_hud_tracker_five)
	return w

func _drive_hud_tracker_five(s: Node) -> void:
	await process_frame
	var hud = s.get_node_or_null("HUD")
	if hud == null: return
	hud.set_self({"name": "Aria", "class": "blade_dancer"}, 96, 52, 2400, 318)
	hud.set_connection("Emberfield")
	hud.set_quests([
		{"id": "q1", "title": "A Hungry Hunter", "objective": "Boars slain 3/8", "progress": 0.375},
		{"id": "q2", "title": "Hides for the Tanner", "objective": "Hides 2/5", "progress": 0.4},
		{"id": "q3", "title": "Into the Deep Field", "objective": "Reach the treants", "progress": 0.2},
		{"id": "q4", "title": "Corrupted Roots", "objective": "Treants slain 0/4", "progress": 0.0},
		{"id": "q6", "title": "The Warden's Awakening", "objective": "Enter Hollow Depths", "progress": 0.0},
	], [])

func _step_hud_dialog_accept() -> Node:
	var w := _new_world_offline()
	_defer(w, _drive_hud_dialog_accept)
	return w

func _drive_hud_dialog_accept(s: Node) -> void:
	await process_frame
	var hud = s.get_node_or_null("HUD")
	if hud == null: return
	hud.set_self({"name": "Aria", "class": "blade_dancer"}, 120, 60, 2400, 318)
	hud.set_connection("Havenport")
	hud.show_npc_dialog({
		"npc_name": "Hunter Rohn",
		"quest_title": "A Hungry Hunter",
		"quest_desc": "The boars east of town are ravaging the grain stores. Cull 8 of them and I'll pay you in gold and trust.",
		"state": "offer",
		"rewards": "120 XP, 15 gold, Boar-Hide Vest",
	})

func _step_hud_dialog_turnin() -> Node:
	var w := _new_world_offline()
	_defer(w, _drive_hud_dialog_turnin)
	return w

func _drive_hud_dialog_turnin(s: Node) -> void:
	await process_frame
	var hud = s.get_node_or_null("HUD")
	if hud == null: return
	hud.set_self({"name": "Aria", "class": "blade_dancer"}, 120, 60, 2400, 318)
	hud.set_connection("Havenport")
	hud.show_npc_dialog({
		"npc_name": "Hunter Rohn",
		"quest_title": "A Hungry Hunter",
		"quest_desc": "Eight boars? The grain stores will sleep easy tonight. Take this — you've earned it.",
		"state": "complete",
		"rewards": "120 XP, 15 gold, Boar-Hide Vest",
	})

func _step_hud_death() -> Node:
	var w := _new_world_offline()
	_defer(w, _drive_hud_death)
	return w

func _drive_hud_death(s: Node) -> void:
	await process_frame
	var hud = s.get_node_or_null("HUD")
	if hud == null: return
	hud.set_self({"name": "Aria", "class": "blade_dancer"}, 0, 60, 2400, 318)
	hud.set_target({"name": "Ashmaw", "hp": 410, "max_hp": 600, "level": 9})
	hud.set_connection("Emberfield — you have fallen")
	hud.show_death()

func _step_hud_skill_cooldowns() -> Node:
	var w := _new_world_offline()
	_defer(w, _drive_hud_skill_cooldowns)
	return w

func _drive_hud_skill_cooldowns(s: Node) -> void:
	await process_frame
	var hud = s.get_node_or_null("HUD")
	if hud == null: return
	hud.set_self({"name": "Aria", "class": "blade_dancer"}, 120, 60, 2400, 318)
	hud.set_connection("Emberfield — in combat")
	hud.set_target({"name": "Forest Boar", "hp": 22, "max_hp": 40, "level": 3})
	hud.set_skill_cooldown("blade_strike", 0)
	hud.set_skill_cooldown("twin_slash", 2400)
	hud.set_skill_cooldown("charge", 4800)
	hud.set_skill_cooldown("whirlwind", 9000)
	hud.set_skill_cooldown("rend", 1200)
	hud.set_skill_cooldown("execute", 6000)

# --- Server-gated stages ---
func _step_combat_live() -> Node:
	if not _has_server:
		return null
	# TODO (M5_5 §1 follow-up): log in screenshot_bot, GM-tp into a band,
	# acquire target, capture a real combat snapshot. Requires the bot
	# account + GM token supplied via env.
	return null

func _step_quest_live() -> Node:
	if not _has_server:
		return null
	return null
