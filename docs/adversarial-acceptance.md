# Local adversarial acceptance

This is the finite local evidence for #111, not finished-product security or
accessibility acceptance. The launcher runs synthetic public-boundary tests;
passing them does not certify a customer deployment, native screen reader,
release package, de-identification, or independent security assessment.
Existing evidence contracts and bytes remain unchanged. No customer PHI,
credential, public endpoint or external target belongs in this procedure.

## Reproduce the local boundary matrix

Run as a non-root macOS/Linux account with the pinned Go compiler and an
**isolated, disposable PostgreSQL cluster**. The hub tests intentionally reset
`readmit_hub_test` tables; never point them at a retained database. Select an
unused port and an empty private local socket directory. For example, with
PostgreSQL tools on PATH:

```sh
lab=$(mktemp -d)
chmod 700 "$lab"
initdb -D "$lab/pg" -A trust -U acceptance
pg_ctl -D "$lab/pg" -l "$lab/postgres.log" -o "-k $lab -p 56111 -h ''" start
createdb -h "$lab" -p 56111 -U acceptance readmit_hub_test
READMIT_HUB_TEST_SOCKET="$lab" READMIT_HUB_TEST_PORT=56111 \
  READMIT_HUB_TEST_USER=acceptance \
  python3 tools/adversarial.py --output "$lab/evidence"
pg_ctl -D "$lab/pg" stop
```

Only the local Unix socket is enabled; PostgreSQL TCP listening is disabled.
Retain `evidence/summary.txt` and its numbered Go JSON event logs privately.
The summary records source revision, dirty state, tracked-diff and launcher
SHA-256 digests. Prefer a clean committed candidate; a dirty run also requires
retaining the exact changed and untracked source files. A missing named test,
skipped test/subtest, failed package, timeout or missing package completion
makes the launcher fail. A zero-test success is not acceptance. Output must be
a new directory and is owner-only. The standalone launcher is not a new CI
aggregate or a change to the [required checks](agents/testing.md).

| Surface | Exercised positive and adversarial boundaries |
| --- | --- |
| Paths and archives | Real CLI archive import accepts safe members, rejects traversal and reuse; sealed packet destinations are refused without changing evidence; linked workspace/backup entries are refused. |
| Renderer and PHI | Legitimate original message/name/note/spec/run/result/report markers are verified present, then absent from all approved support bundles and events; actual HTML report output escapes script text. The new CLI corpus preserves printable hostile text/non-UTF-8 bytes on roundtrip, hides them by default, escapes explicit display and refuses control-byte input without reflection. |
| Secrets and encryption | A planted test credential is found by the CLI scanner without printing its value; unresolved credentials refuse scanning. Wrong/rotated keys, altered ciphertext and retained destinations refuse decryption/publication. |
| Authorization and roles | Public role matrix, wrong tokens, immediate token removal, cross-project isolation, stale approval and role revocation run through the hub, including real disposable PostgreSQL persistence/restore. |
| Observations and TLS | Complete synthetic observation windows succeed; old capture watermarks, failed collection and cancellation cannot become absence or freshness. Real loopback TLS checks CA/hostname and mutual-client verification/TLS floor failures. |
| Permissions and recovery | Unprivileged filesystem denial is distinct from failure; saved filters refuse unreadable state; backup preserves exact identities, rejects interrupted/damaged/linked data and cancellation; hub restore retains access decisions. |

The exact test names live in `tools/adversarial.py`, so deleting or renaming one
requires an explicit matrix change. These are local integration tests built
from source, not an independent implementation or tests of signed packages.
Existing CI still owns full race, independent protocol, fuzz, vulnerability,
packaging, desktop and five native archive smoke checks.

## Browser exercise and retained local result

On September 19, 2026, the 32 named local tests above passed on macOS arm64
with Go 1.27.1 and an isolated PostgreSQL 14 cluster. PostgreSQL 14 is a local
hub test environment here, not proof of the advertised external database
connector matrix. The new hostile-input test first refused the embedded control
bytes as expected by the parser contract; the final test explicitly checks that
refusal separately from successful printable-text roundtrip.

A temporary Vite page imported the real `Palette`, `Separator`, and `Status`
components from `desktop/frontend/src/shell.tsx`, with production styles, and
was exercised in Chromium 153.0.8010.48. This component exercise is intentionally
limited: it does not exercise the complete application, Wails bridge, native
webview, native file dialog or a screen reader. No frontend test framework,
package dependency or CI test-runner decision is adopted; #183 remains open.

The temporary page used this synthetic fixture (create beside the frontend's
`index.html`, then remove it after the exercise):

```tsx
import { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { Palette, Separator, Status } from './src/shell';
import './src/styles.css';
const attack = '<img src="https://planted.invalid/leak" onerror="window.PLANTED=111"><script>window.PLANTED=111</script>';
function Probe() {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [split, setSplit] = useState(58);
  const [chosen, setChosen] = useState('none');
  return <main><h1>Synthetic boundary probe</h1>
    <button onClick={() => setOpen(true)}>Open palette</button>
    <Separator split={split} min={20} max={80} step={5}
      onSplit={setSplit} bounds={() => null}/>
    <Status state="failed" indicator={undefined} reason={attack}/>
    <output aria-label="Chosen command">{chosen}</output>
    <Palette open={open} commands={[{id:'go-to-evidence', title:attack, keys:'F6'}]}
      query={query} onQuery={setQuery} onClose={() => setOpen(false)} onRun={setChosen}/>
  </main>;
}
createRoot(document.getElementById('root')!).render(<Probe/>);
```

Load it from a temporary HTML page with `lang="en"`, a `root` div, and a module
script pointing to the fixture. Bind Vite to loopback only. Block and count all
non-loopback requests before navigation. Do not permit the planted URL to load.
Retain the browser version, accessibility-tree snapshot, screenshot and assertion
output with the candidate evidence. The observed outcomes were:

- Enter on the opener focuses the labelled command textbox. Repeated Tab cannot
  focus the background opener or separator while the modal is open. Chromium
  may move focus to browser chrome; on reentry it returns to the modal.
- Escape closes the modal and restores focus to the opener. Reopen, type in the
  textbox, and Enter selects `go-to-evidence` and closes the dialog.
- The separator exposes its name, orientation and current/min/max values.
  ArrowRight changes 58 to 63; Home sets 20 and ArrowLeft cannot pass 20;
  End sets 80 and ArrowRight cannot pass 80.
- Both hostile labels remain literal text: no `img` node, executed marker,
  attempted non-loopback request or page error. Local/session storage stay empty.
- The status remains a semantic live status with textual failure, and the
  accessibility tree retains the literal hostile text and labelled controls.

The review used the [Web Interface Guidelines](https://github.com/vercel-labs/web-interface-guidelines)
for semantic controls, names, focus and status feedback. Accessibility-tree
inspection is evidence about browser semantics, not evidence of spoken output.

### Replay the browser assertions

The exact assertion probe below used Playwright 1.64.0-alpha-1789764292000 with the local Chrome
channel (Chromium 153.0.8010.48). Use an isolated temporary harness installation
of that version; do not add it to the frontend package manifest. Save the fixture
above as `desktop/frontend/acceptance-111.tsx`, and save this page beside it as
`acceptance-111.html`:

```html
<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Synthetic acceptance</title></head><body><div id="root"></div><script type="module" src="/acceptance-111.tsx"></script></body></html>
```

Start `npm run dev -- --host 127.0.0.1 --port 51111 --strictPort` in
`desktop/frontend`. Save the following probe outside the source tree as
`browser.cjs`; run `node browser.cjs /absolute/path/to/playwright /private/evidence`
with an existing owner-only evidence directory. It imports Playwright from that
explicit path, uses a fresh browser profile, blocks all non-loopback requests,
and never opens a customer workspace. Stop Vite and remove both temporary
frontend files after the exercise. The fixture is a controlled component page,
not the packaged application. Its source and assertions below belong to the
candidate commit; retain their SHA-256 with the browser output when exporting
this procedure independently of the repository.

```js
const {chromium} = require(process.argv[2]);
const assert = require('node:assert/strict');
const fs = require('node:fs');
(async()=>{
 const browser=await chromium.launch({headless:true,channel:"chrome"});
 try {
 const page=await browser.newPage(); const external=[]; const errors=[];
 await page.route('**/*', route => { if(new URL(route.request().url()).hostname !== '127.0.0.1') {external.push(route.request().url()); return route.abort();} return route.continue(); });
 page.on('pageerror', e=>errors.push(e.message));
 await page.goto('http://127.0.0.1:51111/acceptance-111.html');
 const opener=page.getByRole('button',{name:'Open palette'});
 await opener.focus(); await page.keyboard.press('Enter');
 const dialog=page.getByRole('dialog',{name:'Command palette'}); await dialog.waitFor();
 assert.equal(await page.getByRole('textbox',{name:'Type a command'}).evaluate(e=>e===document.activeElement),true);
 for(let i=0;i<8;i++) {await page.keyboard.press('Tab'); assert.equal(await page.evaluate(()=>!document.hasFocus() || !!document.activeElement.closest('dialog')),true,'modal reached background');}
 await page.keyboard.press('Escape'); await dialog.waitFor({state:'hidden'});
 assert.equal(await opener.evaluate(e=>e===document.activeElement),true,'focus did not return');
 await page.keyboard.press('Enter'); await dialog.waitFor();
 await page.getByRole('textbox',{name:'Type a command'}).fill('synthetic'); await page.keyboard.press('Enter');
 await dialog.waitFor({state:'hidden'});
 assert.equal(await page.getByLabel('Chosen command').innerText(),'go-to-evidence');
 const sep=page.getByRole('separator'); await sep.focus();
 for(const [key,value] of [['ArrowRight','63'],['Home','20'],['ArrowLeft','20'],['End','80'],['ArrowRight','80']]) {await page.keyboard.press(key);assert.equal(await sep.getAttribute('aria-valuenow'),value);}
 assert.equal(await page.locator('img').count(),0);
 assert.equal(await page.evaluate(()=>window.PLANTED),undefined);
 assert.equal(await page.locator('p[role=status]').getAttribute('role'),'status');
 assert.match(await page.locator('p[role=status]').innerText(),/<img src=/);
 assert.deepEqual(external,[]); assert.deepEqual(errors,[]);
 assert.equal(await page.evaluate(()=>localStorage.length+sessionStorage.length),0);
 fs.writeFileSync(require('node:path').join(process.argv[3], 'browser-accessibility.txt'),await page.locator('body').ariaSnapshot());
 await page.screenshot({path:require('node:path').join(process.argv[3], 'browser.png'),fullPage:true});
 console.log('PASS Chromium '+browser.version()+': modal focus containment/return, Enter/Escape, separator keyboard bounds, accessible labels/status, inert renderer, no external requests/storage');
 } finally {await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1});
```

## Still required for full acceptance

- Exercise complete installed journeys on the declared Windows, macOS and Ubuntu
  matrix with native keyboard navigation and actual screen readers, including
  native dialogs, errors, cancellations, focus recovery, zoom and high contrast.
  Retain screen-reader/OS versions and human observations; automated semantics
  or a headless package installation does not pass this gate.
- Retain candidate-specific signed package hashes and independent security
  assessment/disposition, and validate actual customer authorization, OS ACLs,
  credential stores, TLS/OIDC, egress and backup/restore operations with explicit
  customer authorization. Local synthetic tests establish none of those controls.
- Resolve #164's selector-persistence decision and #183's frontend test-runner
  decision separately. This work does not change either contract or decision.

#111 remains open until that remaining scope is actually evidenced.
