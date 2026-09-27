// Command stage seals an administrator's built validator metadata as a
// readmit-fhir-validator-capability/v1 directory. It is the explicit local
// staging step: it reads only that folder, runs no program and uses no network.
package main

import (
	"context"
	"encoding/json/v2"
	"flag"
	"fmt"
	"os"

	"github.com/bharm16/readmit/internal/fhirvalidator"
)

func main() {
	metadata := flag.String("metadata", "", "the build's metadata folder")
	output := flag.String("output", "", "the new capability directory")
	flag.Parse()
	if *metadata == "" || *output == "" || flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	capability, err := fhirvalidator.StageBuild(context.Background(), *metadata, *output)
	if err != nil {
		fmt.Fprintln(os.Stderr, "stage:", err)
		os.Exit(1)
	}
	raw, _ := json.Marshal(map[string]string{"capability": capability.Identity(), "directory": *output})
	fmt.Println(string(raw))
}
