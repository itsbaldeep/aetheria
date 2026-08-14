extends Control

# Character select/create (brief §7). Lists the roster, creates new
# characters, and hands the picked character to the world (M2). All
# server logic stays in ApiClient/Session; this is form plumbing.

var session: Session
var api_base: String = ""

var api: ApiClient

@onready var char_list: ItemList = %CharList
@onready var status: Label = %Status
@onready var name_edit: LineEdit = %NameEdit
@onready var class_opt: OptionButton = %ClassOption
@onready var create_btn: Button = %CreateButton
@onready var play_btn: Button = %PlayButton

func _ready() -> void:
	api = ApiClient.new()
	api.api_base = api_base
	add_child(api)
	api.roster_done.connect(_on_roster_done)
	api.create_char_done.connect(_on_create_done)
	create_btn.pressed.connect(_on_create_pressed)
	play_btn.pressed.connect(_on_play_pressed)
	char_list.item_selected.connect(func(_i): play_btn.disabled = false)
	_refresh()

func _refresh() -> void:
	status.text = "Loading characters…"
	api.roster(session.token)

func _on_roster_done(ok: bool, body: Dictionary) -> void:
	if not ok:
		status.text = "Failed to load characters."
		return
	session.from_roster(body)
	char_list.clear()
	for c in session.characters:
		char_list.add_item("%s  (Lv%s %s)" % [c.get("name", "?"), c.get("level", 0), c.get("class", "?")])
	status.text = "Characters: %d" % session.characters.size()

func _on_create_pressed() -> void:
	var name: String = name_edit.text.strip_edges()
	var klass: String = class_opt.get_item_text(class_opt.selected)
	if name.is_empty():
		status.text = "Enter a character name."
		return
	create_btn.disabled = true
	status.text = "Creating %s…" % name
	api.create_character(session.token, name, klass)

func _on_create_done(ok: bool, body: Dictionary) -> void:
	create_btn.disabled = false
	if not ok:
		status.text = "Create failed: %s" % body.get("error", "server_error")
		return
	name_edit.text = ""
	_refresh()

func _on_play_pressed() -> void:
	var selected: PackedInt32Array = char_list.get_selected_items()
	if selected.is_empty():
		return
	var idx: int = selected[0]
	var character: Dictionary = session.characters[idx]
	var name: String = str(character.get("name", ""))
	print("[aetheria-client] entering world as %s" % name)
	var cfg := ClientConfig.load_default()
	var scene: Node = load("res://scenes/World.tscn").instantiate()
	scene.session = session
	scene.character = character
	scene.ws_url = cfg.ws_url
	get_tree().root.add_child(scene)
	queue_free()
