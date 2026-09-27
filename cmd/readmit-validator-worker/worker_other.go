//go:build !linux

package main

// run refuses unqualified execution outside the fixed Linux worker image.
func run() int { return 126 }
