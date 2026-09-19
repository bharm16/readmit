# Third-party notices

The executable contains Go's standard library (BSD-3-Clause), Cobra v1.10.2
(Apache-2.0), pflag (BSD-3-Clause), and, on Windows, mousetrap (Apache-2.0).
Exact application dependency versions are recorded in `go.mod` and `go.sum`.
Their license texts are included in `licenses/`.

The embedded v2.5.1 field labels are adapted from nHapi under MPL-2.0. Their
preferred source form is supplied as `dictionary/fields-v251.json` in release
archives, with attribution and the original source revision in
`docs/dictionary-provenance.md`. No nHapi runtime code is included.

The desktop shell is a separate module and is not part of the release archives.
Its native packages carry this file beside the application.
Its executable contains Wails 2 (MIT) and its dependencies, recorded in
`desktop/go.mod` and `desktop/go.sum`, and the bundled interface contains React
and React DOM (MIT), recorded with their exact versions in
`desktop/frontend/package-lock.json`. Those two license texts are included in
`licenses/`. Vite, TypeScript and the Vite React plugin build the interface and
are not shipped inside it.

GoReleaser, WiX and govulncheck are build/development tools, not application
runtime dependencies. WiX writes the Windows installer database and
contributes no code to the application that database installs. GitHub Actions
attests only executable and package build outputs, never customer evidence.
