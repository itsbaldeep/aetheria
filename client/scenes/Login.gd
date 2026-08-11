extends Control

# Login screen (brief §7: login → character select/create → world).
# Uses ApiClient + Session (headless-testable logic); this node is only the
# form plumbing.

var api: ApiClient
var session: Session

@onready var email_edit: LineEdit = %EmailEdit
@onready var password_edit: LineEdit = %PasswordEdit
@onready var status: Label = %Status
@onready var login_btn: Button = %LoginButton

func _ready() -> void:
	var cfg: ClientConfig = ClientConfig.load_default()
	api = ApiClient.new()
	api.api_base = cfg.api_base
	add_child(api)
	session = Session.new()
	api.login_done.connect(_on_login_done)
	login_btn.pressed.connect(_on_login_pressed)
	password_edit.text_submitted.connect(func(_t): _on_login_pressed())

func _on_login_pressed() -> void:
	var email: String = email_edit.text.strip_edges()
	var pw: String = password_edit.text
	if email.is_empty() or pw.is_empty():
		status.text = "Enter your email and password."
		return
	status.text = "Logging in…"
	login_btn.disabled = true
	api.login(email, pw)

func _on_login_done(ok: bool, body: Dictionary) -> void:
	login_btn.disabled = false
	if not ok:
		status.text = "Login failed: %s" % _friendly(body.get("error", "server_error"))
		return
	session.from_login(body)
	if session.authenticated:
		status.text = "Success — loading characters…"
		_change_to_char_select()
	else:
		status.text = "Login failed: no session returned."

func _change_to_char_select() -> void:
	var scene: Node = load("res://scenes/CharSelect.tscn").instantiate()
	scene.session = session
	scene.api_base = api.api_base
	get_tree().root.add_child(scene)
	queue_free()

func _friendly(e: String) -> String:
	match e:
		"bad_credentials": return "Wrong email or password."
		"account_banned": return "This account is banned."
		"rate_limited": return "Too many attempts. Wait a moment."
	return e
