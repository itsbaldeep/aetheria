extends Node

# Boot scene (M0/M1): logs the client version, loads client config, and
# hands off to the login flow. Kept minimal so `godot --headless` boots
# clean and a test can override the target scene.

func _ready() -> void:
	print("[aetheria-client] booted, engine=%s protocol=%s" % [Engine.get_version_info().string, "0.1.0"])
	var cfg: ClientConfig = ClientConfig.load_default()
	print("[aetheria-client] api=%s ws=%s" % [cfg.api_base, cfg.ws_url])
	var login: Node = load("res://scenes/Login.tscn").instantiate()
	# The root is still busy setting up children during the main scene's _ready;
	# a direct add_child here fails silently (blank window). Defer it.
	get_tree().root.add_child.call_deferred(login)
	queue_free()
