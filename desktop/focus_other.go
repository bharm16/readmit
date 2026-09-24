//go:build !windows

package main

// guardWebviewFocus has nothing to guard outside Windows: only Wails' Windows
// window hands its focus to a webview that can end the process by refusing it.
func guardWebviewFocus() bool { return true }
