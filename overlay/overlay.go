package overlay

import (
	"fmt"
	"log"
	"math"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/shape"
	"github.com/jezek/xgb/xproto"

	"gocrosshair/config"
)

// Overlay manages the X11 crosshair overlay window.
type Overlay struct {
	conn      *xgb.Conn
	screen    *xproto.ScreenInfo
	windowID  xproto.Window
	gcID      xproto.Gcontext
	outlineGC xproto.Gcontext
	config    *config.Config
	monitor   Monitor
	centerX   int16
	centerY   int16
	visible   bool

	// Dynamic resizing state
	dynamic     *DynamicSizer
	currentSize int
}

// NewOverlay creates a new crosshair overlay connected to the X server.
func NewOverlay(cfg *config.Config) (*Overlay, error) {
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to X server: %w", err)
	}

	// Initialize the shape extension for click-through functionality.
	if err := shape.Init(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to initialize shape extension: %w", err)
	}

	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)

	monitors, err := GetMonitors(conn, screen)
	if err != nil {
		log.Printf("Warning: failed to get monitors: %v, using screen dimensions", err)
		monitors = []Monitor{{
			Name:    "default",
			X:       0,
			Y:       0,
			Width:   screen.WidthInPixels,
			Height:  screen.HeightInPixels,
			Primary: true,
		}}
	}

	monitor := SelectMonitor(monitors, cfg.Position.Monitor)

	centerX := monitor.CenterX() + int16(cfg.Position.OffsetX)
	centerY := monitor.CenterY() + int16(cfg.Position.OffsetY)

	o := &Overlay{
		conn:        conn,
		screen:      screen,
		config:      cfg,
		monitor:     monitor,
		centerX:     centerX,
		centerY:     centerY,
		visible:     true,
		currentSize: cfg.Crosshair.Size,
	}

	// Initialize dynamic sizer if enabled
	if cfg.Dynamic.Enabled {
		o.dynamic = NewDynamicSizer(conn, screen, cfg)
	}

	return o, nil
}

// Close releases X server resources and closes the connection.
func (o *Overlay) Close() {
	if o.dynamic != nil {
		o.dynamic.Stop()
	}
	if o.conn != nil {
		o.conn.Close()
	}
}

// createWindow creates the overlay window with override-redirect to bypass WM control.
func (o *Overlay) createWindow() error {
	wid, err := xproto.NewWindowId(o.conn)
	if err != nil {
		return fmt.Errorf("failed to create window ID: %w", err)
	}
	o.windowID = wid

	// Create a full-screen window to ensure we can draw anywhere
	screenWidth := o.screen.WidthInPixels
	screenHeight := o.screen.HeightInPixels

	// Window attributes:
	// - OverrideRedirect: bypass window manager (no decorations, absolute positioning)
	// - BackPixel: background color (will be shaped away)
	// - EventMask: we need exposure events for redrawing
	mask := uint32(xproto.CwBackPixel | xproto.CwOverrideRedirect | xproto.CwEventMask)
	values := []uint32{
		0x000000, // BackPixel: black (will be transparent via shape)
		1,        // OverrideRedirect: true
		xproto.EventMaskExposure | xproto.EventMaskStructureNotify,
	}

	err = xproto.CreateWindowChecked(
		o.conn,
		o.screen.RootDepth,
		o.windowID,
		o.screen.Root,
		0, 0,
		screenWidth,
		screenHeight,
		0,
		xproto.WindowClassInputOutput,
		o.screen.RootVisual,
		mask,
		values,
	).Check()

	if err != nil {
		return fmt.Errorf("failed to create window: %w", err)
	}

	return nil
}

// createGraphicsContext creates graphics contexts for drawing.
func (o *Overlay) createGraphicsContext() error {
	gcid, err := xproto.NewGcontextId(o.conn)
	if err != nil {
		return fmt.Errorf("failed to create GC ID: %w", err)
	}
	o.gcID = gcid

	color := o.config.GetColorUint32()
	mask := uint32(xproto.GcForeground)
	values := []uint32{color}

	if o.config.Crosshair.InvertedColor {
		mask = uint32(xproto.GcFunction | xproto.GcForeground)
		values = []uint32{xproto.GxXor, color}
	}

	if err := xproto.CreateGCChecked(o.conn, o.gcID, xproto.Drawable(o.windowID), mask, values).Check(); err != nil {
		return fmt.Errorf("failed to create GC: %w", err)
	}

	if o.config.Crosshair.OutlineThickness > 0 {
		outlineGC, err := xproto.NewGcontextId(o.conn)
		if err != nil {
			return fmt.Errorf("failed to create outline GC ID: %w", err)
		}
		o.outlineGC = outlineGC

		outlineColor := o.config.GetOutlineColorUint32()
		// FIX: Outline should always use normal foreground drawing,
		// never XOR mode, regardless of the inverted_color setting.
		outlineMask := uint32(xproto.GcForeground)
		outlineValues := []uint32{outlineColor}
		if err := xproto.CreateGCChecked(o.conn, o.outlineGC, xproto.Drawable(o.windowID), outlineMask, outlineValues).Check(); err != nil {
			return fmt.Errorf("failed to create outline GC: %w", err)
		}
	}

	return nil
}

// setOpacity sets the window opacity using the _NET_WM_WINDOW_OPACITY property.
// Requires a compositor (e.g., picom, compton, xcompmgr) to have visible effect.
func (o *Overlay) setOpacity() error {
	opacity := o.config.Crosshair.Opacity
	if opacity <= 0 || opacity >= 1.0 {
		// No need to set property; 1.0 is the default and 0.0 would be invisible
		return nil
	}

	// The _NET_WM_WINDOW_OPACITY property uses a uint32 where
	// 0xFFFFFFFF = fully opaque and 0x00000000 = fully transparent
	opacityValue := uint32(math.Round(opacity * float64(0xFFFFFFFF)))

	atomName := "_NET_WM_WINDOW_OPACITY"
	atomReply, err := xproto.InternAtom(o.conn, false, uint16(len(atomName)), atomName).Reply()
	if err != nil {
		return fmt.Errorf("failed to intern _NET_WM_WINDOW_OPACITY atom: %w", err)
	}

	cardinalReply, err := xproto.InternAtom(o.conn, false, 8, "CARDINAL").Reply()
	if err != nil {
		return fmt.Errorf("failed to intern CARDINAL atom: %w", err)
	}

	// Pack the uint32 as little-endian bytes
	data := []byte{
		byte(opacityValue),
		byte(opacityValue >> 8),
		byte(opacityValue >> 16),
		byte(opacityValue >> 24),
	}

	err = xproto.ChangePropertyChecked(
		o.conn,
		xproto.PropModeReplace,
		o.windowID,
		atomReply.Atom,
		cardinalReply.Atom,
		32, // format: 32-bit
		1,  // data length in units of format size
		data,
	).Check()
	if err != nil {
		return fmt.Errorf("failed to set opacity: %w", err)
	}

	return nil
}

// applyShapeWithSize configures the window shape for the given size.
func (o *Overlay) applyShapeWithSize(size int) error {
	cfg := o.config.Crosshair

	shapeRects := GenerateShape(
		cfg.Shape,
		o.centerX,
		o.centerY,
		int16(size),
		int16(cfg.Thickness),
		int16(cfg.Gap),
	)

	var boundingRects []xproto.Rectangle
	if cfg.OutlineThickness > 0 {
		outlineRects := GenerateOutline(shapeRects, int16(cfg.OutlineThickness))
		boundingRects = append(boundingRects, outlineRects...)
	}
	boundingRects = append(boundingRects, shapeRects...)

	// Set the BOUNDING shape: defines the visible area of the window
	err := shape.RectanglesChecked(
		o.conn,
		shape.SoSet,
		shape.SkBounding,
		xproto.ClipOrderingUnsorted,
		o.windowID,
		0, 0,
		boundingRects,
	).Check()
	if err != nil {
		return fmt.Errorf("failed to set bounding shape: %w", err)
	}

	// Set the INPUT shape: empty = entire window is click-through
	err = shape.RectanglesChecked(
		o.conn,
		shape.SoSet,
		shape.SkInput,
		xproto.ClipOrderingUnsorted,
		o.windowID,
		0, 0,
		[]xproto.Rectangle{},
	).Check()
	if err != nil {
		return fmt.Errorf("failed to set input shape: %w", err)
	}

	return nil
}

// applyShape configures the window shape using the base config size.
func (o *Overlay) applyShape() error {
	return o.applyShapeWithSize(o.currentSize)
}

// drawCrosshairWithSize renders the crosshair onto the window at the given size.
func (o *Overlay) drawCrosshairWithSize(size int) error {
	cfg := o.config.Crosshair

	shapeRects := GenerateShape(
		cfg.Shape,
		o.centerX,
		o.centerY,
		int16(size),
		int16(cfg.Thickness),
		int16(cfg.Gap),
	)

	if cfg.OutlineThickness > 0 && o.outlineGC != 0 {
		outlineRects := GenerateOutline(shapeRects, int16(cfg.OutlineThickness))
		if len(outlineRects) > 0 {
			if err := xproto.PolyFillRectangleChecked(o.conn, xproto.Drawable(o.windowID), o.outlineGC, outlineRects).Check(); err != nil {
				return fmt.Errorf("failed to draw outline: %w", err)
			}
		}
	}

	if len(shapeRects) > 0 {
		if err := xproto.PolyFillRectangleChecked(o.conn, xproto.Drawable(o.windowID), o.gcID, shapeRects).Check(); err != nil {
			return fmt.Errorf("failed to draw crosshair: %w", err)
		}
	}

	return nil
}

// drawCrosshair renders the crosshair onto the window.
func (o *Overlay) drawCrosshair() error {
	return o.drawCrosshairWithSize(o.currentSize)
}

// redrawWithSize clears the window and redraws the crosshair at a new size.
func (o *Overlay) redrawWithSize(size int) {
	o.currentSize = size

	// Clear the window
	xproto.ClearArea(o.conn, true, o.windowID, 0, 0, 0, 0)

	// Reapply shape mask for new size
	if err := o.applyShapeWithSize(size); err != nil {
		log.Printf("Warning: failed to reapply shape: %v", err)
	}

	// Redraw crosshair
	if err := o.drawCrosshairWithSize(size); err != nil {
		log.Printf("Warning: failed to redraw crosshair: %v", err)
	}
}

// getKeysymKeycode queries the X server for the keycode of a given keysym.
func getKeysymKeycode(conn *xgb.Conn, screen *xproto.ScreenInfo, targetSym xproto.Keysym) xproto.Keycode {
	setup := xproto.Setup(conn)
	count := byte(int(setup.MaxKeycode) - int(setup.MinKeycode) + 1)
	mapping, err := xproto.GetKeyboardMapping(conn, setup.MinKeycode, count).Reply()
	if err != nil {
		return 0
	}

	for i := 0; i < len(mapping.Keysyms); i++ {
		if mapping.Keysyms[i] == targetSym {
			return xproto.Keycode(int(setup.MinKeycode) + (i / int(mapping.KeysymsPerKeycode)))
		}
	}
	return 0
}

// toggleVisibility toggles the visibility of the crosshair window.
func (o *Overlay) toggleVisibility() {
	o.visible = !o.visible
	if o.visible {
		xproto.MapWindowChecked(o.conn, o.windowID).Check()
	} else {
		xproto.UnmapWindowChecked(o.conn, o.windowID).Check()
	}
}

// Run initializes and runs the overlay event loop.
func (o *Overlay) Run() error {
	if err := o.createWindow(); err != nil {
		return err
	}

	if err := o.createGraphicsContext(); err != nil {
		return err
	}

	if err := o.applyShape(); err != nil {
		return err
	}

	// Set opacity before mapping the window
	if err := o.setOpacity(); err != nil {
		log.Printf("Warning: failed to set opacity: %v", err)
	}

	if err := xproto.MapWindowChecked(o.conn, o.windowID).Check(); err != nil {
		return fmt.Errorf("failed to map window: %w", err)
	}

	if err := o.drawCrosshair(); err != nil {
		return err
	}

	// Grab Ctrl+Shift+H hotkey
	const keysymH = 0x0068 // lowercase h
	keycode := getKeysymKeycode(o.conn, o.screen, keysymH)
	if keycode != 0 {
		modifiers := []uint16{
			xproto.ModMaskControl | xproto.ModMaskShift,
			xproto.ModMaskControl | xproto.ModMaskShift | xproto.ModMaskLock,
			xproto.ModMaskControl | xproto.ModMaskShift | xproto.ModMask2,
			xproto.ModMaskControl | xproto.ModMaskShift | xproto.ModMaskLock | xproto.ModMask2,
		}
		for _, mod := range modifiers {
			xproto.GrabKeyChecked(o.conn, false, o.screen.Root, mod, keycode, xproto.GrabModeAsync, xproto.GrabModeAsync).Check()
		}
	} else {
		log.Println("Warning: Could not find keycode for hotkey (H)")
	}

	log.Printf("Crosshair overlay running on monitor %q at (%d, %d). Press Ctrl+C to exit.",
		o.monitor.Name, o.centerX, o.centerY)

	// Start dynamic resizing goroutine if enabled
	if o.dynamic != nil {
		o.dynamic.Start()
		go o.dynamicResizeLoop()
	}

	for {
		ev, err := o.conn.WaitForEvent()
		if err != nil {
			return fmt.Errorf("X11 connection error: %w", err)
		}

		if ev == nil {
			return nil
		}

		switch ev := ev.(type) {
		case xproto.ExposeEvent:
			if o.visible {
				if err := o.drawCrosshair(); err != nil {
					log.Printf("Warning: failed to redraw crosshair: %v", err)
				}
			}
		case xproto.KeyPressEvent:
			if ev.Detail == keycode {
				o.toggleVisibility()
			}
		}
	}
}

// dynamicResizeLoop runs at ~60fps and updates crosshair size based on input state.
func (o *Overlay) dynamicResizeLoop() {
	ticker := time.NewTicker(16 * time.Millisecond) // ~60fps
	defer ticker.Stop()

	for range ticker.C {
		if o.dynamic == nil {
			return
		}

		newSize := o.dynamic.CurrentSize()
		if newSize != o.currentSize && o.visible {
			o.redrawWithSize(newSize)
		}
	}
}

// ListMonitors connects to X server and prints available monitors.
func ListMonitors() error {
	conn, err := xgb.NewConn()
	if err != nil {
		return fmt.Errorf("failed to connect to X server: %w", err)
	}
	defer conn.Close()

	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)

	monitors, err := GetMonitors(conn, screen)
	if err != nil {
		return fmt.Errorf("failed to get monitors: %w", err)
	}

	PrintMonitors(monitors)
	return nil
}
