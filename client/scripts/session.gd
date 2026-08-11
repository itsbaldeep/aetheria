class_name Session
extends RefCounted

# Session state for the logged-in account: token, expiry, and the character
# roster. Pure data + helpers, decoupled from rendering so it is testable in
# `godot --headless` (brief §7). The ApiClient fills this via server JSON.

var token: String = ""
var account_id: int = 0
var expires_at: String = ""
var characters: Array = []

var authenticated: bool:
	get: return token != ""

# Fills session from the /auth/login response body.
func from_login(body: Dictionary) -> void:
	token = str(body.get("token", ""))
	account_id = int(body.get("id", 0))
	expires_at = str(body.get("expires_at", ""))

# Fills the roster from /auth/characters response body.
func from_roster(body: Dictionary) -> void:
	characters = body.get("characters", [])

func character_by_name(name: String) -> Dictionary:
	for c in characters:
		if str(c.get("name", "")) == name:
			return c
	return {}

func has_character(name: String) -> bool:
	return not character_by_name(name).is_empty()
