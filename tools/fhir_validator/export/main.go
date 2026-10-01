// Command export writes a readmit-fhir-validator-package/v1 directory on the
// build machine: the staged capability, the worker image as this machine's
// container engine saves it, and the manifest binding them. It prints the
// package identity the administrator publishes for `readmit validator
// install`. It is the explicit administrator step; nothing is fetched.
//
//	export --capability STAGED_CAPABILITY --output NEW_PACKAGE [--socket PATH]
package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/bharm16/readmit/internal/fhirvalidator"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run exports one package and answers its exit status: 0 exported, 1 refused
// with an actionable state on stderr, 2 a usage error.
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("export", flag.ContinueOnError)
	flags.SetOutput(stderr)
	capability := flags.String("capability", "", "the staged capability folder")
	output := flags.String("output", "", "a new package folder")
	socket := flags.String("socket", "", "the local container engine's Unix socket")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *capability == "" || *output == "" {
		fmt.Fprintln(stderr, "usage: export --capability DIR --output NEW_PACKAGE [--socket PATH]; see docs/fhir-validation.md")
		return 2
	}
	fail := func(err error) int {
		var status fhirvalidator.Status
		if errors.As(err, &status) {
			raw, _ := json.Marshal(status)
			fmt.Fprintln(stderr, string(raw))
		} else {
			fmt.Fprintln(stderr, "export:", err)
		}
		return 1
	}
	engine, err := fhirvalidator.LocalEngine(*socket)
	if err != nil {
		return fail(err)
	}
	defer engine.Close()
	p, err := engine.ExportPackage(context.Background(), *capability, *output)
	if err != nil {
		return fail(err)
	}
	raw, _ := json.Marshal(map[string]string{"package": p.Identity, "capability": p.Capability.Identity(), "image": p.Manifest.Image}, json.Deterministic(true))
	fmt.Fprintln(stdout, string(raw))
	return 0
}
