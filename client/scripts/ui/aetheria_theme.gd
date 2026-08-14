class_name AetheriaTheme
extends RefCounted

# Aetheria UI design tokens + programmatic Theme builder. One source of truth
# for the look: night-ember palette (deep indigo + gold), Cinzel display font
# for names/titles, Exo 2 for body. Everything is built in code so the theme
# lives in exactly one place and the .tscn files stay readable.

# --- palette ---
const INK := Color("#0b0e1a")
const PANEL := Color("#151b2e")
const PANEL_DARK := Color("#101426")
const EDGE := Color("#d4af37")
const EDGE_DIM := Color("#8a6d2f")
const TEXT := Color("#edeff4")
const TEXT_DIM := Color("#a7b0c3")
const TEXT_FAINT := Color("#6f7a92")
const HEALTH := Color("#e5484d")
const HEALTH_HI := Color("#f0737a")
const MANA := Color("#4fc3f7")
const XP := Color("#8be08f")
const GOLD := Color("#f0c86a")
const DANGER := Color("#ff5c57")
const ACCENT := Color("#d4af37")
const BLADE := Color("#e86a6a")
const SPELL := Color("#7c8cff")
const MOB_B1 := Color("#9bbd8b")  # band 1 — soft moss
const MOB_B2 := Color("#d8a23a")  # band 2 — ember amber
const MOB_B3 := Color("#e0574f")  # band 3 — forge red
const PLAYER_OTH := Color("#c3c9d8")

const ROUND := 8.0
const ROUND_SM := 5.0

var title_font: Font
var ui_font: Font
var ui_bold_font: Font

func _init() -> void:
	var cinzel := FontFile.new()
	cinzel.load_dynamic_font("res://assets/fonts/Cinzel.ttf")
	cinzel.font_weight = 600
	title_font = cinzel

	var exo := FontFile.new()
	exo.load_dynamic_font("res://assets/fonts/Exo2.ttf")
	exo.font_weight = 400
	ui_font = exo

	var exo_bold := FontFile.new()
	exo_bold.load_dynamic_font("res://assets/fonts/Exo2.ttf")
	exo_bold.font_weight = 600
	ui_bold_font = exo_bold

# Builds the global Theme applied to the game's root Control.
func build() -> Theme:
	var t := Theme.new()
	t.default_font = ui_font
	t.default_font_size = 16

	t.set_color("font_color", "Label", TEXT)
	t.set_color("font_shadow_color", "Label", Color(0, 0, 0, 0.55))
	t.set_constant("shadow_offset_x", "Label", 1)
	t.set_constant("shadow_offset_y", "Label", 1)

	t.set_font("font", "Label", ui_bold_font)
	t.set_font_size("font_size", "Label", 16)

	# Panels
	t.set_stylebox("panel", "PanelContainer", panel(PANEL, EDGE_DIM, 0.35, 0))
	t.set_stylebox("panel", "Panel", panel(PANEL_DARK, EDGE_DIM, 0.25, 0))

	# Buttons
	var b_n := button_style(Color(0.09, 0.12, 0.22, 0.9), EDGE_DIM, 0.45, TEXT_DIM)
	var b_h := button_style(Color(0.13, 0.18, 0.32, 0.95), EDGE, 0.9, TEXT)
	var b_p := button_style(Color(0.16, 0.21, 0.38, 1.0), EDGE, 1.0, TEXT)
	var b_d := button_style(Color(0.06, 0.08, 0.16, 0.6), Color(0.3, 0.33, 0.45, 0.6), 0.2, TEXT_FAINT)
	var b_f := button_style(Color(0.09, 0.12, 0.22, 0.9), EDGE_DIM, 0.6, TEXT)
	b_f.border_width_bottom = 2
	t.set_stylebox("normal", "Button", b_n)
	t.set_stylebox("hover", "Button", b_h)
	t.set_stylebox("pressed", "Button", b_p)
	t.set_stylebox("disabled", "Button", b_d)
	t.set_stylebox("focus", "Button", b_f)
	t.set_color("font_color", "Button", TEXT_DIM)
	t.set_color("font_hover_color", "Button", TEXT)
	t.set_color("font_pressed_color", "Button", TEXT)
	t.set_color("font_disabled_color", "Button", TEXT_FAINT)
	t.set_constant("h_separation", "Button", 10)
	t.set_constant("outline_size", "Button", 0)

	# LineEdit
	t.set_stylebox("normal", "LineEdit", panel(Color(0.05, 0.07, 0.14, 0.9), Color(0.3, 0.33, 0.45, 0.7), 0.4, 0))
	t.set_stylebox("focus", "LineEdit", panel(Color(0.05, 0.07, 0.14, 0.95), EDGE_DIM, 0.8, 0))
	t.set_color("font_color", "LineEdit", TEXT)
	t.set_color("font_placeholder_color", "LineEdit", TEXT_FAINT)
	t.set_constant("minimum_character_width", "LineEdit", 8)

	# ItemList
	t.set_stylebox("panel", "ItemList", panel(Color(0.05, 0.07, 0.14, 0.8), Color(0.3, 0.33, 0.45, 0.4), 0.25, 0))
	t.set_stylebox("focus", "ItemList", StyleBoxEmpty.new())
	t.set_color("font_color", "ItemList", TEXT)
	t.set_color("font_selected_color", "ItemList", TEXT)
	t.set_color("font_hovered_color", "ItemList", TEXT)
	t.set_color("font_outline_color", "ItemList", Color.TRANSPARENT)
	t.set_stylebox("selected", "ItemList", button_style(Color(0.16, 0.21, 0.38, 0.9), EDGE_DIM, 0.7, TEXT))
	t.set_stylebox("cursor", "ItemList", StyleBoxEmpty.new())

	# OptionButton (reuse button styles)
	t.set_stylebox("normal", "OptionButton", b_n)
	t.set_stylebox("hover", "OptionButton", b_h)
	t.set_stylebox("pressed", "OptionButton", b_p)
	t.set_stylebox("focus", "OptionButton", b_f)
	t.set_color("font_color", "OptionButton", TEXT)

	# ProgressBar (HP/MP/XP bars)
	t.set_stylebox("background", "ProgressBar", bar_bg())
	t.set_stylebox("fill", "ProgressBar", bar_fill(HEALTH))
	t.set_stylebox("background_fg", "ProgressBar", StyleBoxEmpty.new())
	t.set_constant("separation", "ProgressBar", 0)

	# RichTextLabel
	t.set_color("default_color", "RichTextLabel", TEXT)

	# ScrollBar (thin gold)
	var sb_g := StyleBoxFlat.new()
	sb_g.bg_color = Color(0.3, 0.33, 0.45, 0.5)
	sb_g.set_corner_radius_all(4)
	t.set_stylebox("grabber", "VScrollBar", sb_g)
	t.set_stylebox("scroll", "VScrollBar", StyleBoxEmpty.new())
	t.set_stylebox("grabber", "HScrollBar", sb_g)
	t.set_stylebox("scroll", "HScrollBar", StyleBoxEmpty.new())

	# Tabs / TabBar
	t.set_stylebox("tab_unselected", "TabContainer", panel(Color(0.05, 0.07, 0.14, 0.7), Color(0.3, 0.33, 0.45, 0.3), 0.2, 0))
	t.set_stylebox("tab_selected", "TabContainer", panel(PANEL, EDGE_DIM, 0.6, 0))
	t.set_stylebox("panel", "TabContainer", panel(PANEL, EDGE_DIM, 0.35, 0))

	# Tooltip
	t.set_stylebox("panel", "TooltipPanel", panel(Color(0.05, 0.07, 0.14, 0.96), EDGE_DIM, 0.8, 0))

	# ScrollContainer transparent
	t.set_stylebox("panel", "ScrollContainer", StyleBoxEmpty.new())
	return t

# --- stylebox factories ---

func panel(bg: Color, border: Color, border_alpha: float, shadow: int) -> StyleBoxFlat:
	var sb := StyleBoxFlat.new()
	sb.bg_color = bg
	sb.border_color = border
	sb.border_color.a = border_alpha
	sb.set_border_width_all(1)
	sb.set_corner_radius_all(ROUND)
	if shadow > 0:
		sb.shadow_color = Color(0, 0, 0, 0.35)
		sb.shadow_size = shadow
		sb.shadow_offset = Vector2(0, 2)
	return sb

func button_style(bg: Color, border: Color, border_alpha: float, font_color: Color) -> StyleBoxFlat:
	var sb := StyleBoxFlat.new()
	sb.bg_color = bg
	sb.border_color = border
	sb.border_color.a = border_alpha
	sb.set_border_width_all(1)
	sb.set_corner_radius_all(ROUND_SM)
	sb.content_margin_left = 14
	sb.content_margin_right = 14
	sb.content_margin_top = 7
	sb.content_margin_bottom = 7
	sb.expand_margin_top = 1
	sb.expand_margin_bottom = 1
	return sb

func bar_bg() -> StyleBoxFlat:
	var sb := StyleBoxFlat.new()
	sb.bg_color = Color(0.03, 0.05, 0.10, 0.85)
	sb.border_color = Color(0, 0, 0, 0.6)
	sb.set_border_width_all(1)
	sb.set_corner_radius_all(4)
	return sb

func bar_fill(color: Color) -> StyleBoxFlat:
	var sb := StyleBoxFlat.new()
	sb.bg_color = color
	sb.set_corner_radius_all(3)
	return sb

# --- convenience API for HUD code ---

func panel_container(style: StyleBoxFlat) -> PanelContainer:
	var pc := PanelContainer.new()
	pc.add_theme_stylebox_override("panel", style)
	return pc

func styled_label(text: String, font: Font, size: int, color: Color) -> Label:
	var l := Label.new()
	l.text = text
	l.add_theme_font_override("font", font)
	l.add_theme_font_size_override("font_size", size)
	l.add_theme_color_override("font_color", color)
	return l

func bar(name: String, color: Color) -> ProgressBar:
	var b := ProgressBar.new()
	b.name = name
	b.custom_minimum_size = Vector2(0, 14)
	b.show_percentage = false
	b.add_theme_stylebox_override("background", bar_bg())
	b.add_theme_stylebox_override("fill", bar_fill(color))
	b.value = 0
	return b
