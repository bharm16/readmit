package main

import "errors"

// limitFileSize has no Windows counterpart: the journeys run where the
// process file size limit exists.
func limitFileSize(int64) error {
	return errors.New("-file-size-limit needs a process file size limit, which Windows does not have")
}
