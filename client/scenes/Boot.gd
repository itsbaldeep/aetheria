extends Node

# Boot scene (M0/M1): logs the client version, loads client config, and
# hands off to the login flow. Kept minimal so `godot --headless` boots
# clean and a test can override the target scene.

func _ready() -> void:
	print("[aetheria-client] booted, engine=%s protocol=%s" % [Engine.get_version_info().string, "0.1.0"])
	var cfg: ClientConfig = ClientConfig.load_default()
	print("[aetheria-client] api=%s ws=%s" % [cfg.api_base, cfg.ws_url])
	var login: Node = load("res://scenes/Login.tscn").instantiate()
	get_tree().root.add_child(login)
	queue_free()
