//go:build darwin || linux

package main

import (
	"os"
	"syscall"
)

func holdSession(path string) (*os.File, func(), error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, nil, e
	}
	return f, func() { _ = f.Close() }, nil
}
func lockSession(path string) (func(), error) {
	_, release, err := holdSession(path)
	return release, err
}
