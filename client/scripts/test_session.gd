extends SceneTree

# M1 headless test for client auth/session logic (brief §7: session logic
# lives decoupled from rendering so godot --headless can run it in CI).
# Tests ClientConfig + Session parsing, NOT live network (that's bottest).
# Run: godot --headless --script res://scripts/test_session.gd


func _init() -> void:
	var failures := 0
	failures += _test_config_defaults()
	failures += _test_session_login()
	failures += _test_session_roster()
	if failures == 0:
		print("test_session: ALL PASS")
		quit(0)
	else:
		printerr("test_session: %d FAILURES" % failures)
		quit(1)

func _test_config_defaults() -> int:
	var cfg := ClientConfig.new()
	# Defaults point at the production domain (brief §7).
	if not cfg.api_base.begins_with("https://"):
		printerr("config api_base default not https: %s" % cfg.api_base)
		return 1
	if not cfg.ws_url.begins_with("wss://"):
		printerr("config ws_url default not wss: %s" % cfg.ws_url)
		return 1
	return 0

func _test_session_login() -> int:
	var s: Variant = Session.new()
	if s.authenticated:
		printerr("fresh session must not be authenticated")
		return 1
	s.from_login({"id": 99, "token": "abc.def.ghi", "expires_at": "2026-08-12T00:00:00Z"})
	if not s.authenticated:
		printerr("session from login must be authenticated")
		return 1
	if s.account_id != 99 or s.token != "abc.def.ghi":
		printerr("session fields not parsed: %d %s" % [s.account_id, s.token])
		return 1
	return 0

func _test_session_roster() -> int:
	var s: Variant = Session.new()
	s.from_login({"id": 1, "token": "tok", "expires_at": ""})
	s.from_roster({
		"characters": [
			{"id": 1, "name": "Aria", "class": "blade_dancer", "level": 1, "zone_id": "havenport"},
			{"id": 2, "name": "Nova", "class": "spellweaver", "level": 1, "zone_id": "havenport"},
		]
	})
	if s.characters.size() != 2:
		printerr("roster size = %d want 2" % s.characters.size())
		return 1
	if not s.has_character("Aria") or s.character_by_name("Nova").get("class", "") != "spellweaver":
		printerr("roster lookup failed")
		return 1
	if s.has_character("Ghost"):
		printerr("roster lookup found a ghost")
		return 1
	return 0
