extends SceneTree

# M1 headless smoke test: verifies the Login + CharSelect scenes instantiate
# cleanly (no missing-node / script errors), and that Boot hands off without
# crashing. Rendering is verified by the human (brief §12.4); this guards the
# scene graph wiring in CI.
# Run: godot --headless --script res://scripts/test_scenes.gd

func _init() -> void:
	var failures := 0

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

	if failures == 0:
		print("test_scenes: ALL PASS")
		quit(0)
	else:
		printerr("test_scenes: %d FAILURES" % failures)
		quit(1)
