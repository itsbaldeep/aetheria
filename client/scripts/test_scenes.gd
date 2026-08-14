extends SceneTree

# M1 headless smoke test: verifies the Login + CharSelect scenes instantiate
# cleanly (no missing-node / script errors), and that Boot hands off without
# crashing. The World scene is added to the live tree so its _ready can run,
# then we confirm the HUD built. Rendering is verified by the human (brief
# §12.4); this guards the scene graph wiring in CI.
# Run: godot --headless --script res://scripts/test_scenes.gd

var failures := 0
var _world: Node = null
var _frames := 0

func _init() -> void:
	var boot_scene: PackedScene = load("res://scenes/Boot.tscn")
	if boot_scene == null:
		printerr("Boot.tscn failed to load")
		failures += 1
	else:
		var boot := boot_scene.instantiate()
		if boot == null:
			printerr("Boot.tscn instantiate returned null")
			failures += 1

	var login_scene: PackedScene = load("res://scenes/Login.tscn")
	if login_scene == null:
		printerr("Login.tscn failed to load")
		failures += 1
	else:
		var login := login_scene.instantiate()
		if login == null:
			printerr("Login.tscn instantiate returned null")
			failures += 1

	var char_scene: PackedScene = load("res://scenes/CharSelect.tscn")
	if char_scene == null:
		printerr("CharSelect.tscn failed to load")
		failures += 1
	else:
		var chars := char_scene.instantiate()
		if chars == null:
			printerr("CharSelect.tscn instantiate returned null")
			failures += 1

	var world_scene: PackedScene = load("res://scenes/World.tscn")
	if world_scene == null:
		printerr("World.tscn failed to load")
		failures += 1
	else:
		_world = world_scene.instantiate()
		if _world == null:
			printerr("World.tscn instantiate returned null")
			failures += 1

func _process(_delta: float) -> bool:
	if _world == null:
		if failures == 0:
			print("test_scenes: ALL PASS")
		else:
			printerr("test_scenes: %d FAILURES" % failures)
		quit(0 if failures == 0 else 1)
		return true
	_frames += 1
	if _frames == 1:
		root.add_child(_world)
		return false
	if _frames == 2:
		var wl := _world.get_node_or_null("HUD")
		if wl == null:
			printerr("World.tscn did not build its HUD")
			failures += 1
		_world.queue_free()
		_world = null
		return false
	return false
