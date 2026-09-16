package overlay

import (
	"log"
	"os"
	"sync"
	"time"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/xproto"
)

// SessionOverlay manages an always-on-top, non-intrusive floating banner
// displayed on Linux X11 desktops when active GUI control is occurring.
type SessionOverlay struct {
	mu          sync.Mutex
	conn        *xgb.Conn
	win         xproto.Window
	gc          xproto.Gcontext
	font        xproto.Font
	screen      *xproto.ScreenInfo
	mapped      bool
	timer       *time.Timer
	hideDelay   time.Duration
	stopChan    chan struct{}
	initialized bool
}

// NewSessionOverlay initializes the overlay on the current DISPLAY.
// If X11 is not available, it safely returns a disabled overlay.
func NewSessionOverlay() *SessionOverlay {
	display := os.Getenv("DISPLAY")
	if display == "" {
		display = ":0"
	}

	so := &SessionOverlay{
		hideDelay: 15 * time.Second,
		stopChan:  make(chan struct{}),
	}

	conn, err := xgb.NewConnDisplay(display)
	if err != nil {
		log.Printf("[Overlay] Could not connect to X11 display %s: %v. Visual banner will be disabled.", display, err)
		return so
	}

	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)
	if screen == nil {
		log.Println("[Overlay] No default screen found on X11 connection.")
		conn.Close()
		return so
	}

	win, err := xproto.NewWindowId(conn)
	if err != nil {
		log.Printf("[Overlay] Failed to generate window ID: %v", err)
		conn.Close()
		return so
	}

	// Dimensions and coordinates (top-right corner)
	const width uint16 = 270
	const height uint16 = 44
	x := int16(screen.WidthInPixels - uint16(width) - 24)
	if x < 10 {
		x = 10
	}
	y := int16(20)

	// Dark slate background: 0x1E1E2E, border: 0xEF4444 (Red-orange active alert)
	var bgPixel uint32 = 0x181825
	var borderPixel uint32 = 0xEF4444

	// Attributes:
	// - OverrideRedirect: 1 (Bypasses Window Manager window decorations and stealing focus)
	// - EventMask: Exposure so we know when to repaint
	mask := uint32(xproto.CwBackPixel | xproto.CwBorderPixel | xproto.CwOverrideRedirect | xproto.CwEventMask)
	values := []uint32{
		bgPixel,
		borderPixel,
		1, // Override redirect = true (Always on top OSD banner)
		uint32(xproto.EventMaskExposure),
	}

	err = xproto.CreateWindowChecked(
		conn,
		screen.RootDepth,
		win,
		screen.Root,
		x, y,
		width, height,
		2, // Border width = 2px
		xproto.WindowClassInputOutput,
		screen.RootVisual,
		mask,
		values,
	).Check()

	if err != nil {
		log.Printf("[Overlay] Failed to create X11 window: %v", err)
		conn.Close()
		return so
	}

	// Open fixed standard font
	font, err := xproto.NewFontId(conn)
	fontLoaded := false
	if err == nil {
		fontErr := xproto.OpenFontChecked(conn, font, uint16(len("fixed")), "fixed").Check()
		if fontErr == nil {
			fontLoaded = true
		}
	}

	// Create Graphics Context
	gc, err := xproto.NewGcontextId(conn)
	if err == nil {
		gcMask := uint32(xproto.GcForeground | xproto.GcBackground)
		gcValues := []uint32{0xFFFFFF, bgPixel}
		if fontLoaded {
			gcMask |= uint32(xproto.GcFont)
			gcValues = append(gcValues, uint32(font))
		}
		_ = xproto.CreateGCChecked(conn, gc, xproto.Drawable(win), gcMask, gcValues).Check()
	}

	so.conn = conn
	so.win = win
	so.gc = gc
	so.font = font
	so.screen = screen
	so.initialized = true

	// Event loop to handle Exposure repaints
	go so.eventLoop()

	return so
}

// NotifyActivity indicates a GUI control action has taken place.
// It maps the window if hidden and resets the 15-second inactivity hide timer.
func (so *SessionOverlay) NotifyActivity() {
	if so == nil || !so.initialized {
		return
	}

	so.mu.Lock()
	defer so.mu.Unlock()

	if !so.mapped {
		_ = xproto.MapWindowChecked(so.conn, so.win).Check()
		so.mapped = true
		so.render()
	}

	if so.timer != nil {
		so.timer.Stop()
	}

	so.timer = time.AfterFunc(so.hideDelay, func() {
		so.mu.Lock()
		defer so.mu.Unlock()
		if so.mapped {
			_ = xproto.UnmapWindowChecked(so.conn, so.win).Check()
			so.mapped = false
		}
	})
}

// render draws the text and status badge inside the banner
func (so *SessionOverlay) render() {
	if !so.initialized || !so.mapped {
		return
	}

	// 1. Clear background
	xproto.PolyFillRectangle(
		so.conn,
		xproto.Drawable(so.win),
		so.gc,
		[]xproto.Rectangle{{X: 0, Y: 0, Width: 270, Height: 44}},
	)

	// 2. Draw text line 1: Warning header
	title := "[!] CONTROL REMOTO GUI ACTIVO"
	xproto.ImageText8(
		so.conn,
		byte(len(title)),
		xproto.Drawable(so.win),
		so.gc,
		14, 18,
		title,
	)

	// 3. Draw text line 2: Subtitle
	subtitle := "Sesion activa - Omni Remote"
	xproto.ImageText8(
		so.conn,
		byte(len(subtitle)),
		xproto.Drawable(so.win),
		so.gc,
		14, 34,
		subtitle,
	)
}

func (so *SessionOverlay) eventLoop() {
	for {
		select {
		case <-so.stopChan:
			return
		default:
			ev, err := so.conn.WaitForEvent()
			if err != nil {
				return
			}
			if ev == nil {
				continue
			}

			switch ev.(type) {
			case xproto.ExposeEvent:
				so.mu.Lock()
				so.render()
				so.mu.Unlock()
			}
		}
	}
}

// Close destroys the overlay and releases X11 resources.
func (so *SessionOverlay) Close() {
	if so == nil || !so.initialized {
		return
	}

	so.mu.Lock()
	defer so.mu.Unlock()

	close(so.stopChan)
	if so.timer != nil {
		so.timer.Stop()
	}

	if so.mapped {
		_ = xproto.UnmapWindowChecked(so.conn, so.win).Check()
		so.mapped = false
	}

	_ = xproto.DestroyWindowChecked(so.conn, so.win).Check()
	so.conn.Close()
	so.initialized = false
}
