package overlay

import (
	"log"
	"math"
	"sync"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"

	"gocrosshair/config"
)

// InputState represents the current player action state.
type InputState int

const (
	StateIdle InputState = iota
	StateWalking
	StateSprinting
	StateScoped
)

// Key symbols for input detection.
const (
	keysymW      xproto.Keysym = 0x0077
	keysymA      xproto.Keysym = 0x0061
	keysymS      xproto.Keysym = 0x0073
	keysymD      xproto.Keysym = 0x0064
	keysymShiftL xproto.Keysym = 0xFFE1
)

// DynamicSizer monitors keyboard/mouse state and calculates the current crosshair size
// with smooth transitions.
type DynamicSizer struct {
	conn   *xgb.Conn
	screen *xproto.ScreenInfo
	config *config.Config

	// Keycodes resolved at init
	keycodeW      xproto.Keycode
	keycodeA      xproto.Keycode
	keycodeS      xproto.Keycode
	keycodeD      xproto.Keycode
	keycodeShiftL xproto.Keycode

	// Current interpolated size (float for smooth transitions)
	mu          sync.Mutex
	currentSize float64
	targetSize  int
	running     bool
	stopCh      chan struct{}
}

// NewDynamicSizer creates a new dynamic sizer that monitors input state.
func NewDynamicSizer(conn *xgb.Conn, screen *xproto.ScreenInfo, cfg *config.Config) *DynamicSizer {
	ds := &DynamicSizer{
		conn:        conn,
		screen:      screen,
		config:      cfg,
		currentSize: float64(cfg.Crosshair.Size),
		targetSize:  cfg.Crosshair.Size,
		stopCh:      make(chan struct{}),
	}

	// Resolve keycodes for WASD and Shift
	ds.keycodeW = getKeysymKeycode(conn, screen, keysymW)
	ds.keycodeA = getKeysymKeycode(conn, screen, keysymA)
	ds.keycodeS = getKeysymKeycode(conn, screen, keysymS)
	ds.keycodeD = getKeysymKeycode(conn, screen, keysymD)
	ds.keycodeShiftL = getKeysymKeycode(conn, screen, keysymShiftL)

	if ds.keycodeW == 0 || ds.keycodeA == 0 || ds.keycodeS == 0 || ds.keycodeD == 0 {
		log.Println("Warning: Could not resolve all WASD keycodes for dynamic crosshair")
	}

	return ds
}

// Start begins the input polling loop.
func (ds *DynamicSizer) Start() {
	ds.running = true
	go ds.pollLoop()
}

// Stop terminates the polling loop.
func (ds *DynamicSizer) Stop() {
	if ds.running {
		ds.running = false
		close(ds.stopCh)
	}
}

// CurrentSize returns the current interpolated crosshair size.
func (ds *DynamicSizer) CurrentSize() int {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return int(math.Round(ds.currentSize))
}

// pollLoop polls the keyboard and mouse state and updates the target size.
func (ds *DynamicSizer) pollLoop() {
	ticker := time.NewTicker(16 * time.Millisecond) // ~60fps
	defer ticker.Stop()

	for {
		select {
		case <-ds.stopCh:
			return
		case <-ticker.C:
			state := ds.queryInputState()
			ds.updateSize(state)
		}
	}
}

// queryInputState queries the current keyboard and mouse state from X11.
func (ds *DynamicSizer) queryInputState() InputState {
	cfg := ds.config.Dynamic

	// Check for RMB (scoping) first — it takes priority
	if cfg.WhileScoped {
		if ds.isMouseButtonPressed(3) { // Button 3 = RMB
			return StateScoped
		}
	}

	// Check for movement keys (WASD)
	if cfg.WhileMove {
		moving := ds.isKeyPressed(ds.keycodeW) ||
			ds.isKeyPressed(ds.keycodeA) ||
			ds.isKeyPressed(ds.keycodeS) ||
			ds.isKeyPressed(ds.keycodeD)

		if moving {
			// Check if sprinting (Shift held while moving)
			if ds.isKeyPressed(ds.keycodeShiftL) {
				return StateSprinting
			}
			return StateWalking
		}
	}

	return StateIdle
}

// isKeyPressed checks if a key is currently pressed using XQueryKeymap.
func (ds *DynamicSizer) isKeyPressed(keycode xproto.Keycode) bool {
	if keycode == 0 {
		return false
	}

	reply, err := xproto.QueryKeymap(ds.conn).Reply()
	if err != nil {
		return false
	}

	// The keymap is a 32-byte array where each bit represents a key
	byteIndex := int(keycode) / 8
	bitIndex := uint(keycode) % 8

	if byteIndex >= len(reply.Keys) {
		return false
	}

	return reply.Keys[byteIndex]&(1<<bitIndex) != 0
}

// isMouseButtonPressed checks if a mouse button is currently pressed.
func (ds *DynamicSizer) isMouseButtonPressed(button byte) bool {
	reply, err := xproto.QueryPointer(ds.conn, ds.screen.Root).Reply()
	if err != nil {
		return false
	}

	// Button masks: Button1 = bit 8, Button2 = bit 9, Button3 = bit 10, etc.
	switch button {
	case 1:
		return reply.Mask&xproto.ButtonMask1 != 0
	case 2:
		return reply.Mask&xproto.ButtonMask2 != 0
	case 3:
		return reply.Mask&xproto.ButtonMask3 != 0
	default:
		return false
	}
}

// updateSize smoothly transitions the current size towards the target size.
func (ds *DynamicSizer) updateSize(state InputState) {
	cfg := ds.config.Dynamic
	baseSize := ds.config.Crosshair.Size

	// Determine target size based on state
	var target int
	switch state {
	case StateWalking:
		target = cfg.WalkingSize
	case StateSprinting:
		target = cfg.SprintSize
	case StateScoped:
		target = cfg.ScopeSize
	default:
		target = baseSize
	}

	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.targetSize = target

	// Smooth interpolation using transition speed
	speed := cfg.TransitionSpeed
	if speed <= 0 {
		// Instant transition
		ds.currentSize = float64(target)
		return
	}

	// Lerp: current = current + (target - current) * (1 - speed)
	// Higher speed = slower transition (more smoothing)
	factor := 1.0 - speed
	if factor < 0.01 {
		factor = 0.01
	}

	diff := float64(target) - ds.currentSize
	if math.Abs(diff) < 0.5 {
		ds.currentSize = float64(target)
	} else {
		ds.currentSize += diff * factor
	}
}
