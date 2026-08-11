class_name ClientConfig
extends RefCounted

# Client configuration (brief §7: reads server addresses from config.json
# next to the executable so the human can point at localhost or the VPS).
# Loaded once at startup; all session/world code reads from here.

const DEFAULT_JSON := {
	"api_base": "https://api.aetheria.apps.deployden.tech",
	"ws_url": "wss://play.aetheria.apps.deployden.tech/ws",
}

var api_base: String = DEFAULT_JSON.api_base
var ws_url: String = DEFAULT_JSON.ws_url

static func load_file(path: String) -> ClientConfig:
	var cfg := ClientConfig.new()
	if not FileAccess.file_exists(path):
		return cfg
	var txt := FileAccess.get_file_as_string(path)
	if txt.is_empty():
		return cfg
	var parsed: Variant = JSON.parse_string(txt)
	if parsed is Dictionary:
		if parsed.has("api_base") and parsed["api_base"] is String:
			cfg.api_base = parsed["api_base"]
		if parsed.has("ws_url") and parsed["ws_url"] is String:
			cfg.ws_url = parsed["ws_url"]
	return cfg

# Loads config.json sitting next to the executable, falling back to the
# project file (source checkout / editor runs).
static func load_default() -> ClientConfig:
	var exe := OS.get_executable_path()
	var dir := exe.get_base_dir()
	var exe_cfg := dir.path_join("config.json")
	if FileAccess.file_exists(exe_cfg):
		return load_file(exe_cfg)
	return load_file("res://config.json")
