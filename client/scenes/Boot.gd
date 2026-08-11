extends Node

# M0: empty boot scene. Logs the client version and hands off to the login
# flow (M1). Kept deliberately minimal so `godot --headless` boots clean.

func _ready() -> void:
	print("[aetheria-client] booted, engine=%s protocol=%s" % [Engine.get_version_info().string, "0.1.0"])
