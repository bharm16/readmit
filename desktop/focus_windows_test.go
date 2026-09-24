//go:build windows

package main

import (
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

const otherMessage = 0x0400 + 378 // WM_USER + 378, a message nothing else sends

type windowClass struct {
	size, style                        uint32
	procedure                          uintptr
	classExtra, windowExtra            int32
	instance, icon, cursor, background uintptr
	menuName, className                *uint16
	smallIcon                          uintptr
}

// A window of the class Wails registers stands in for Wails' own. The guard
// hands Wails the focus of the enabled window and every other message, and
// withholds only the focus the window receives while a dialog has disabled it.
func TestTheFocusOfADisabledWindowIsWithheldFromWails(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var (
		getModuleHandle = syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW")
		registerClass   = user32.NewProc("RegisterClassExW")
		unregisterClass = user32.NewProc("UnregisterClassW")
		createWindow    = user32.NewProc("CreateWindowExW")
		destroyWindow   = user32.NewProc("DestroyWindow")
		enableWindow    = user32.NewProc("EnableWindow")
		sendMessage     = user32.NewProc("SendMessageW")
	)

	if guardWebviewFocus() {
		t.Fatal("the guard reported itself in place with no window of Wails' class in this process")
	}

	// The stand-in for Wails' window procedure records the focus it is
	// handed, which Wails hands on to WebView2.
	var handedFocus, handedOther int
	standIn := func(window uintptr, message uint32, wparam, lparam uintptr) uintptr {
		switch message {
		case wmSetFocus:
			handedFocus++
			return 0
		case otherMessage:
			handedOther++
			return 0
		}
		result, _, _ := defWindowProc.Call(window, uintptr(message), wparam, lparam)
		return result
	}
	instance, _, _ := getModuleHandle.Call(0)
	class, _ := syscall.UTF16PtrFromString("wailsWindow")
	registration := windowClass{procedure: syscall.NewCallback(standIn), instance: instance, className: class}
	registration.size = uint32(unsafe.Sizeof(registration))
	if atom, _, err := registerClass.Call(uintptr(unsafe.Pointer(&registration))); atom == 0 {
		t.Fatalf("registering the window class: %v", err)
	}
	defer unregisterClass.Call(uintptr(unsafe.Pointer(class)), instance)
	window, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 100, 100, 0, 0, instance, 0)
	if window == 0 {
		t.Fatalf("creating the window: %v", err)
	}
	defer destroyWindow.Call(window)
	if !guardWebviewFocus() {
		t.Fatal("the guard was not placed in front of the window's procedure")
	}

	send := func(message uintptr) { sendMessage.Call(window, message, 0, 0) }
	send(wmSetFocus)
	if handedFocus != 1 {
		t.Fatalf("an enabled window's focus reached Wails %d times, want 1", handedFocus)
	}
	enableWindow.Call(window, 0)
	send(wmSetFocus)
	send(otherMessage)
	if handedFocus != 1 {
		t.Errorf("a disabled window's focus reached Wails")
	}
	if handedOther != 1 {
		t.Errorf("another message to a disabled window reached Wails %d times, want 1", handedOther)
	}
	enableWindow.Call(window, 1)
	send(wmSetFocus)
	if handedFocus != 2 {
		t.Errorf("the focus of the window enabled again reached Wails %d times in all, want 2", handedFocus)
	}
}
