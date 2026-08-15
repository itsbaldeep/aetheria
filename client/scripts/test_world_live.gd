extends SceneTree

# Live integration test (client-side): drives ApiClient (register → login →
# create char) then WorldSession (Bearer WS handshake → ServerHello →
# EnterWorldAck) against a real gameserver — exactly the path the World scene
# uses. Guards the WebSocket connect (headers + no-subprotocol) in CI/dev.
#
# Run: godot --headless --path . --script res://scripts/test_world_live.gd -- \
#      --api http://127.0.0.1:3016 --ws ws://127.0.0.1:3020/ws
# Skip-safe: without --ws the test exits PASS with a note (CI stays green).

var _api_base := ""
var _ws_url := ""
var _api: Node
var _session: Session
var _world: WorldSession
var _step := "register"
var _failures := 0
var _stamp := ""
var _char_id := 0
var _token := ""

func _init() -> void:
	_stamp = Time.get_datetime_string_from_system(true).replace(":", "").replace("T", "").replace("-", "")
	var args := OS.get_cmdline_user_args()
	for i in range(args.size()):
		if args[i] == "--api" and i + 1 < args.size():
			_api_base = args[i + 1]
		if args[i] == "--ws" and i + 1 < args.size():
			_ws_url = args[i + 1]
	if _ws_url.is_empty() or _api_base.is_empty():
		print("test_world_live: SKIP (no --api/--ws given; offline CI safe)")
		quit(0)
		return

	_api = preload("res://scripts/api_client.gd").new()
	_api.api_base = _api_base
	root.add_child(_api)
	_session = Session.new()
	_api.register_done.connect(_on_register)
	_api.login_done.connect(_on_login)
	_api.roster_done.connect(_on_roster)
	_api.create_char_done.connect(_on_create)
	# Defer so ApiClient._ready runs before request().
	call_deferred("_go_register")

func _go_register() -> void:
	_api.register("live-%s@aetheria.test" % _stamp, "live-pass-77")

func _on_register(ok: bool, body: Dictionary) -> void:
	if not ok:
		printerr("register failed: %s" % body)
		_failures += 1
		_quit()
		return
	_api.login(_api_registered_email(), "live-pass-77")

func _api_registered_email() -> String:
	return "live-%s@aetheria.test" % _stamp

func _on_login(ok: bool, body: Dictionary) -> void:
	if not ok or str(body.get("token", "")).is_empty():
		printerr("login failed: %s" % body)
		_failures += 1
		_quit()
		return
	_session.from_login(body)
	_api.roster(_session.token)

func _on_roster(ok: bool, body: Dictionary) -> void:
	if not ok:
		printerr("roster failed: %s" % body)
		_failures += 1
		_quit()
		return
	_session.from_roster(body)
	if _session.characters.is_empty():
		_api.create_character(_session.token, "Live%s" % _stamp.right(4), "blade_dancer")
	else:
		_finish_setup(int(_session.characters[0].get("id", 0)), _session.token)

func _on_create(ok: bool, body: Dictionary) -> void:
	if not ok:
		printerr("create failed: %s" % body)
		_failures += 1
		_quit()
		return
	_api.roster(_session.token)

func _finish_setup(char_id: int, token: String) -> void:
	_char_id = char_id
	_token = token
	_world = WorldSession.new()
	_world.name = "WorldSession"
	root.add_child(_world)
	_world.server_hello.connect(func(h): _log("ServerHello %s" % h))
	_world.entered_world.connect(_on_entered)
	_world.disconnected.connect(_on_disconnect)
	_world.connect_to_server(_ws_url, _token, _char_id)

func _log(s: String) -> void:
	print("test_world_live: %s" % s)

func _on_entered(ack: Dictionary) -> void:
	if not ack.get("ok", false):
		printerr("EnterWorld rejected: %s" % ack)
		_failures += 1
		_quit()
		return
	_log("EnterWorldAck ok, self_id=%d zone=%s" % [int(ack.get("entity_id", 0)), str(ack.get("zone_id", "?"))])
	if int(ack.get("entity_id", 0)) <= 0:
		printerr("no entity id in ack")
		_failures += 1
		_quit()
		return
	_log("PASS — WS handshake + EnterWorld round-trip OK")
	if _failures == 0:
		print("test_world_live: ALL PASS")
	quit(0 if _failures == 0 else 1)

func _on_disconnect(reason: String, _code: int) -> void:
	printerr("WS disconnected before EnterWorld: %s" % reason)
	_failures += 1
	_quit()

func _quit() -> void:
	if _failures == 0:
		print("test_world_live: ALL PASS")
	else:
		printerr("test_world_live: %d FAILURES" % _failures)
	quit(0 if _failures == 0 else 1)
