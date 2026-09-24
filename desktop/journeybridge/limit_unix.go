//go:build !windows

package main

import "syscall"

// limitFileSize lowers this process's file size limit, so a write that would
// make one file larger than bytes is refused as a full disk refuses it. The Go
// runtime ignores the signal the kernel sends with that refusal, so the write
// returns an error rather than ending the application.
func limitFileSize(bytes int64) error {
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &limit); err != nil {
		return err
	}
	limit.Cur = uint64(bytes)
	return syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limit)
}
