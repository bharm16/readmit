# Native Mac screenshot catalog

Run from the repository root:

```sh
python3 tools/screenshot_catalog.py
```

The script builds a dedicated Wails application and captures its actual native
WKWebView. It writes a new `~/Desktop/Readmit Screenshots/YYYY-MM-DD_HH-MM-SS/`
directory containing PNGs, `index.html`, `manifest.json`, the frontend receipt
`capture.json`, and `build.log`. Open `index.html` to browse/filter the gallery.
Use `--output /absolute/path/to/new-directory` to choose another location. An
existing destination is refused. Earlier runs are never overwritten.

Requirements: macOS, Xcode command-line tools, the repository's Go and Node
versions, and an active graphical login session. Dependencies come from the
existing frontend lockfile. No browser automation package is needed. The capture
window is 1100 by 760 CSS points in the current system theme, matching the app's
normal initial appearance. PNGs use the native display scale (for example,
2200 by 1520 pixels on a Retina display). Window chrome is excluded. The snapshot
API captures only the capture application's own WebKit view; it does not record
other windows or require desktop Screen Recording permission.

## What is captured

- The production App's nine destinations, its top-level and nested ARIA tabs,
  case workflows, and selected menus/dialogs.
- Individual examples for all 146 current React function components (88 exported
  and 58 private), including
  panels, controls, and shared layout components. A TypeScript source inventory
  is regenerated on each run; a new component without an example or
  captured instance makes the run incomplete.
- Explicit navigation for panels that use ordinary buttons rather than tabs.
- Representative populated/form-ready, empty, error, busy, disabled and open
  states where the example's component contract supports them. This is a visual
  catalog, not every permutation of input data or every possible workflow.
- Additional viewport captures as a page or modal scrolls. Component crops are
  taken from their actual visible DOM bounds. A clipped crop is labelled
  `partial` in the manifest and can be read beside the page's scroll captures.

Private rendering helpers have explicit result fixtures where navigation alone
does not reveal them. The capture build exposes them through a virtual module;
production source exports stay unchanged. All inventoried components require
an actual native crop before the run can pass. Native file
pickers and OS permission prompts are not React components and are outside this
catalog; fixture data supplies their application-side outcomes.

## Isolation and completion

`desktop/captureapp` binds only snapshot/report methods. It does not bind the
real application or hub facades, read personal settings, or open real projects.
The separate `catalog.html` entry mounts the production React components over
`testkit`'s typed synthetic fixtures. The normal frontend entry and shipped
application never import the catalog. Preview buttons cannot execute the
backend because no real backend is present. These screenshots establish native
rendering of fixture states, not engine, database, licensing or network behavior.

Only the catalog Vite build adds stable source-qualified React display names.
The collector uses React's inspection links to find visible component bounds.
If a future React version changes those links, missing component captures fail
coverage instead of silently claiming success.

Exit status zero means all required component captures and planned scenarios
succeeded. Missing fixtures, rendering errors, native snapshot failures, missing
PNGs or uncaptured components produce a nonzero exit and retained
output. Read the gallery's failure list and `build.log`; incomplete output is
useful for diagnosis but is not full coverage. A native window closed early is
also a failure.

The script checks TypeScript before building. To validate changes to this tool:

```sh
python3 -m unittest discover -s tools -p 'test_screenshot_catalog.py' -v
cd desktop/frontend && npm run build
```

Then run the full native catalog and inspect representative page, dialog and
component PNGs. The native run itself is the rendering acceptance check.
