#!/usr/bin/env python3
"""Build and run the native Mac screenshot catalog into a fresh Desktop folder."""
from __future__ import annotations
import argparse
import datetime as dt
import hashlib
import html
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]

def command(args: list[str], cwd: Path, log) -> None:
    print('+ ' + ' '.join(args), flush=True)
    result = subprocess.run(args, cwd=cwd, stdout=log, stderr=subprocess.STDOUT)
    log.flush()
    if result.returncode:
        raise RuntimeError(f'{args[0]} failed ({result.returncode}); see build.log')

def gallery(output: Path, report: dict) -> None:
    cards = []
    seen = set()
    problems = list(report.get('failures', []))
    shots = report.get('shots', [])
    if not shots:
        problems.append('No screenshots were captured')
    for item in report.get('inventory', []):
        if not any(shot.get('component') == item['name'] and shot.get('source') == item['source'] for shot in shots):
            problems.append(f"Uncaptured: {item['source']}#{item['name']}")
    for shot in report.get('shots', []):
        filename = shot['file']
        if Path(filename).name != filename or not filename.endswith('.png') or filename in seen:
            raise RuntimeError('Invalid or duplicate capture filename')
        seen.add(filename)
        path = output / filename
        if not path.is_file():
            raise RuntimeError(f"Missing PNG: {path.name}")
        shot['sha256'] = hashlib.sha256(path.read_bytes()).hexdigest()
        title = shot.get('component') or shot['scenario']
        cards.append(f'<article data-kind="{html.escape(shot["kind"])}"><h2>{html.escape(title)}</h2>'
                     f'<p>{html.escape(shot["scenario"])}</p><a href="{html.escape(path.name)}">'
                     f'<img loading="lazy" src="{html.escape(path.name)}" alt="{html.escape(title)}"></a></article>')
    problems.extend(f"Uncaptured: {item['name']}" for item in report.get('missing', []) if not any(item['name'] in problem for problem in problems))
    report['coverage'] = [{**item, 'captures': [shot['file'] for shot in shots if shot.get('component') == item['name'] and shot.get('source') == item['source']]} for item in report.get('inventory', [])]
    report['problems'] = problems
    report['complete'] = not problems
    (output / 'manifest.json').write_text(json.dumps(report, indent=2) + '\n')
    status = 'Complete' if report['complete'] else 'INCOMPLETE'
    captured = sum(bool(item['captures']) for item in report['coverage'])
    expected = len(report['coverage'])
    notes = ''.join(f'<li>{html.escape(problem)}</li>' for problem in problems)
    (output / 'index.html').write_text('''<!doctype html><meta charset="utf-8"><title>Readmit screenshots</title>
<style>body{font:15px system-ui;background:#f5f5f5;color:#222;margin:32px}header{max-width:1000px}input,select{font:inherit;padding:10px;margin:8px}main{display:grid;grid-template-columns:repeat(auto-fill,minmax(440px,1fr));gap:24px}article{background:white;border:1px solid #ddd;padding:18px;border-radius:10px;overflow:hidden}h2{font-size:18px}p{color:#555}img{width:100%;height:auto;max-height:650px;object-fit:contain;object-position:top}a{color:inherit}</style>
''' + f'<header><h1>Readmit screenshot catalog · {status}</h1><p>{len(cards)} native Wails WebKit captures · {captured}/{expected} components · 1100 × 760 · synthetic fixtures. Component crops may show one visible portion; page scroll captures retain surrounding content. These demonstrate rendering, not live backend acceptance.</p><a href="manifest.json">Coverage manifest</a><ul>{notes}</ul><p><input id="filter" placeholder="Filter by component, page or state"><select id="kind"><option value="">All captures</option><option>page</option><option>component</option></select></p></header><main>' + ''.join(cards) + '''</main><script>function filter(){const q=document.querySelector('#filter').value.toLowerCase(),k=document.querySelector('#kind').value;document.querySelectorAll('article').forEach(a=>a.hidden=!a.textContent.toLowerCase().includes(q)||(k&&a.dataset.kind!==k))}document.querySelector('#filter').oninput=filter;document.querySelector('#kind').onchange=filter</script>''')

def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, help='Fresh destination directory; never overwritten')
    args = parser.parse_args()
    if platform.system() != 'Darwin':
        parser.error('native capture currently requires macOS with Xcode command-line tools')
    for executable in ('node', 'npm', 'go'):
        if not shutil.which(executable):
            parser.error(f'{executable} is required')
    output = (args.output or Path.home() / 'Desktop' / 'Readmit Screenshots' / dt.datetime.now().strftime('%Y-%m-%d_%H-%M-%S')).expanduser().absolute()
    try:
        output.mkdir(parents=True, exist_ok=False, mode=0o700)
    except OSError as error:
        parser.error(f'cannot create fresh output directory: {error}')
    print(f'Output: {output}', flush=True)
    frontend = ROOT / 'desktop/frontend'
    try:
        with (output / 'build.log').open('w') as log, tempfile.TemporaryDirectory(prefix='readmit-catalog-') as temp:
            if not (frontend / 'node_modules').is_dir():
                command(['npm', 'ci'], frontend, log)
            command(['node', 'catalog.inventory.mjs'], frontend, log)
            command(['npx', 'tsc', '--noEmit'], frontend, log)
            command(['npx', 'vite', 'build', '--config', 'vite.catalog.config.ts'], frontend, log)
            dist = ROOT / 'desktop/captureapp/dist'
            (dist / 'catalog.html').rename(dist / 'index.html')
            (dist / '.gitkeep').touch()
            executable = str(Path(temp) / 'readmit-catalog')
            command(['go', 'build', '-tags', 'production', '-o', executable, './captureapp'], ROOT / 'desktop', log)
            command([executable, str(output)], ROOT, log)
        report = json.loads((output / 'capture.json').read_text())
        report['commit'] = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
        report['source_sha256'] = {str(path.relative_to(ROOT)): hashlib.sha256(path.read_bytes()).hexdigest() for path in sorted((ROOT / 'desktop/frontend/src').glob('*')) if path.is_file()}
        report['working_tree_dirty'] = bool(subprocess.check_output(['git', 'status', '--porcelain'], cwd=ROOT))
        gallery(output, report)
        print(f"{'Complete' if report['complete'] else 'Incomplete'}: {len(report.get('shots', []))} captures\n{output / 'index.html'}")
        return 0 if report['complete'] else 1
    except (RuntimeError, OSError, ValueError) as error:
        (output / 'failure.txt').write_text(str(error) + '\n')
        print(f'{error}\nOutput retained: {output}', file=sys.stderr)
        return 1

if __name__ == '__main__':
    raise SystemExit(main())
