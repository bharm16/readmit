"""Measure the local synthetic CLI envelope; print Markdown, never a pass verdict.

Run with an explicitly selected executable. Temporary corpus bytes are owned by
this harness and removed on exit. No customer evidence, hostname, serial number,
network target or installation directory is recorded. macOS/Linux only: native
Windows, reference hardware and webview painting require separate qualification.
"""
import argparse
import hashlib
import json
import math
import os
from pathlib import Path
import re
import selectors
import signal
import shutil
import subprocess
import sys
import tempfile
import time


PLAN = ['--framing', 'mllp', '--terminator', 'cr', '--encoding', 'us-ascii', '--direction', 'inbound']
INPUTS = ['--seed', '7', '--base-time', '2026-01-02T03:04:05Z', '--generator-version', 'readmit-corpus-v1', '--profile-version', 'readmit-siu-v1']


def p95(values):
    if not values or any(not math.isfinite(v) or v < 0 for v in values):
        raise ValueError('finite nonnegative measurements required')
    return sorted(values)[math.ceil(len(values) * .95) - 1]


def rss_bytes(output, system):
    patterns = {'darwin': r'^\s*(\d+)\s+maximum resident set size\s*$',
                'linux': r'^\s*Maximum resident set size \(kbytes\):\s*(\d+)\s*$'}
    match = re.search(patterns.get(system, r'(?!)'), output, re.MULTILINE)
    if not match or int(match[1]) <= 0:
        raise ValueError('OS peak RSS unavailable; cannot replace it with a buffer bound')
    return int(match[1]) * (1024 if system == 'linux' else 1)


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def run(command, timeout=300, **kwargs):
    # time(1) wraps the actual executable: a timeout must stop both, not leave
    # the measured child consuming a deleted temporary corpus in the background.
    with subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                          text=True, start_new_session=True, **kwargs) as process:
        try:
            stdout, stderr = process.communicate(timeout=timeout)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.communicate()
            raise RuntimeError('measurement deadline exceeded; no envelope verdict recorded') from None
        if process.returncode:
            raise RuntimeError('measurement command failed; no envelope verdict recorded')
        return subprocess.CompletedProcess(command, process.returncode, stdout, stderr)


def cancellation(command, corpus, report):
    # The first completed batch is a causal barrier; never signal before the
    # CLI has installed its handler or infer cancellation from a killed process.
    process = subprocess.Popen([*command, 'corpus', 'scan', str(corpus), *PLAN,
                                '--progress', '--report', str(report)],
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    try:
        with selectors.DefaultSelector() as selector:
            selector.register(process.stderr, selectors.EVENT_READ)
            if not selector.select(30):
                raise RuntimeError('no scan progress before cancellation deadline')
            line = process.stderr.readline()
            if not line.startswith(b'scanning:'):
                raise RuntimeError('scan did not reach the cancellation barrier')
        started = time.monotonic_ns()
        process.send_signal(signal.SIGINT)
        stdout, _ = process.communicate(timeout=30)
        elapsed = (time.monotonic_ns() - started) / 1e6
        if process.returncode == 0 or b'State: cancelled' not in stdout or report.exists():
            raise RuntimeError('interrupted scan did not preserve the cancelled/no-report contract')
        return elapsed
    except subprocess.TimeoutExpired:
        raise RuntimeError('cancellation acknowledgement deadline exceeded') from None
    finally:
        if process.poll() is None:
            process.kill()
        process.communicate()


def oversized_import(command, corpus, root):
    try:
        imported = subprocess.run([*command, 'import', '--file', str(corpus), *PLAN,
                                   '--output', str(root / 'case'), '--receipt', str(root / 'receipt.json')],
                                  capture_output=True, text=True, timeout=60)
    except (OSError, subprocess.TimeoutExpired):
        raise RuntimeError('oversized import could not be measured') from None
    if (imported.returncode == 0
            or 'a declared import location exceeds its size limit' not in imported.stderr
            or (root / 'case').exists() or (root / 'receipt.json').exists()):
        raise RuntimeError('oversized case import did not reach its size refusal without artifacts')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--operation-policy', type=Path, required=True, help='explicit activated local operation policy for synthetic generation and import')
    parser.add_argument('--large', action='store_true', help='also write and scan exactly 5 GiB of synthetic messages once')
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    if sys.platform not in ('darwin', 'linux'):
        parser.error('OS peak RSS measurement supports macOS/Linux only')
    try:
        policy = args.operation_policy.resolve(strict=True)
    except OSError:
        parser.error('operation policy must name an existing local file')
    command = [str(binary), '--operation-policy', str(policy)]
    identity = digest(binary)
    with tempfile.TemporaryDirectory(prefix='readmit-performance-') as scratch:
        root = Path(scratch)
        results = []
        for messages in (10000, 1000000):
            corpus, manifest = root / f'{messages}.mllp', root / f'{messages}.json'
            run([*command, 'corpus', 'generate', '--output', str(corpus), '--manifest', str(manifest),
                 '--messages', str(messages), *INPUTS, *PLAN])
            declaration = json.loads(manifest.read_text())
            expected = digest(corpus)
            if declaration['sha256'] != expected or declaration['bytes'] != corpus.stat().st_size:
                raise RuntimeError('generator manifest does not identify actual bytes')
            elapsed, rss = [], []
            for repetition in range(5):
                report = root / f'{messages}-{repetition}.json'
                started = time.monotonic_ns()
                measured = run(['/usr/bin/time', '-l' if sys.platform == 'darwin' else '-v',
                                *command, 'corpus', 'scan', str(corpus), *PLAN,
                                '--window-offset', '9800', '--window-limit', '200', '--report', str(report)],
                               env=dict(os.environ, LC_ALL='C'))
                elapsed.append((time.monotonic_ns() - started) / 1e6)
                rss.append(rss_bytes(measured.stderr, sys.platform))
                benchmark = json.loads(report.read_text())
                if benchmark['corpus']['sha256'] != expected or benchmark['measured']['records'] != messages or benchmark['measured']['undecodable'] != 0:
                    raise RuntimeError('scan did not measure the complete declared corpus')
            results.append((messages, corpus.stat().st_size, expected, elapsed, rss, benchmark['hardware']))
        large_result = None
        if args.large:
            if shutil.disk_usage(root).free < 6 * 1024**3:
                raise RuntimeError('5 GiB measurement requires at least 6 GiB free scratch space')
            # Independently specified padded fixture, not readmit-corpus-v1.
            # Spread the remainder across whole records; every record is MLLP.
            large = root / 'five-gib.mllp'
            count, size = 1000000, 5 * 1024**3
            base, extra = divmod(size, count)
            prefix = b'\x0bMSH|^~\\&|READMIT|TEST|RECV|LAB|20260101120000||SIU^S12|SCALE|P|2.5.1\rNTE|1||'
            with large.open('xb') as stream:
                for length, records in ((base + 1, extra), (base, count - extra)):
                    record = prefix + b'X' * (length - len(prefix) - 3) + b'\r\x1c\r'
                    for offset in range(0, records, 1000):
                        stream.write(record * min(1000, records - offset))
            large_sha = digest(large)
            report = root / 'five-gib.json'
            started = time.monotonic_ns()
            measured = run(['/usr/bin/time', '-l' if sys.platform == 'darwin' else '-v', *command,
                            'corpus', 'scan', str(large), *PLAN, '--report', str(report)],
                           env=dict(os.environ, LC_ALL='C'))
            wall = (time.monotonic_ns() - started) / 1e6
            observed = json.loads(report.read_text())
            if observed['corpus']['sha256'] != large_sha or observed['measured']['records'] != count or observed['measured']['undecodable'] != 0 or large.stat().st_size != size:
                raise RuntimeError('5 GiB scan did not read the exact fixture')
            oversized_import(command, large, root)
            large_result = (large_sha, wall, rss_bytes(measured.stderr, sys.platform))
        cancel = [cancellation(command, corpus, root / f'cancel-{i}.json') for i in range(5)]
        if digest(corpus) != expected:
            raise RuntimeError('read-only scans changed corpus bytes')
        if digest(binary) != identity:
            raise RuntimeError('executable changed during measurement')
    print('# Local CLI performance measurements\n')
    print(f'Executable SHA-256: `{identity}`. Hardware observation: `{results[-1][-1]}`.\n')
    print('Generator: readmit-corpus-v1, seed 7, base time 2026-01-02T03:04:05Z, profile readmit-siu-v1; MLLP/CR/US-ASCII/inbound.\n')
    for messages, size, sha, elapsed, rss, _ in results:
        print(f'- {messages} messages, {size} bytes, SHA-256 `{sha}`. Scan wall milliseconds: {elapsed}; nearest-rank p95 {p95(elapsed):.3f}. OS peak RSS bytes per process: {rss}.')
    if large_result:
        print(f'- Independent 1,000,000-message exact 5 GiB fixture (padded NTE, repeated control ID): SHA-256 `{large_result[0]}`; one scan {large_result[1]:.3f} ms, OS peak RSS {large_result[2]} bytes. Case import refused and wrote no case/receipt. One sample, no percentile claim; this is not project-scale acceptance.')
    print(f'- SIGINT after completed batch, milliseconds: {cancel}; nearest-rank p95 {p95(cancel):.3f}. All five exited nonzero, reported cancelled and wrote no benchmark. Corpus unchanged.')
    print('\nFive samples include the first scan; no cold-cache claim. OS RSS is process peak, not the scanner buffer counter. These are CLI scans, not project imports, UI paints, or a claim to meet the 1M/5GiB envelope. Installed RAM/storage class are undeclared.')


if __name__ == '__main__':
    main()
