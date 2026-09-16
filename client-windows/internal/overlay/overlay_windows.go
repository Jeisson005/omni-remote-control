//go:build windows

package overlay

import (
	"log"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/Jeisson005/omni-remote-control/client-windows/internal/win32"
)

type SessionOverlay struct {
	mu        sync.Mutex
	hwnd      uintptr
	visible   bool
	timer     *time.Timer
	hideDelay time.Duration
	stopChan  chan struct{}
	readyChan chan struct{}
}

func NewSessionOverlay() *SessionOverlay {
	so := &SessionOverlay{
		hideDelay: 15 * time.Second,
		stopChan:  make(chan struct{}),
		readyChan: make(chan struct{}),
	}

	go so.runWindowLoop()
	<-so.readyChan

	return so
}

func (so *SessionOverlay) runWindowLoop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	className := "OmniRemoteOverlayWindow"
	clsPtr, _ := syscall.UTF16PtrFromString(className)

	wndProc := syscall.NewCallback(func(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
		switch msg {
		case win32.WM_PAINT:
			var ps win32.PAINTSTRUCT
			hdc := win32.BeginPaint(hwnd, &ps)
			if hdc != 0 {
				rect := ps.RcPaint

				// Dark slate background (RGB: 24, 24, 37 -> BGR 0x00251818)
				bgBrush := win32.CreateSolidBrush(0x00251818)
				win32.FillRect(hdc, &rect, bgBrush)
				win32.DeleteObject(bgBrush)

				// Transparent background for text
				win32.SetBkMode(hdc, 1)

				// Title text: Red/coral alert
				win32.SetTextColor(hdc, 0x004444EF) // BGR for #EF4444
				rTitle := win32.RECT{Left: 12, Top: 8, Right: rect.Right, Bottom: 24}
				win32.DrawText(hdc, "● CONTROL REMOTO GUI ACTIVO", &rTitle, win32.DT_LEFT|win32.DT_SINGLELINE|win32.DT_NOCLIP)

				// Subtitle text: Muted white/slate
				win32.SetTextColor(hdc, 0x00D0D0D0)
				rSub := win32.RECT{Left: 12, Top: 26, Right: rect.Right, Bottom: 42}
				win32.DrawText(hdc, "Sesion activa - Omni Remote", &rSub, win32.DT_LEFT|win32.DT_SINGLELINE|win32.DT_NOCLIP)

				win32.EndPaint(hwnd, &ps)
			}
			return 0

		case win32.WM_ERASEBKGND:
			return 1

		case win32.WM_DESTROY:
			win32.PostQuitMessage(0)
			return 0

		default:
			return win32.DefWindowProc(hwnd, msg, wparam, lparam)
		}
	})

	var wc win32.WNDCLASSEXW
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.LpfnWndProc = wndProc
	wc.LpszClassName = clsPtr

	_, _ = win32.RegisterClassEx(&wc)

	screenWidth := win32.GetSystemMetrics(win32.SM_CXSCREEN)
	const winWidth int32 = 270
	const winHeight int32 = 46
	x := screenWidth - winWidth - 25
	if x < 10 {
		x = 10
	}
	y := int32(20)

	hwnd, err := win32.CreateWindowEx(
		win32.WS_EX_TOPMOST|win32.WS_EX_TOOLWINDOW|win32.WS_EX_NOACTIVATE,
		className,
		"Omni Remote Active Session",
		win32.WS_POPUP,
		x, y,
		winWidth, winHeight,
		0, 0, 0, nil,
	)

	if err != nil || hwnd == 0 {
		log.Printf("[Overlay-Win32] Failed to create overlay window: %v", err)
		close(so.readyChan)
		return
	}

	so.hwnd = hwnd
	close(so.readyChan)

	var msg win32.MSG
	for {
		res := win32.GetMessage(&msg, 0, 0, 0)
		if res <= 0 {
			break
		}
		win32.TranslateMessage(&msg)
		win32.DispatchMessage(&msg)
	}
}

func (so *SessionOverlay) NotifyActivity() {
	if so == nil || so.hwnd == 0 {
		return
	}

	so.mu.Lock()
	defer so.mu.Unlock()

	if !so.visible {
		win32.ShowWindow(so.hwnd, win32.SW_SHOWNOACTIVATE)
		win32.InvalidateRect(so.hwnd, nil, true)
		so.visible = true
	}

	if so.timer != nil {
		so.timer.Stop()
	}

	so.timer = time.AfterFunc(so.hideDelay, func() {
		so.mu.Lock()
		defer so.mu.Unlock()
		if so.visible {
			win32.ShowWindow(so.hwnd, win32.SW_HIDE)
			so.visible = false
		}
	})
}

func (so *SessionOverlay) Close() {
	if so == nil || so.hwnd == 0 {
		return
	}

	so.mu.Lock()
	defer so.mu.Unlock()

	if so.timer != nil {
		so.timer.Stop()
	}

	if so.visible {
		win32.ShowWindow(so.hwnd, win32.SW_HIDE)
		so.visible = false
	}

	win32.DestroyWindow(so.hwnd)
	so.hwnd = 0
}
