package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework WebKit -framework UniformTypeIdentifiers
#include <stdlib.h>
void readmitSnapshot(char *path, double x, double y, double width, double height);
*/
import "C"
import (
	"errors"
	"time"
	"unsafe"
)

var snapshotResult = make(chan string, 1)

//export readmitSnapshotFinished
func readmitSnapshotFinished(message *C.char) { snapshotResult <- C.GoString(message) }

func snapshot(path string, x, y, width, height float64) error {
	p := C.CString(path)
	C.readmitSnapshot(p, C.double(x), C.double(y), C.double(width), C.double(height))
	C.free(unsafe.Pointer(p))
	select {
	case message := <-snapshotResult:
		if message != "" {
			return errors.New(message)
		}
		return nil
	case <-time.After(20 * time.Second):
		return errors.New("native snapshot timed out")
	}
}
