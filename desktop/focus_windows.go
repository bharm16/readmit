//go:build windows

package main

import (
	"os"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// Wails' Windows window hands the keyboard focus to WebView2 each time the
// window receives it, and the go-webview2 release it pins ends the process
// when WebView2 refuses. WebView2 refuses while the window is disabled, which
// it is for as long as a host dialog it owns is open, yet the window can still
// receive the focus then. No Wails 2 or go-webview2 release handles the
// refusal, so the shell withholds that one handoff: while the window is
// disabled its focus is not passed on. The page loses nothing, since a
// disabled window takes no input; when the dialog closes the window is
// enabled and activated again, and that focus reaches the page as before.
// Every other message, and the focus of an enabled window, reaches Wails
// unchanged.

const (
	wmSetFocus  = 0x0007
	gwlpWndProc = ^uintptr(3) // GWLP_WNDPROC, which is -4
)

var (
	user32                   = syscall.NewLazyDLL("user32.dll")
	findWindowEx             = user32.NewProc("FindWindowExW")
	getWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	getWindowLongPtr         = user32.NewProc("GetWindowLongPtrW")
	setWindowLongPtr         = user32.NewProc("SetWindowLongPtrW")
	callWindowProc           = user32.NewProc("CallWindowProcW")
	defWindowProc            = user32.NewProc("DefWindowProcW")
	isWindowEnabled          = user32.NewProc("IsWindowEnabled")

	// wailsProcedure is the window procedure Wails gave its window, which
	// every message but a withheld focus is handed to.
	wailsProcedure atomic.Uintptr
)

// guardWebviewFocus places focusGuardProcedure in front of Wails' window
// procedure and reports whether it is in place. It runs once, after Wails has
// created its window.
func guardWebviewFocus() bool {
	window := applicationWindow()
	if window == 0 {
		return false
	}
	previous, _, _ := getWindowLongPtr.Call(window, gwlpWndProc)
	if previous == 0 {
		return false
	}
	wailsProcedure.Store(previous)
	replaced, _, _ := setWindowLongPtr.Call(window, gwlpWndProc, syscall.NewCallback(focusGuardProcedure))
	return replaced == previous
}

func focusGuardProcedure(window uintptr, message uint32, wparam, lparam uintptr) uintptr {
	if message == wmSetFocus {
		if enabled, _, _ := isWindowEnabled.Call(window); uint32(enabled) == 0 {
			result, _, _ := defWindowProc.Call(window, uintptr(message), wparam, lparam)
			return result
		}
	}
	result, _, _ := callWindowProc.Call(wailsProcedure.Load(), window, uintptr(message), wparam, lparam)
	return result
}

// applicationWindow is this process's window of the class Wails registers its
// window under when no other class is configured.
func applicationWindow() uintptr {
	class, err := syscall.UTF16PtrFromString("wailsWindow")
	if err != nil {
		return 0
	}
	process := uint32(os.Getpid())
	for window := uintptr(0); ; {
		window, _, _ = findWindowEx.Call(0, window, uintptr(unsafe.Pointer(class)), 0)
		if window == 0 {
			return 0
		}
		var owner uint32
		getWindowThreadProcessID.Call(window, uintptr(unsafe.Pointer(&owner)))
		if owner == process {
			return window
		}
	}
}
