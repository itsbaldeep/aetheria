class_name ApiClient
extends Node

# Thin HTTP client over the authserver API. One HTTPRequest child handles a
# single in-flight call at a time; callers wire into `completed` signals.
# Decoupled from any UI so a headless test can drive it directly.

signal login_done(ok: bool, body: Dictionary)
signal roster_done(ok: bool, body: Dictionary)
signal create_char_done(ok: bool, body: Dictionary)
signal register_done(ok: bool, body: Dictionary)

var api_base: String = ""

var _req: HTTPRequest

func _ready() -> void:
	_req = HTTPRequest.new()
	add_child(_req)
	_req.timeout = 10.0
	_req.request_completed.connect(_on_completed)

func _make_url(path: String) -> String:
	return api_base.trim_suffix("/") + path

func login(email: String, password: String) -> void:
	_tag = &"login_done"
	var body := JSON.stringify({"email": email, "password": password})
	_req.request(_make_url("/auth/login"), ["Content-Type: application/json"], HTTPClient.METHOD_POST, body)

func roster(token: String) -> void:
	_tag = &"roster_done"
	_req.request(_make_url("/auth/characters"), _auth_headers(token), HTTPClient.METHOD_GET)

func create_character(token: String, name: String, klass: String) -> void:
	_tag = &"create_char_done"
	var body := JSON.stringify({"name": name, "class": klass})
	_req.request(_make_url("/auth/characters/create"), _auth_headers(token), HTTPClient.METHOD_POST, body)

func register(email: String, password: String) -> void:
	_tag = &"register_done"
	var body := JSON.stringify({"email": email, "password": password})
	_req.request(_make_url("/auth/register"), ["Content-Type: application/json"], HTTPClient.METHOD_POST, body)

func _auth_headers(token: String) -> PackedStringArray:
	return PackedStringArray(["Content-Type: application/json", "Authorization: Bearer " + token])

func _on_completed(result: int, code: int, _headers: PackedStringArray, body: PackedByteArray) -> void:
	var json: Variant = JSON.parse_string(body.get_string_from_utf8())
	var dict: Dictionary = json if json is Dictionary else {}
	# A successful HTTP exchange may still be an application error (400/401/
	# 409). Treat "ok" as 2xx, so callers check body["error"] too.
	emit_signal(_tag, code >= 200 and code < 300, dict)

var _tag: StringName = &"login_done"
