// readmit-validator-worker is an optional staged container entrypoint, not an
// alternate user command or a dependency of ordinary readmit execution.
package main

import "os"

func main() { os.Exit(run()) }
