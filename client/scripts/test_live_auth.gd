extends SceneTree

# M1 live integration test (client-side): drives ApiClient + Session against
# a real authserver. Requires a fresh account, so it registers one first,
# then logs in and lists characters — exercising the exact Godot code path
# the Login/CharSelect scenes use (brief §7 headless-testability).
#
# Run: godot --headless --script res://scripts/test_live_auth.gd -- \
#      --api https://api.aetheria.apps.deployden.tech
#
# Skip-safe: without --api this test exits PASS with a note (CI stays green
# offline); the Go bot scenarios own the authoritative live assertions.

const AuthApi = preload("res://scripts/api_client.gd")

var _api_base: String = ""
var _api: Node
var _step: String = "register"
var _failures: int = 0
var _stamp: String = ""

func _init() -> void:
	_stamp = Time.get_datetime_string_from_system(true).replace(":", "").replace("T", "").replace("-", "")
	var args := OS.get_cmdline_user_args()
	for i in range(args.size()):
		if args[i] == "--api" and i + 1 < args.size():
			_api_base = args[i + 1]
	if _api_base.is_empty():
		print("test_live_auth: SKIP (no --api given; offline CI safe)")
		quit(0)
		var _unused := _step
		return

	_api = AuthApi.new()
	_api.api_base = _api_base
	root.add_child(_api)
	_api.register_done.connect(_on_register)
	_api.login_done.connect(_on_login)
	_api.roster_done.connect(_on_roster)
	# Defer: ApiClient._ready() must run before request() is valid.
	_api.call_deferred("register", "godot-live-" + _stamp + "@aetheria.test", "live-pass-42")

func _on_register(ok: bool, body: Dictionary) -> void:
	if not ok:
		printerr("register failed: %s" % str(body))
		_finish(1)
		return
	_step = "login"
	_api.login("godot-live-" + _stamp + "@aetheria.test", "live-pass-42")

func _on_login(ok: bool, body: Dictionary) -> void:
	if not ok or not body.has("token"):
		printerr("login failed: %s" % str(body))
		_finish(1)
		return
	_step = "roster"
	_api.roster(str(body["token"]))

func _on_roster(ok: bool, body: Dictionary) -> void:
	if not ok or not body.has("characters"):
		printerr("roster failed: %s" % str(body))
		_finish(1)
		return
	print("test_live_auth: ALL PASS (register->login->roster via Godot ApiClient)")
	_finish(0)

func _finish(rc: int) -> void:
	_api.queue_free()
	quit(rc)
