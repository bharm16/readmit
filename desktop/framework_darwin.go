//go:build darwin && production

package main

// Wails' native file dialogs use UTType on the supported macOS floor. Its CLI
// supplies this linker flag; direct Go builds must supply the same framework.

// #cgo LDFLAGS: -framework UniformTypeIdentifiers
import "C"
