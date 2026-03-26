// Package config handles configuration loading, saving, and validation for gocrosshair.
package config

// Default configuration values.
const (
	DefaultShape            = "cross"
	DefaultColor            = "#00FF00"
	DefaultInvertedColor    = false
	DefaultOpacity          = 0.8
	DefaultSize             = 9
	DefaultThickness        = 2
	DefaultGap              = 0
	DefaultOutlineThickness = 1
	DefaultOutlineColor     = "#000000"
	DefaultMonitor          = 0
	DefaultOffsetX          = 0
	DefaultOffsetY          = 0

	// Dynamic defaults
	DefaultDynamicEnabled         = true
	DefaultDynamicWhileMove       = true
	DefaultDynamicWhileScoped     = true
	DefaultDynamicWalkingSize     = 12
	DefaultDynamicSprintSize      = 15
	DefaultDynamicScopeSize       = 6
	DefaultDynamicTransitionSpeed = 0.15
)

// Valid shape options.
var ValidShapes = []string{"cross", "dot", "circle", "cross-dot", "caret"}

// Default returns a new Config with default values.
func Default() *Config {
	return &Config{
		Crosshair: CrosshairConfig{
			Shape:            DefaultShape,
			Color:            DefaultColor,
			InvertedColor:    DefaultInvertedColor,
			Opacity:          DefaultOpacity,
			Size:             DefaultSize,
			Thickness:        DefaultThickness,
			Gap:              DefaultGap,
			OutlineThickness: DefaultOutlineThickness,
			OutlineColor:     DefaultOutlineColor,
		},
		Dynamic: DynamicConfig{
			Enabled:         DefaultDynamicEnabled,
			WhileMove:       DefaultDynamicWhileMove,
			WhileScoped:     DefaultDynamicWhileScoped,
			WalkingSize:     DefaultDynamicWalkingSize,
			SprintSize:      DefaultDynamicSprintSize,
			ScopeSize:       DefaultDynamicScopeSize,
			TransitionSpeed: DefaultDynamicTransitionSpeed,
		},
		Position: PositionConfig{
			Monitor: DefaultMonitor,
			OffsetX: DefaultOffsetX,
			OffsetY: DefaultOffsetY,
		},
	}
}

// DefaultConfigContent returns the default configuration as a TOML string with comments.
func DefaultConfigContent() string {
	return `# ====================================================================
# gocrosshair - Production Ready Configuration
# ====================================================================
# This file controls the on-screen crosshair overlay.
# Changes take effect immediately after saving (if the app is running).
#
# Hotkeys (hardcoded):
#   Ctrl + Shift + H   → Toggle crosshair visibility
# ====================================================================

# --------------------------------------------------------------------
# [crosshair] - Visual appearance
# --------------------------------------------------------------------
[crosshair]

# Shape of the crosshair.
# Valid values: cross, dot, circle, cross-dot, caret
shape = "cross"

# Color in hex format (#RRGGBB, 0xRRGGBB, or RRGGBB).
color = "#00FF00"

# Invert crosshair color using XOR blending (useful for high contrast).
inverted_color = false

# Opacity (0.0 = fully transparent, 1.0 = fully opaque).
# May require a compositor to work properly.
opacity = 0.8

# Base size of the crosshair in pixels.
# - For "cross": arm length from center.
# - For "dot": radius.
size = 9

# Thickness of the crosshair lines (pixels).
thickness = 2

# Gap between the center and the start of each arm (pixels).
# Set >0 to create a hollow cross.
gap = 0

# Outline thickness (pixels). Set to 0 to disable outline.
outline_thickness = 1

# Outline color (hex format).
outline_color = "#000000"

# --------------------------------------------------------------------
# [dynamic] - Dynamic resizing based on player actions
# --------------------------------------------------------------------
[dynamic]

# Master switch for dynamic behaviour.
enabled = true

# Resize when moving (walking or sprinting).
while_move = true

# Resize when scoped (aiming down sights).
while_scoped = true

# Size while walking (normal movement).
walking_size = 12

# Size while sprinting (e.g., holding Shift).
sprint_size = 15

# Size while scoped (aiming).
scope_size = 6

# Smoothing factor for size transitions (0.0 = instant, 1.0 = very slow).
transition_speed = 0.15

# --- Hardcoded input bindings (cannot be changed in this file) ---
# Walking   : W, A, S, D keys
# Sprinting : Left Shift (while moving)
# Scoping   : Right Mouse Button (RMB)
# --------------------------------------------------------------------

# --------------------------------------------------------------------
# [position] - Screen placement
# --------------------------------------------------------------------
[position]

# Monitor to display the crosshair on.
#   0,1,2,…   → monitors
#   -1        → primary monitor (Windows: main display; Linux: usually first)
monitor = 0

# Offset from the monitor's center (pixels).
# Positive X = right, Positive Y = down.
offset_x = 0
offset_y = 0
`
}
