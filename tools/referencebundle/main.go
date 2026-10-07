// Command referencebundle validates and packages the maintainer's complete HL7
// library for embedding in the desktop. It acquires no content or rights.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/bharm16/readmit/internal/hl7reference"
)

func main() {
	source := flag.String("source", "", "complete local HL7 library folder")
	output := flag.String("output", "", "new bundled archive file")
	flag.Parse()
	if *source == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "referencebundle requires -source and -output")
		os.Exit(2)
	}
	file, err := os.OpenFile(*output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err == nil {
		err = hl7reference.BuildBundle(*source, file)
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(*output)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
