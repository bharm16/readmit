# Third-party notices

The executable contains Go's standard library (BSD-3-Clause), Cobra v1.10.2
(Apache-2.0), pflag (BSD-3-Clause), and, on Windows, mousetrap (Apache-2.0).
Exact application dependency versions are recorded in `go.mod` and `go.sum`.
Their license texts are included in `licenses/`.

The embedded v2.5.1 field labels are adapted from nHapi under MPL-2.0. Their
preferred source form is supplied as `dictionary/fields-v251.json` in release
archives, with attribution and the original source revision in
`docs/dictionary-provenance.md`. No nHapi runtime code is included.

GoReleaser and govulncheck are build/development tools, not application runtime
dependencies. GitHub Actions attests only executable build outputs, never
customer evidence.
