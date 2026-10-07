//go:build !darwin && !linux

package main

import (
	"errors"
	"os"
)

func holdSession(string) (*os.File, func(), error) {
	return nil, nil, errors.New("product lab driver requires Linux or macOS")
}
func lockSession(path string) (func(), error) {
	_, release, err := holdSession(path)
	return release, err
}
